package backoff

import (
	"sync"
	"testing"
	"time"
)

func TestNew_initialState(t *testing.T) {
	b := New()
	if b.current != DefaultInitial {
		t.Errorf("expected initial current=%v, got %v", DefaultInitial, b.current)
	}
	if b.max != DefaultMax {
		t.Errorf("expected max=%v, got %v", DefaultMax, b.max)
	}
	if b.factor != DefaultFactor {
		t.Errorf("expected factor=%v, got %v", DefaultFactor, b.factor)
	}
}

func TestNext_returnsPositive(t *testing.T) {
	b := New()
	for i := 0; i < 20; i++ {
		d := b.Next()
		if d <= 0 {
			t.Errorf("iteration %d: Next() returned non-positive duration: %v", i, d)
		}
	}
}

func TestNext_firstCallNearInitial(t *testing.T) {
	b := New()
	d := b.Next()
	lower := time.Duration(float64(DefaultInitial) * (1 - jitterFraction))
	upper := time.Duration(float64(DefaultInitial) * (1 + jitterFraction))
	if d < lower || d > upper {
		t.Errorf("first Next()=%v, expected in [%v, %v]", d, lower, upper)
	}
}

func TestNext_advancesInternalCounter(t *testing.T) {
	b := New()
	_ = b.Next() // consumes 2s slot, advances current to 4s
	expected := time.Duration(float64(DefaultInitial) * DefaultFactor)
	if b.current != expected {
		t.Errorf("after first Next(), expected current=%v, got %v", expected, b.current)
	}
}

func TestNext_cappedAtMax(t *testing.T) {
	b := New()
	// Exhaust all doublings until we hit the cap.
	for i := 0; i < 30; i++ {
		b.Next()
	}
	if b.current != DefaultMax {
		t.Errorf("expected current capped at DefaultMax=%v, got %v", DefaultMax, b.current)
	}
}

func TestNext_valuesNeverExceedMaxPlusJitter(t *testing.T) {
	b := New()
	upper := time.Duration(float64(DefaultMax) * (1.0 + jitterFraction + 0.01))
	for i := 0; i < 50; i++ {
		d := b.Next()
		if d > upper {
			t.Errorf("iteration %d: Next()=%v exceeds max+jitter bound (%v)", i, d, upper)
		}
	}
}

func TestReset_restoresInitial(t *testing.T) {
	b := New()
	for i := 0; i < 8; i++ {
		b.Next()
	}
	b.Reset()
	if b.current != DefaultInitial {
		t.Errorf("after Reset, expected current=%v, got %v", DefaultInitial, b.current)
	}
}

func TestNext_afterReset_nearInitial(t *testing.T) {
	b := New()
	for i := 0; i < 8; i++ {
		b.Next()
	}
	b.Reset()
	d := b.Next()
	lower := time.Duration(float64(DefaultInitial) * (1 - jitterFraction))
	upper := time.Duration(float64(DefaultInitial) * (1 + jitterFraction))
	if d < lower || d > upper {
		t.Errorf("after Reset, Next()=%v, expected in [%v, %v]", d, lower, upper)
	}
}

func TestNext_growsMonotonically_base(t *testing.T) {
	// The base (without jitter) must double each step until max.
	b := New()
	expected := DefaultInitial
	for i := 0; i < 10; i++ {
		b.Next()
		next := time.Duration(float64(expected) * DefaultFactor)
		if next > DefaultMax {
			next = DefaultMax
		}
		if b.current != next {
			t.Errorf("step %d: expected current=%v, got %v", i, next, b.current)
		}
		expected = next
	}
}

// TestConcurrentSafe runs Next and Reset concurrently to verify the mutex works.
func TestConcurrentSafe(t *testing.T) {
	b := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = b.Next()
			}
		}()
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				b.Reset()
			}
		}()
	}
	wg.Wait()
	// If we get here without a data-race detector complaint, the mutex works.
}
