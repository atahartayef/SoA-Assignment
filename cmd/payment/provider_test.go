package main

import (
	"strings"
	"testing"

	"github.com/maltashop/lean-platform/internal/platform"
)

func TestCallProvider(t *testing.T) {
	app := &server{cfg: platform.Config{DeclineOver: 75000}} // EUR 750

	if reference, reason := app.callProvider(11450); reason != "" || !strings.HasPrefix(reference, "psp_") {
		t.Fatalf("an ordinary basket should be authorised, got reference %q reason %q", reference, reason)
	}
	if _, reason := app.callProvider(89900); reason != "card_limit_exceeded" {
		t.Fatalf("a basket over the limit should be declined, got %q", reason)
	}
	if _, reason := app.callProvider(75000); reason != "" {
		t.Fatalf("the limit itself should be authorised, got %q", reason)
	}
}
