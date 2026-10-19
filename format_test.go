package main

import (
	"testing"
	"time"
)

func TestProgressDurationFormatting(t *testing.T) {
	for _, d := range []time.Duration{
		0, 100 * time.Millisecond, 1300 * time.Millisecond,
		time.Minute, time.Minute + 1500*time.Millisecond,
		time.Hour, time.Hour + 2*time.Minute + 3400*time.Millisecond,
	} {
		var buffer [64]byte
		if got := string(appendDuration(buffer[:0], d)); got != d.String() {
			t.Errorf("%v: got %q, want %q", d, got, d.String())
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
