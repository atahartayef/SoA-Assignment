package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/maltashop/lean-platform/internal/platform"
)

// allowed records which event may move the order on from which state. A message
// that does not fit is late or duplicated, and is ignored rather than applied.
var allowed = map[string]map[string]string{
	"PENDING": {
		"InventoryReserved": "STOCK_RESERVED",
		"InventoryRejected": "REJECTED",
	},
	"STOCK_RESERVED": {
		"PaymentAuthorised": "CONFIRMED",
		"PaymentDeclined":   "COMPENSATING",
		"InventoryRejected": "REJECTED",
	},
	"COMPENSATING": {
		"InventoryReleased": "CANCELLED",
	},
}

func next(state, eventType string) (string, bool) {
	to, ok := allowed[state][eventType]
	return to, ok
}

// onEvent is the Order service's whole part in the saga: it observes what the
// other services decided and keeps its own aggregate up to date, publishing the
// outcome for anyone who cares (the Notification service, and in production the
// recommendation feed and the partner API).
func (s *server) onEvent(ctx context.Context, envelope platform.Envelope) error {
	var outcome struct {
		OrderID string `json:"order_id"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(envelope.Data, &outcome); err != nil {
		return err
	}
	ctx = platform.WithOrder(ctx, outcome.OrderID)
	logger := platform.Ctx(ctx, s.logger)

	return platform.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		fresh, err := platform.Claim(ctx, tx, envelope)
		if err != nil {
			return err
		}
		if !fresh {
			logger.Info("event.duplicate_ignored", slog.String("event", "event.duplicate_ignored"),
				slog.String("event_type", envelope.EventType))
			return nil
		}

		var state, customer string
		var total int64
		var reason *string
		if err := tx.QueryRow(ctx,
			`SELECT state, customer_id, total_cents, failure_reason FROM orders
			  WHERE order_id = $1 FOR UPDATE`, outcome.OrderID).
			Scan(&state, &customer, &total, &reason); err != nil {
			logger.Warn("order.unknown", slog.String("event", "order.unknown"),
				slog.String("event_type", envelope.EventType))
			return nil
		}

		target, ok := next(state, envelope.EventType)
		if !ok {
			logger.Warn("event.out_of_order", slog.String("event", "event.out_of_order"),
				slog.String("state", state), slog.String("event_type", envelope.EventType))
			return nil
		}

		failure := outcome.Reason
		if failure == "" && reason != nil {
			failure = *reason
		}
		if _, err := tx.Exec(ctx,
			`UPDATE orders SET state = $2, failure_reason = NULLIF($3, ''), updated_at = now()
			  WHERE order_id = $1`, outcome.OrderID, target, failure); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO order_history (order_id, state, note) VALUES ($1, $2, $3)`,
			outcome.OrderID, target, envelope.EventType); err != nil {
			return err
		}

		lines, err := orderLines(ctx, tx, outcome.OrderID)
		if err != nil {
			return err
		}

		switch target {
		case "CONFIRMED":
			event, err := platform.NewEnvelope(s.cfg.ServiceName, "OrderConfirmed",
				envelope.CorrelationID, envelope.EventID,
				platform.OrderOutcome{OrderID: outcome.OrderID, CustomerID: customer,
					TotalCents: total, Lines: lines})
			if err != nil {
				return err
			}
			if err := platform.Enqueue(ctx, tx, platform.SubjectOrderConfirmed, event); err != nil {
				return err
			}
		case "REJECTED", "CANCELLED":
			event, err := platform.NewEnvelope(s.cfg.ServiceName, "OrderCancelled",
				envelope.CorrelationID, envelope.EventID,
				platform.OrderOutcome{OrderID: outcome.OrderID, CustomerID: customer, Reason: failure})
			if err != nil {
				return err
			}
			if err := platform.Enqueue(ctx, tx, platform.SubjectOrderCancelled, event); err != nil {
				return err
			}
		}
		logger.Info("order.state_changed", slog.String("event", "order.state_changed"),
			slog.String("from", state), slog.String("to", target),
			slog.String("event_type", envelope.EventType))
		return nil
	})
}

func orderLines(ctx context.Context, tx pgx.Tx, orderID string) ([]platform.Line, error) {
	rows, err := tx.Query(ctx, `SELECT sku, quantity FROM order_items WHERE order_id = $1 ORDER BY sku`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := []platform.Line{}
	for rows.Next() {
		var line platform.Line
		if err := rows.Scan(&line.SKU, &line.Quantity); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
}
