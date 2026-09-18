package catalog

import (
	"context"
	"sync"
	"time"
)

type tokenBucket struct {
	mu           sync.Mutex
	tokens       float64
	max          float64
	refillPerSec float64
	last         time.Time
	now          func() time.Time
}

func newTokenBucket(refillPerSec float64, burst int, now func() time.Time) *tokenBucket {
	return &tokenBucket{
		tokens:       float64(burst),
		max:          float64(burst),
		refillPerSec: refillPerSec,
		last:         now(),
		now:          now,
	}
}

func (b *tokenBucket) wait(ctx context.Context) error {
	for {
		wait, ok := b.take()
		if ok {
			return nil
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (b *tokenBucket) take() (time.Duration, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	elapsed := now.Sub(b.last)
	if elapsed > 0 {
		b.tokens = min(b.max, b.tokens+elapsed.Seconds()*b.refillPerSec)
		b.last = now
	}

	if b.tokens >= 1 {
		b.tokens--
		return 0, true
	}

	deficit := 1 - b.tokens
	return time.Duration(deficit / b.refillPerSec * float64(time.Second)), false
}

type circuitState int

const (
	circuitClosed circuitState = iota
	circuitOpen
	circuitHalfOpen
)

type circuitBreaker struct {
	mu sync.Mutex

	state            circuitState
	consecutiveFails int
	trialInFlight    bool

	failureThreshold int
	openDuration     time.Duration
	openedAt         time.Time

	now func() time.Time
}

func newCircuitBreaker(failureThreshold int, openDuration time.Duration, now func() time.Time) *circuitBreaker {
	return &circuitBreaker{
		failureThreshold: failureThreshold,
		openDuration:     openDuration,
		now:              now,
	}
}

func (b *circuitBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case circuitOpen:
		if b.now().Sub(b.openedAt) < b.openDuration {
			return false
		}
		b.state = circuitHalfOpen
		b.trialInFlight = true
		return true
	case circuitHalfOpen:
		if b.trialInFlight {
			return false
		}
		b.trialInFlight = true
		return true
	default:
		return true
	}
}

func (b *circuitBreaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.state = circuitClosed
	b.consecutiveFails = 0
	b.trialInFlight = false
}

func (b *circuitBreaker) recordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.consecutiveFails++
	b.trialInFlight = false

	if b.state == circuitHalfOpen || b.consecutiveFails >= b.failureThreshold {
		b.state = circuitOpen
		b.openedAt = b.now()
	}
}
