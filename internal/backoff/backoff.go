// Package backoff implements exponential backoff with random jitter.
// It is safe for concurrent use.
package backoff

import (
	"math/rand"
	"sync"
	"time"
)

const (
	// DefaultInitial is the first backoff duration.
	DefaultInitial = 2 * time.Second
	// DefaultMax caps the backoff duration.
	DefaultMax = 5 * time.Minute
	// DefaultFactor is the multiplier applied after each failure.
	DefaultFactor = 2.0
	// jitterFraction controls the maximum random offset (±25 % of current duration).
	jitterFraction = 0.25
)

// Backoff tracks exponential backoff state.
// Zero value is not valid; use New().
type Backoff struct {
	mu      sync.Mutex
	initial time.Duration
	max     time.Duration
	factor  float64
	current time.Duration
}

// New creates a Backoff with the package defaults:
// start 2 s, max 5 m, factor 2×, ±25 % jitter.
func New() *Backoff {
	return &Backoff{
		initial: DefaultInitial,
		max:     DefaultMax,
		factor:  DefaultFactor,
		current: DefaultInitial,
	}
}

// Next returns the next wait duration (with jitter applied) and advances the
// internal counter for the subsequent call.
// Call Reset after a stable connection to start over from the initial value.
func (b *Backoff) Next() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	base := b.current

	// Apply ±jitterFraction random jitter.
	maxJitter := float64(base) * jitterFraction
	jitter := time.Duration((rand.Float64()*2 - 1) * maxJitter)
	d := base + jitter
	if d < 0 {
		d = b.initial
	}

	// Advance internal counter for next call.
	next := time.Duration(float64(b.current) * b.factor)
	if next > b.max {
		next = b.max
	}
	b.current = next

	return d
}

// Reset returns the backoff to its initial value.
// Call this after a connection has been stable long enough to be considered healthy.
func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = b.initial
}
