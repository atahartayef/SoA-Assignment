package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Stream wraps the JetStream handle every service uses.
type Stream struct {
	Conn   *nats.Conn
	JS     jetstream.JetStream
	logger *slog.Logger
	name   string
}

// Connect opens the connection and makes sure the single MALTASHOP stream exists.
// One stream with subject filters, rather than one stream per event, keeps the
// operational surface small, which is the whole point of this design.
func Connect(ctx context.Context, cfg Config, logger *slog.Logger) (*Stream, error) {
	conn, err := nats.Connect(cfg.NatsURL,
		nats.Name(cfg.ServiceName),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			logger.Warn("nats.disconnected", slog.String("error", fmt.Sprint(err)))
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) { logger.Info("nats.reconnected") }),
	)
	if err != nil {
		return nil, err
	}
	js, err := jetstream.New(conn)
	if err != nil {
		return nil, err
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       StreamName,
		Subjects:   []string{StreamSubject},
		Storage:    jetstream.FileStorage,
		Retention:  jetstream.LimitsPolicy,
		MaxAge:     30 * 24 * time.Hour,
		Duplicates: 2 * time.Minute, // JetStream drops a repeated Nats-Msg-Id inside this window
		Replicas:   1,               // three in production, across two availability zones
	})
	if err != nil {
		return nil, err
	}
	return &Stream{Conn: conn, JS: js, logger: logger, name: cfg.ServiceName}, nil
}

// Publish sends one envelope. The event ID is sent as Nats-Msg-Id, so a retry of
// the same publish inside the duplicate window is dropped by the server rather
// than delivered twice.
func (s *Stream) Publish(ctx context.Context, subject string, envelope Envelope) error {
	body, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	message := &nats.Msg{Subject: subject, Data: body, Header: nats.Header{}}
	message.Header.Set(jetstream.MsgIDHeader, envelope.EventID)
	message.Header.Set("Correlation-Id", envelope.CorrelationID)
	if _, err = s.JS.PublishMsg(ctx, message); err != nil {
		return err
	}
	Ctx(ctx, s.logger).Info("event.published",
		slog.String("event", "event.published"),
		slog.String("subject", subject),
		slog.String("event_type", envelope.EventType),
		slog.String("event_id", envelope.EventID))
	return nil
}

// Handler acts on one message. Returning an error leaves the message unacknowledged,
// so JetStream redelivers it; the consumer-side de-duplication makes that safe.
type Handler func(ctx context.Context, envelope Envelope) error

// Subscribe creates a durable consumer filtered to the given subjects and runs
// handler for every message. The consumer name is the service name, so a restart
// picks up exactly where the service left off.
func (s *Stream) Subscribe(ctx context.Context, durable string, subjects []string, handle Handler) error {
	consumer, err := s.JS.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:        durable,
		FilterSubjects: subjects,
		AckPolicy:      jetstream.AckExplicitPolicy,
		DeliverPolicy:  jetstream.DeliverAllPolicy,
		MaxDeliver:     6,
		AckWait:        30 * time.Second,
		BackOff:        []time.Duration{time.Second, 3 * time.Second, 10 * time.Second},
	})
	if err != nil {
		return err
	}
	_, err = consumer.Consume(func(message jetstream.Msg) {
		var envelope Envelope
		if err := json.Unmarshal(message.Data(), &envelope); err != nil {
			s.logger.Error("event.unreadable", slog.String("error", err.Error()))
			_ = message.Term() // a message that cannot be parsed will never parse: stop redelivering
			return
		}
		messageCtx := WithCorrelation(context.Background(), envelope.CorrelationID)
		if err := handle(messageCtx, envelope); err != nil {
			Ctx(messageCtx, s.logger).Error("event.failed",
				slog.String("event", "event.failed"),
				slog.String("event_type", envelope.EventType),
				slog.String("error", err.Error()))
			_ = message.Nak()
			return
		}
		_ = message.Ack()
	})
	if err != nil {
		return err
	}
	s.logger.Info("consumer.started", slog.String("event", "consumer.started"),
		slog.String("durable", durable), slog.Any("subjects", subjects))
	return nil
}
