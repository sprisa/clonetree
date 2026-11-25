package main

import "testing"

func TestCountFormatting(t *testing.T) {
	for n, want := range map[uint64]string{0: "0", 999: "999", 1000: "1,000", 400000: "400,000", 1234567: "1,234,567"} {
		if got := groupedCount(n); got != want {
			t.Errorf("%d: got %q, want %q", n, got, want)
		}
	}
	for n, want := range map[float64]string{12: "12", 1500: "1.5k", 4100000: "4.1M"} {
		if got := countString(n); got != want {
			t.Errorf("%v: got %q, want %q", n, got, want)
		}
	}
}

func TestValidID(t *testing.T) {
	for _, id := range []string{"a", "A", "1", "test-123", "feature_name", "A_9-"} {
		if !validID(id) {
			t.Errorf("rejected valid ID %q", id)
		}
	}
	for _, id := range []string{"", "_first", "-first", "a.b", "a/b", "a\\b", "a\n", "a b", "é", "a日本"} {
		if validID(id) {
			t.Errorf("accepted invalid ID %q", id)
		}
	}
}
