package main

import "testing"

func TestCanHold(t *testing.T) {
	cases := []struct {
		name   string
		onHand int
		held   int
		wanted int
		want   bool
	}{
		{"free stock is available", 9, 0, 3, true},
		{"stock held by an open saga is not available", 9, 7, 3, false},
		{"the last unit may be taken", 9, 8, 1, true},
		{"a pre-order item can never be held", 0, 0, 1, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := canHold(test.onHand, test.held, test.wanted); got != test.want {
				t.Fatalf("canHold(%d, %d, %d) = %v, want %v",
					test.onHand, test.held, test.wanted, got, test.want)
			}
		})
	}
}
