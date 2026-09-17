// Command order is the Order service: the only synchronous step in checkout.
//
// It takes the request, writes the order and the OrderPlaced event in one local
// transaction and answers at once. It then watches the stream and records what
// the other services decide. It never tells them what to do: this is a
// choreographed saga, so there is no orchestrator and no central point of failure.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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

type placeOrderRequest struct {
	CustomerID string          `json:"customer_id"`
	Items      []platform.Line `json:"items"`
}

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

	// Choreography: the Order service subscribes to what happened. It issues no commands.
	err = stream.Subscribe(ctx, "order-service", []string{
		platform.SubjectInventoryReserved,
		platform.SubjectInventoryRejected,
		platform.SubjectInventoryReleased,
		platform.SubjectPaymentAuthorised,
		platform.SubjectPaymentDeclined,
	}, app.onEvent)
	if err != nil {
		logger.Error("service.start_failed", slog.String("error", err.Error()))
		return
	}

	router := chi.NewRouter()
	router.Use(platform.Correlate)
	platform.Health(router, cfg, func(ctx context.Context) error { return pool.Ping(ctx) })
	router.Post("/orders", app.placeOrder)
	router.Get("/orders/{id}", app.getOrder)

	if err := platform.Serve(ctx, cfg, router, logger); err != nil {
		logger.Error("service.stopped_with_error", slog.String("error", err.Error()))
	}
}

func (s *server) placeOrder(writer http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	var body placeOrderRequest
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.CustomerID == "" || len(body.Items) == 0 {
		platform.JSON(writer, http.StatusBadRequest,
			map[string]string{"error": "customer_id and a non-empty items array are required"})
		return
	}
	idempotencyKey := request.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		idempotencyKey = uuid.NewString()
	}

	// A repeated Idempotency-Key must return the first order, not make a second one.
	var existingID, existingState string
	err := s.pool.QueryRow(ctx,
		`SELECT order_id::text, state FROM orders WHERE idempotency_key = $1`, idempotencyKey).
		Scan(&existingID, &existingState)
	if err == nil {
		platform.Ctx(ctx, s.logger).Info("order.idempotent_replay",
			slog.String("event", "order.idempotent_replay"), slog.String("order_id", existingID))
		platform.JSON(writer, http.StatusOK,
			map[string]any{"order_id": existingID, "state": existingState, "replayed": true})
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		platform.JSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	orderID := uuid.NewString()
	ctx = platform.WithOrder(ctx, orderID)
	correlationID := platform.Correlation(ctx)
	var total int64

	err = platform.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		// Price every line first: the total is part of the order row, and the
		// order row must exist before its items can reference it.
		type priced struct {
			line platform.Line
			unit int64
		}
		lines := make([]priced, 0, len(body.Items))
		total = 0
		for _, line := range body.Items {
			if line.Quantity <= 0 {
				return fmt.Errorf("quantity for %s must be positive", line.SKU)
			}
			var unit int64
			if err := tx.QueryRow(ctx, `SELECT unit_cents FROM price_cache WHERE sku = $1`, line.SKU).
				Scan(&unit); err != nil {
				return fmt.Errorf("unknown sku %s", line.SKU)
			}
			total += unit * int64(line.Quantity)
			lines = append(lines, priced{line, unit})
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO orders (order_id, customer_id, state, total_cents, idempotency_key, correlation_id)
			 VALUES ($1, $2, 'PENDING', $3, $4, $5)`,
			orderID, body.CustomerID, total, idempotencyKey, correlationID); err != nil {
			return err
		}
		for _, item := range lines {
			if _, err := tx.Exec(ctx,
				`INSERT INTO order_items (order_id, sku, quantity, unit_cents) VALUES ($1, $2, $3, $4)`,
				orderID, item.line.SKU, item.line.Quantity, item.unit); err != nil {
				return err
			}
		}
		envelope, err := platform.NewEnvelope(s.cfg.ServiceName, "OrderPlaced", correlationID, "",
			platform.OrderPlaced{OrderID: orderID, CustomerID: body.CustomerID,
				TotalCents: total, Currency: "EUR", Lines: body.Items})
		if err != nil {
			return err
		}
		// The event joins the outbox inside this transaction: it is sent if and
		// only if the order row is committed.
		return platform.Enqueue(ctx, tx, platform.SubjectOrderPlaced, envelope)
	})
	if err != nil {
		platform.Ctx(ctx, s.logger).Warn("order.rejected",
			slog.String("event", "order.rejected"), slog.String("error", err.Error()))
		platform.JSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	platform.Ctx(ctx, s.logger).Info("order.accepted",
		slog.String("event", "order.accepted"), slog.Int64("total_cents", total),
		slog.Int("lines", len(body.Items)))
	writer.Header().Set("Location", "/orders/"+orderID)
	platform.JSON(writer, http.StatusCreated, map[string]any{
		"order_id":       orderID,
		"state":          "PENDING",
		"total_cents":    total,
		"currency":       "EUR",
		"correlation_id": correlationID,
		"message":        "Order received. Stock and payment are being confirmed.",
	})
}

func (s *server) getOrder(writer http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	id := chi.URLParam(request, "id")
	var (
		customer, state, correlation string
		reason                       *string
		total                        int64
		created, updated             time.Time
	)
	err := s.pool.QueryRow(ctx,
		`SELECT customer_id, state, total_cents, failure_reason, correlation_id::text, created_at, updated_at
		   FROM orders WHERE order_id = $1`, id).
		Scan(&customer, &state, &total, &reason, &correlation, &created, &updated)
	if err != nil {
		platform.JSON(writer, http.StatusNotFound, map[string]string{"error": "order not found"})
		return
	}
	rows, err := s.pool.Query(ctx,
		`SELECT sku, quantity, unit_cents FROM order_items WHERE order_id = $1 ORDER BY sku`, id)
	if err != nil {
		platform.JSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var sku string
		var quantity int
		var unit int64
		if err := rows.Scan(&sku, &quantity, &unit); err == nil {
			items = append(items, map[string]any{"sku": sku, "quantity": quantity, "unit_cents": unit})
		}
	}
	history, _ := s.pool.Query(ctx,
		`SELECT state, note, recorded_at FROM order_history WHERE order_id = $1 ORDER BY recorded_at`, id)
	defer history.Close()
	timeline := []map[string]any{}
	for history.Next() {
		var state, note string
		var at time.Time
		if err := history.Scan(&state, &note, &at); err == nil {
			timeline = append(timeline, map[string]any{"state": state, "note": note, "at": at})
		}
	}
	platform.JSON(writer, http.StatusOK, map[string]any{
		"order_id": id, "customer_id": customer, "state": state, "total_cents": total,
		"currency": "EUR", "failure_reason": reason, "correlation_id": correlation,
		"created_at": created, "updated_at": updated, "items": items, "timeline": timeline,
	})
}
