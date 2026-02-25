package backoff

import (
	"math/rand"
	"sync"
	"time"
)

const (
	DefaultInitial = 2 * time.Second
	DefaultMax     = 5 * time.Minute
	DefaultFactor  = 2.0
	jitterFraction = 0.25
)

type Backoff struct {
	mu      sync.Mutex
	initial time.Duration
	max     time.Duration
	factor  float64
	current time.Duration
}

func New() *Backoff {
	return &Backoff{
		initial: DefaultInitial,
		max:     DefaultMax,
		factor:  DefaultFactor,
		current: DefaultInitial,
	}
}

func (b *Backoff) Next() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	base := b.current

	maxJitter := float64(base) * jitterFraction
	jitter := time.Duration((rand.Float64()*2 - 1) * maxJitter)
	d := base + jitter
	if d < 0 {
		d = b.initial
	}

	next := time.Duration(float64(b.current) * b.factor)
	if next > b.max {
		next = b.max
	}
	b.current = next

	return d
}

func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = b.initial
}
