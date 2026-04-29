// Package clock abstracts time for testability.
package clock

import "time"

// Clock provides the current time.
type Clock interface {
	Now() time.Time
}

// RealClock uses the system clock.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }

// MockClock is a fake clock for testing.
type MockClock struct {
	T time.Time
}

// Now returns the mock time.
func (m *MockClock) Now() time.Time { return m.T }

var (
	_ Clock = (*RealClock)(nil)
	_ Clock = (*MockClock)(nil)
)
