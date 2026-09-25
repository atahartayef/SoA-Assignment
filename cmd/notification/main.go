// Command notification is the Notification service.
//
// It owns a Valkey keyspace rather than a relational database: the messages it
// writes are short-lived, read by key and never joined, so a key-value store is
// the right shape and costs almost nothing to run. Nothing in checkout waits for
// this service, which is the fix for the March incident.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/maltashop/lean-platform/internal/platform"
)

type notification struct {
	NotificationID string    `json:"notification_id"`
	OrderID        string    `json:"order_id"`
	CustomerID     string    `json:"customer_id"`
	Channel        string    `json:"channel"`
	Template       string    `json:"template"`
	Subject        string    `json:"subject"`
	Body           string    `json:"body"`
	Status         string    `json:"status"`
	CorrelationID  string    `json:"correlation_id"`
	CreatedAt      time.Time `json:"created_at"`
}

type server struct {
	cfg    platform.Config
	redis  *redis.Client
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

	options, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		logger.Error("service.start_failed", slog.String("error", err.Error()))
		return
	}
	client := redis.NewClient(options)
	defer client.Close()

	stream, err := platform.Connect(ctx, cfg, logger)
	if err != nil {
		logger.Error("service.start_failed", slog.String("error", err.Error()))
		return
	}
	defer stream.Conn.Drain()

	app := &server{cfg: cfg, redis: client, logger: logger}
	if err := stream.Subscribe(ctx, "notification-service",
		[]string{platform.SubjectOrderConfirmed, platform.SubjectOrderCancelled}, app.onEvent); err != nil {
		logger.Error("service.start_failed", slog.String("error", err.Error()))
		return
	}

	router := chi.NewRouter()
	router.Use(platform.Correlate)
	platform.Health(router, cfg, func(ctx context.Context) error { return client.Ping(ctx).Err() })
	router.Get("/notifications", app.list)
	if err := platform.Serve(ctx, cfg, router, logger); err != nil {
		logger.Error("service.stopped_with_error", slog.String("error", err.Error()))
	}
}

func (s *server) list(writer http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	pattern := "notification:*"
	if orderID := request.URL.Query().Get("order_id"); orderID != "" {
		pattern = "notification:" + orderID + ":*"
	}
	keys, err := s.redis.Keys(ctx, pattern).Result()
	if err != nil {
		platform.JSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	sort.Strings(keys)
	out := []notification{}
	for _, key := range keys {
		raw, err := s.redis.Get(ctx, key).Result()
		if err != nil {
			continue
		}
		var item notification
		if json.Unmarshal([]byte(raw), &item) == nil {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	platform.JSON(writer, http.StatusOK, out)
}

func (s *server) onEvent(ctx context.Context, envelope platform.Envelope) error {
	var outcome platform.OrderOutcome
	if err := json.Unmarshal(envelope.Data, &outcome); err != nil {
		return err
	}
	ctx = platform.WithOrder(ctx, outcome.OrderID)

	// De-duplication without a database: SET NX on the event ID succeeds once.
	fresh, err := s.redis.SetNX(ctx, "seen:"+envelope.EventID, "1", 48*time.Hour).Result()
	if err != nil {
		return err
	}
	if !fresh {
		platform.Ctx(ctx, s.logger).Info("event.duplicate_ignored",
			slog.String("event", "event.duplicate_ignored"), slog.String("event_type", envelope.EventType))
		return nil
	}

	item := notification{
		NotificationID: uuid.NewString(),
		OrderID:        outcome.OrderID,
		CustomerID:     outcome.CustomerID,
		Channel:        "email",
		Status:         "SENT",
		CorrelationID:  envelope.CorrelationID,
		CreatedAt:      time.Now().UTC(),
	}
	switch envelope.EventType {
	case "OrderConfirmed":
		item.Template = "order-confirmed"
		item.Subject = fmt.Sprintf("MaltaShop order %s confirmed", short(outcome.OrderID))
		item.Body = fmt.Sprintf("Thank you. We have taken EUR %.2f for %d item(s). Your order is on its way.",
			float64(outcome.TotalCents)/100, len(outcome.Lines))
	case "OrderCancelled":
		item.Template = "order-cancelled"
		item.Subject = fmt.Sprintf("MaltaShop order %s could not be completed", short(outcome.OrderID))
		item.Body = fmt.Sprintf("We are sorry: your order was not completed (%s). You have not been charged.",
			outcome.Reason)
	default:
		return nil
	}

	body, err := json.Marshal(item)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("notification:%s:%s", item.OrderID, item.NotificationID)
	if err := s.redis.Set(ctx, key, body, 30*24*time.Hour).Err(); err != nil {
		return err
	}
	platform.Ctx(ctx, s.logger).Info("notification.sent",
		slog.String("event", "notification.sent"),
		slog.String("template", item.Template), slog.String("channel", "email"))
	return nil
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
