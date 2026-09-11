package platform

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Subjects. A subject is part of the contract between services, so they are
// declared once and nowhere else.
const (
	SubjectOrderPlaced       = "maltashop.order.placed"
	SubjectOrderConfirmed    = "maltashop.order.confirmed"
	SubjectOrderCancelled    = "maltashop.order.cancelled"
	SubjectInventoryReserved = "maltashop.inventory.reserved"
	SubjectInventoryRejected = "maltashop.inventory.rejected"
	SubjectInventoryReleased = "maltashop.inventory.released"
	SubjectPaymentAuthorised = "maltashop.payment.authorised"
	SubjectPaymentDeclined   = "maltashop.payment.declined"

	StreamName    = "MALTASHOP"
	StreamSubject = "maltashop.>"
)

// Envelope is the shape of every message on the stream. The fields outside
// Data are the ones the platform guarantees; Data is owned by the publisher.
type Envelope struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	CorrelationID string          `json:"correlation_id"`
	CausationID   string          `json:"causation_id,omitempty"`
	Producer      string          `json:"producer"`
	Data          json.RawMessage `json:"data"`
}

type Line struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

type OrderPlaced struct {
	OrderID    string `json:"order_id"`
	CustomerID string `json:"customer_id"`
	TotalCents int64  `json:"total_cents"`
	Currency   string `json:"currency"`
	Lines      []Line `json:"lines"`
}

type OrderOutcome struct {
	OrderID    string `json:"order_id"`
	CustomerID string `json:"customer_id"`
	TotalCents int64  `json:"total_cents,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Lines      []Line `json:"lines,omitempty"`
}

type StockOutcome struct {
	OrderID string `json:"order_id"`
	Reason  string `json:"reason,omitempty"`
}

type PaymentOutcome struct {
	OrderID      string `json:"order_id"`
	PSPReference string `json:"psp_reference,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// NewEnvelope builds a message with the platform fields already filled in.
func NewEnvelope(producer, eventType, correlationID, causationID string, data any) (Envelope, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		EventID:       uuid.NewString(),
		EventType:     eventType,
		SchemaVersion: 1,
		OccurredAt:    time.Now().UTC(),
		CorrelationID: correlationID,
		CausationID:   causationID,
		Producer:      producer,
		Data:          body,
	}, nil
}
