// Command payment is the Payment service.
//
// It reacts to InventoryReserved - never to OrderPlaced - which is how the
// choreography enforces the order of the saga without anyone directing it:
// payment cannot be taken for stock that was never held.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maltashop/lean-platform/internal/platform"
)

type server struct {
	cfg    platform.Config
	pool   *pgxpool.Pool
	stream *platform.Stream
	logger *slog.Logger
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe this container and exit")
	flag.Parse()
	cfg := platform.Load()
	if *healthcheck {
		platform.SelfCheck(cfg.Port)
	}
	logger := platform.NewLogger(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	pool, err := platform.OpenDB(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("service.start_failed", slog.String("error", err.Error()))
		return
	}
	defer pool.Close()
	stream, err := platform.Connect(ctx, cfg, logger)
	if err != nil {
		logger.Error("service.start_failed", slog.String("error", err.Error()))
		return
	}
	defer stream.Conn.Drain()

	app := &server{cfg: cfg, pool: pool, stream: stream, logger: logger}
	go platform.RunOutbox(ctx, pool, stream, logger, time.Duration(cfg.OutboxPoll)*time.Millisecond)

	// Payment listens for OrderPlaced only to learn the amount, and acts on
	// InventoryReserved. Both arrive on one stream, and JetStream delivers a
	// stream in the order it was written, so the intent is always recorded first.
	if err := stream.Subscribe(ctx, "payment-service",
		[]string{platform.SubjectOrderPlaced, platform.SubjectInventoryReserved}, app.onEvent); err != nil {
		logger.Error("service.start_failed", slog.String("error", err.Error()))
		return
	}

	router := chi.NewRouter()
	router.Use(platform.Correlate)
	platform.Health(router, cfg, func(ctx context.Context) error { return pool.Ping(ctx) })
	router.Get("/payments", app.listPayments)
	if err := platform.Serve(ctx, cfg, router, logger); err != nil {
		logger.Error("service.stopped_with_error", slog.String("error", err.Error()))
	}
}

func (s *server) listPayments(writer http.ResponseWriter, request *http.Request) {
	orderID := request.URL.Query().Get("order_id")
	query := `SELECT order_id::text, amount_cents, state, coalesce(psp_reference, ''),
	                 coalesce(decline_reason, ''), created_at FROM payments`
	args := []any{}
	if orderID != "" {
		query += ` WHERE order_id = $1`
		args = append(args, orderID)
	}
	query += ` ORDER BY created_at DESC LIMIT 50`
	rows, err := s.pool.Query(request.Context(), query, args...)
	if err != nil {
		platform.JSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, state, reference, reason string
		var amount int64
		var created time.Time
		if err := rows.Scan(&id, &amount, &state, &reference, &reason, &created); err == nil {
			out = append(out, map[string]any{"order_id": id, "amount_cents": amount, "state": state,
				"psp_reference": reference, "decline_reason": reason, "created_at": created})
		}
	}
	platform.JSON(writer, http.StatusOK, out)
}

// callProvider stands in for the external card provider. A real adapter would
// call its API here; the limit makes the saga's compensating path easy to show.
func (s *server) callProvider(amount int64) (string, string) {
	if amount > s.cfg.DeclineOver {
		return "", "card_limit_exceeded"
	}
	return "psp_" + uuid.NewString()[:12], ""
}

func (s *server) onEvent(ctx context.Context, envelope platform.Envelope) error {
	switch envelope.EventType {
	case "OrderPlaced":
		return s.recordIntent(ctx, envelope)
	case "InventoryReserved":
		return s.onReserved(ctx, envelope)
	}
	return nil
}

// recordIntent stores what the order is worth, so that when stock is reserved the
// Payment service can act without calling anyone.
func (s *server) recordIntent(ctx context.Context, envelope platform.Envelope) error {
	var placed platform.OrderPlaced
	if err := json.Unmarshal(envelope.Data, &placed); err != nil {
		return err
	}
	ctx = platform.WithOrder(ctx, placed.OrderID)
	return platform.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		fresh, err := platform.Claim(ctx, tx, envelope)
		if err != nil || !fresh {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO payment_intents (order_id, customer_id, amount_cents, currency)
			 VALUES ($1, $2, $3, $4) ON CONFLICT (order_id) DO NOTHING`,
			placed.OrderID, placed.CustomerID, placed.TotalCents, placed.Currency)
		if err == nil {
			platform.Ctx(ctx, s.logger).Info("payment.intent_recorded",
				slog.String("event", "payment.intent_recorded"), slog.Int64("amount_cents", placed.TotalCents))
		}
		return err
	})
}

func (s *server) onReserved(ctx context.Context, envelope platform.Envelope) error {
	var reserved platform.StockOutcome
	if err := json.Unmarshal(envelope.Data, &reserved); err != nil {
		return err
	}
	ctx = platform.WithOrder(ctx, reserved.OrderID)
	logger := platform.Ctx(ctx, s.logger)

	return platform.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		fresh, err := platform.Claim(ctx, tx, envelope)
		if err != nil || !fresh {
			return err
		}
		var amount int64
		var customer string
		if err := tx.QueryRow(ctx,
			`SELECT amount_cents, customer_id FROM payment_intents WHERE order_id = $1`,
			reserved.OrderID).Scan(&amount, &customer); err != nil {
			logger.Warn("payment.no_intent", slog.String("event", "payment.no_intent"))
			return nil
		}

		reference, declined := s.callProvider(amount)
		state := "AUTHORISED"
		if declined != "" {
			state = "DECLINED"
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO payments (payment_id, order_id, customer_id, amount_cents, state,
			                       psp_reference, decline_reason)
			 VALUES ($1, $2, $3, $4, $5, NULLIF($6,''), NULLIF($7,''))
			 ON CONFLICT (order_id) DO NOTHING`,
			uuid.NewString(), reserved.OrderID, customer, amount, state, reference, declined); err != nil {
			return err
		}

		subject, eventType := platform.SubjectPaymentAuthorised, "PaymentAuthorised"
		if declined != "" {
			subject, eventType = platform.SubjectPaymentDeclined, "PaymentDeclined"
		}
		event, err := platform.NewEnvelope(s.cfg.ServiceName, eventType,
			envelope.CorrelationID, envelope.EventID,
			platform.PaymentOutcome{OrderID: reserved.OrderID, PSPReference: reference, Reason: declined})
		if err != nil {
			return err
		}
		if declined != "" {
			logger.Warn("payment.declined", slog.String("event", "payment.declined"),
				slog.Int64("amount_cents", amount), slog.String("reason", declined))
		} else {
			logger.Info("payment.authorised", slog.String("event", "payment.authorised"),
				slog.Int64("amount_cents", amount))
		}
		return platform.Enqueue(ctx, tx, subject, event)
	})
}
