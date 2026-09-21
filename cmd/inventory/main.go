// Command inventory is the Inventory service.
//
// It reacts to two events and emits three. Reserving stock is a "try": the stock
// is held, not taken, which is what lets the saga undo the step if payment fails.
// Inventory knows nothing about Payment, and Payment knows nothing about Inventory.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
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

	err = stream.Subscribe(ctx, "inventory-service", []string{
		platform.SubjectOrderPlaced,
		platform.SubjectPaymentDeclined,
		platform.SubjectOrderConfirmed,
	}, app.onEvent)
	if err != nil {
		logger.Error("service.start_failed", slog.String("error", err.Error()))
		return
	}

	router := chi.NewRouter()
	router.Use(platform.Correlate)
	platform.Health(router, cfg, func(ctx context.Context) error { return pool.Ping(ctx) })
	router.Get("/stock", app.listStock)
	if err := platform.Serve(ctx, cfg, router, logger); err != nil {
		logger.Error("service.stopped_with_error", slog.String("error", err.Error()))
	}
}

func (s *server) listStock(writer http.ResponseWriter, request *http.Request) {
	rows, err := s.pool.Query(request.Context(),
		`SELECT sku, description, on_hand, held, on_hand - held AS available FROM stock ORDER BY sku`)
	if err != nil {
		platform.JSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var sku, description string
		var onHand, held, available int
		if err := rows.Scan(&sku, &description, &onHand, &held, &available); err == nil {
			out = append(out, map[string]any{"sku": sku, "description": description,
				"on_hand": onHand, "held": held, "available": available})
		}
	}
	platform.JSON(writer, http.StatusOK, out)
}

func (s *server) onEvent(ctx context.Context, envelope platform.Envelope) error {
	switch envelope.EventType {
	case "OrderPlaced":
		var placed platform.OrderPlaced
		if err := json.Unmarshal(envelope.Data, &placed); err != nil {
			return err
		}
		return s.reserve(platform.WithOrder(ctx, placed.OrderID), envelope, placed)
	case "PaymentDeclined":
		var outcome platform.PaymentOutcome
		if err := json.Unmarshal(envelope.Data, &outcome); err != nil {
			return err
		}
		return s.release(platform.WithOrder(ctx, outcome.OrderID), envelope, outcome.OrderID)
	case "OrderConfirmed":
		var outcome platform.OrderOutcome
		if err := json.Unmarshal(envelope.Data, &outcome); err != nil {
			return err
		}
		return s.commit(platform.WithOrder(ctx, outcome.OrderID), envelope, outcome.OrderID)
	}
	return nil
}

