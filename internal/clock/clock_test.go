package clock

import (
	"testing"
	"time"
)

func TestRealClock_Now(t *testing.T) {
	c := RealClock{}
	before := time.Now()
	got := c.Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Fatalf("RealClock.Now() out of range: %v", got)
	}
}

func TestMockClock_Now(t *testing.T) {
	fixed := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &MockClock{T: fixed}
	if got := c.Now(); got != fixed {
		t.Fatalf("expected %v, got %v", fixed, got)
	}
}
