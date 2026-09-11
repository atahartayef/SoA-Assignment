package platform

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewEnvelopeFillsThePlatformFields(t *testing.T) {
	envelope, err := NewEnvelope("order-service", "OrderPlaced", "corr-1", "cause-1",
		OrderPlaced{OrderID: "o-1", CustomerID: "MT-8820", TotalCents: 11450, Currency: "EUR"})
	if err != nil {
		t.Fatal(err)
	}
	if envelope.EventID == "" {
		t.Error("every event needs an ID: it is the de-duplication key on both sides")
	}
	if envelope.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", envelope.SchemaVersion)
	}
	if envelope.CorrelationID != "corr-1" || envelope.CausationID != "cause-1" {
		t.Error("the correlation and causation IDs must survive into the envelope")
	}
	if time.Since(envelope.OccurredAt) > time.Minute {
		t.Error("occurred_at should be the moment the event was made")
	}
	var back OrderPlaced
	if err := json.Unmarshal(envelope.Data, &back); err != nil || back.TotalCents != 11450 {
		t.Errorf("the payload did not round-trip: %v %+v", err, back)
	}
}

func TestEnvelopeIDsAreUnique(t *testing.T) {
	first, _ := NewEnvelope("order-service", "OrderPlaced", "c", "", OrderPlaced{})
	second, _ := NewEnvelope("order-service", "OrderPlaced", "c", "", OrderPlaced{})
	if first.EventID == second.EventID {
		t.Error("two events must not share an ID, or one would be dropped as a duplicate")
	}
}
