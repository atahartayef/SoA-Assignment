package main

import "testing"

// The saga's state machine is the one piece of order logic worth testing on its
// own: it is pure, and it is what stops a late or repeated event corrupting an order.
func TestNext(t *testing.T) {
	cases := []struct {
		name      string
		from      string
		event     string
		want      string
		permitted bool
	}{
		{"stock is held", "PENDING", "InventoryReserved", "STOCK_RESERVED", true},
		{"nothing in stock", "PENDING", "InventoryRejected", "REJECTED", true},
		{"payment succeeds", "STOCK_RESERVED", "PaymentAuthorised", "CONFIRMED", true},
		{"payment fails, compensation begins", "STOCK_RESERVED", "PaymentDeclined", "COMPENSATING", true},
		{"the hold comes back", "COMPENSATING", "InventoryReleased", "CANCELLED", true},
		{"a repeated authorisation changes nothing", "CONFIRMED", "PaymentAuthorised", "", false},
		{"payment cannot be taken before stock is held", "PENDING", "PaymentAuthorised", "", false},
		{"a cancelled order cannot be revived", "CANCELLED", "InventoryReserved", "", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, ok := next(test.from, test.event)
			if ok != test.permitted {
				t.Fatalf("next(%q, %q) permitted = %v, want %v", test.from, test.event, ok, test.permitted)
			}
			if ok && got != test.want {
				t.Fatalf("next(%q, %q) = %q, want %q", test.from, test.event, got, test.want)
			}
		})
	}
}
