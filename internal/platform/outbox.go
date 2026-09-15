package platform

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// outboxLockID identifies the PostgreSQL advisory lock that elects the single
// replica allowed to drain the outbox. Only one replica publishes at a time,
// which keeps events for one order in the order they were written. It is the
// simpler half of the trade-off against sharing the work with SKIP LOCKED:
// less throughput, but strict ordering and nothing to reason about.
const outboxLockID int64 = 847_120_391

// Enqueue stores a message in the same transaction as the business change.
func Enqueue(ctx context.Context, tx pgx.Tx, subject string, envelope Envelope) error {
	body, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox (subject, body) VALUES ($1, $2)`, subject, body)
	return err
}

// Claim records that an event has been handled. It returns false when the event
// has been seen before, which is what makes every handler safe to redeliver into.
func Claim(ctx context.Context, tx pgx.Tx, envelope Envelope) (bool, error) {
	tag, err := tx.Exec(ctx,
		`INSERT INTO processed_events (event_id, event_type) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		envelope.EventID, envelope.EventType)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// RunOutbox drains the outbox until ctx is cancelled. The replica holding the
// advisory lock does the work; the others poll and find the lock taken, so they
// cost nothing and take over the moment the leader stops.
func RunOutbox(ctx context.Context, pool *pgxpool.Pool, stream *Stream, logger *slog.Logger, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := drain(ctx, pool, stream); err != nil {
				logger.Warn("outbox.drain_failed", slog.String("event", "outbox.drain_failed"),
					slog.String("error", err.Error()))
			}
		}
	}
}

func drain(ctx context.Context, pool *pgxpool.Pool, stream *Stream) error {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer connection.Release()

	var leader bool
	if err := connection.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, outboxLockID).Scan(&leader); err != nil {
		return err
	}
	if !leader {
		return nil
	}
	defer func() { _, _ = connection.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, outboxLockID) }()

	rows, err := connection.Query(ctx,
		`SELECT id, subject, body FROM outbox WHERE published_at IS NULL ORDER BY id LIMIT 100`)
	if err != nil {
		return err
	}
	type pending struct {
		id       int64
		subject  string
		envelope Envelope
	}
	var batch []pending
	for rows.Next() {
		var id int64
		var subject string
		var body []byte
		if err := rows.Scan(&id, &subject, &body); err != nil {
			rows.Close()
			return err
		}
		var envelope Envelope
		if err := json.Unmarshal(body, &envelope); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, pending{id, subject, envelope})
	}
	rows.Close()

	for _, item := range batch {
		messageCtx := WithCorrelation(ctx, item.envelope.CorrelationID)
		if err := stream.Publish(messageCtx, item.subject, item.envelope); err != nil {
			return err // leave it unpublished; the next tick tries again
		}
		if _, err := connection.Exec(ctx, `UPDATE outbox SET published_at = now() WHERE id = $1`, item.id); err != nil {
			return err
		}
	}
	return nil
}
