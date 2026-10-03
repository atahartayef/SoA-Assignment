// Package contracts holds the consumer-driven contract tests.
//
// notification-service is the consumer and owns order-confirmed.v1.schema.json.
// order-service is the provider. Both pipelines run this test, so the provider
// cannot drop, rename or retype a field the consumer depends on without a red build.
package contracts

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/maltashop/lean-platform/internal/platform"
)

func load(t *testing.T) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	file, err := os.Open("order-confirmed.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := compiler.AddResource("order-confirmed.v1.schema.json", file); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("order-confirmed.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

// sample is built the same way the Order service builds the real event, so the
// test fails if the production code and the contract drift apart.
func sample(t *testing.T) map[string]any {
	t.Helper()
	envelope, err := platform.NewEnvelope("order-service", "OrderConfirmed",
		"5b1f0c6e-2e45-4f4b-9f0a-6a7e9b1d2c33", "0d2d0b2f-1111-4a4a-8888-9f0a6a7e9b1d",
		platform.OrderOutcome{
			OrderID:    "c4d1e2f3-4a5b-4c6d-8e9f-0a1b2c3d4e5f",
			CustomerID: "MT-8820",
			TotalCents: 19190,
			Lines:      []platform.Line{{SKU: "TOASTER-2S", Quantity: 2}, {SKU: "MIXER-900", Quantity: 1}},
		})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(body, &generic); err != nil {
		t.Fatal(err)
	}
	return generic
}

func TestPublishedEventSatisfiesTheConsumerContract(t *testing.T) {
	if err := load(t).Validate(sample(t)); err != nil {
		t.Fatalf("the event the Order service publishes breaks the contract: %v", err)
	}
}

func TestContractFailsWhenTheCorrelationIDIsDropped(t *testing.T) {
	event := sample(t)
	delete(event, "correlation_id")
	if err := load(t).Validate(event); err == nil {
		t.Fatal("an event without a correlation ID should not pass: tracing would break")
	}
}

func TestContractFailsWhenTheTotalBecomesFormattedText(t *testing.T) {
	event := sample(t)
	event["data"].(map[string]any)["total_cents"] = "191.90"
	if err := load(t).Validate(event); err == nil {
		t.Fatal("a formatted total should not pass: the consumer does arithmetic on it")
	}
}