// reserve is the "try" half of try-confirm-cancel: it holds stock without taking it.
func (s *server) reserve(ctx context.Context, envelope platform.Envelope, placed platform.OrderPlaced) error {
	logger := platform.Ctx(ctx, s.logger)
	return platform.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		fresh, err := platform.Claim(ctx, tx, envelope)
		if err != nil || !fresh {
			return err
		}
		lines := append([]platform.Line(nil), placed.Lines...)
		// Rows are locked in SKU order so two orders for the same items cannot deadlock.
		sort.Slice(lines, func(i, j int) bool { return lines[i].SKU < lines[j].SKU })

		shortfall := ""
		for _, line := range lines {
			var onHand, held int
			if err := tx.QueryRow(ctx,
				`SELECT on_hand, held FROM stock WHERE sku = $1 FOR UPDATE`, line.SKU).Scan(&onHand, &held); err != nil {
				shortfall = "unknown_sku:" + line.SKU
				break
			}
			if !canHold(onHand, held, line.Quantity) {
				shortfall = "insufficient_stock:" + line.SKU
				break
			}
		}

		body, _ := json.Marshal(lines)
		if shortfall != "" {
			if _, err := tx.Exec(ctx,
				`INSERT INTO holds (order_id, lines, state) VALUES ($1, $2, 'REJECTED')
				   ON CONFLICT (order_id) DO NOTHING`, placed.OrderID, body); err != nil {
				return err
			}
			event, err := platform.NewEnvelope(s.cfg.ServiceName, "InventoryRejected",
				envelope.CorrelationID, envelope.EventID,
				platform.StockOutcome{OrderID: placed.OrderID, Reason: shortfall})
			if err != nil {
				return err
			}
			logger.Warn("stock.rejected", slog.String("event", "stock.rejected"),
				slog.String("reason", shortfall))
			return platform.Enqueue(ctx, tx, platform.SubjectInventoryRejected, event)
		}

		for _, line := range lines {
			if _, err := tx.Exec(ctx, `UPDATE stock SET held = held + $2 WHERE sku = $1`,
				line.SKU, line.Quantity); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO holds (order_id, lines, state) VALUES ($1, $2, 'HELD')
			   ON CONFLICT (order_id) DO UPDATE SET state = 'HELD', updated_at = now()`,
			placed.OrderID, body); err != nil {
			return err
		}
		event, err := platform.NewEnvelope(s.cfg.ServiceName, "InventoryReserved",
			envelope.CorrelationID, envelope.EventID, platform.StockOutcome{OrderID: placed.OrderID})
		if err != nil {
			return err
		}
		logger.Info("stock.reserved", slog.String("event", "stock.reserved"), slog.Int("lines", len(lines)))
		return platform.Enqueue(ctx, tx, platform.SubjectInventoryReserved, event)
	})
}

// release is the "cancel" half: the compensating action for reserve.
func (s *server) release(ctx context.Context, envelope platform.Envelope, orderID string) error {
	logger := platform.Ctx(ctx, s.logger)
	return platform.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		fresh, err := platform.Claim(ctx, tx, envelope)
		if err != nil || !fresh {
			return err
		}
		lines, err := heldLines(ctx, tx, orderID)
		if err != nil {
			return err
		}
		for _, line := range lines {
			if _, err := tx.Exec(ctx, `UPDATE stock SET held = greatest(held - $2, 0) WHERE sku = $1`,
				line.SKU, line.Quantity); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE holds SET state = 'RELEASED', updated_at = now() WHERE order_id = $1`, orderID); err != nil {
			return err
		}
		event, err := platform.NewEnvelope(s.cfg.ServiceName, "InventoryReleased",
			envelope.CorrelationID, envelope.EventID, platform.StockOutcome{OrderID: orderID})
		if err != nil {
			return err
		}
		logger.Info("stock.released", slog.String("event", "stock.released"))
		return platform.Enqueue(ctx, tx, platform.SubjectInventoryReleased, event)
	})
}

// commit is the "confirm" half: the hold becomes a real deduction.
func (s *server) commit(ctx context.Context, envelope platform.Envelope, orderID string) error {
	logger := platform.Ctx(ctx, s.logger)
	return platform.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		fresh, err := platform.Claim(ctx, tx, envelope)
		if err != nil || !fresh {
			return err
		}
		lines, err := heldLines(ctx, tx, orderID)
		if err != nil {
			return err
		}
		for _, line := range lines {
			if _, err := tx.Exec(ctx,
				`UPDATE stock SET on_hand = on_hand - $2, held = greatest(held - $2, 0) WHERE sku = $1`,
				line.SKU, line.Quantity); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE holds SET state = 'COMMITTED', updated_at = now() WHERE order_id = $1`, orderID); err != nil {
			return err
		}
		logger.Info("stock.committed", slog.String("event", "stock.committed"))
		return nil
	})
}

// canHold is the whole availability rule: stock already promised to another
// open saga is not available, even though it is still on the shelf.
func canHold(onHand, held, wanted int) bool {
	return onHand-held >= wanted
}

func heldLines(ctx context.Context, tx pgx.Tx, orderID string) ([]platform.Line, error) {
	var body []byte
	if err := tx.QueryRow(ctx,
		`SELECT lines FROM holds WHERE order_id = $1 AND state = 'HELD' FOR UPDATE`, orderID).Scan(&body); err != nil {
		return nil, nil // nothing held: the compensation has already run
	}
	var lines []platform.Line
	return lines, json.Unmarshal(body, &lines)
}
