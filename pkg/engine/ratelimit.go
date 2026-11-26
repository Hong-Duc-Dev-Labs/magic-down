package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// rateLimiter is a single token bucket shared by every segment, single-stream
// body and HLS fragment of all active tasks. A rate of 0 means unlimited.
//
// The bucket is not reserved up front: each waiter recomputes the shortfall
// from the tokens currently available and sleeps at most rateCheckInterval, so
// changing the configured rate is observed within that interval and pause or
// cancel wakes the waiter immediately.
type rateLimiter struct {
	rate atomic.Int64 // bytes per second; 0 = unlimited

	mu       sync.Mutex
	tokens   float64
	maxBurst float64
	last     time.Time
}

const rateCheckInterval = 250 * time.Millisecond

func newRateLimiter(bytesPerSecond int64) *rateLimiter {
	l := &rateLimiter{}
	l.rate.Store(bytesPerSecond)
	return l
}

// setRate updates the ceiling. It never blocks in-flight waiters.
func (l *rateLimiter) setRate(bytesPerSecond int64) {
	if bytesPerSecond < 0 {
		bytesPerSecond = 0
	}
	l.rate.Store(bytesPerSecond)
}

func (l *rateLimiter) currentRate() int64 {
	if l == nil {
		return 0
	}
	return l.rate.Load()
}

// waitOrderedLimiters enforces per-task rate limit first, then global rate
// limit. Both buckets are non-reserving so deadlock is impossible.
func waitOrderedLimiters(ctx context.Context, n int64, taskLimiter, globalLimiter *rateLimiter) error {
	if n <= 0 {
		return nil
	}
	if taskLimiter != nil {
		if err := taskLimiter.wait(ctx, n); err != nil {
			return err
		}
	}
	if globalLimiter != nil {
		if err := globalLimiter.wait(ctx, n); err != nil {
			return err
		}
	}
	return nil
}

// cappedReadBufferSize returns min(baseSize, effectiveRate/4) when throttled
// low so reads stay smooth without choppy spikes. Always returns >= 1024.
func cappedReadBufferSize(baseSize int, taskLimiter, globalLimiter *rateLimiter) int {
	var effRate int64
	if r := taskLimiter.currentRate(); r > 0 {
		effRate = r
	}
	if r := globalLimiter.currentRate(); r > 0 && (effRate == 0 || r < effRate) {
		effRate = r
	}
	if effRate <= 0 {
		return baseSize
	}
	capped := int(effRate / 4)
	if capped < 1024 {
		capped = 1024
	}
	if capped > baseSize {
		return baseSize
	}
	return capped
}

// wait blocks until n bytes may be transferred at the current rate. It returns
// ctx.Err() when the transfer is paused or cancelled while waiting.
func (l *rateLimiter) wait(ctx context.Context, n int64) error {
	if n <= 0 {
		return nil
	}
	for {
		rate := l.rate.Load()
		if rate <= 0 {
			return nil
		}

		l.mu.Lock()
		now := time.Now()
		if l.last.IsZero() {
			l.last = now
		}
		if elapsed := now.Sub(l.last).Seconds(); elapsed > 0 {
			l.tokens += elapsed * float64(rate)
		}
		l.last = now
		// Ensure burst is large enough to satisfy the largest active read chunk
		// (e.g. 64 KB) even when rate < 256 KB/s, so waiters never livelock.
		if float64(n) > l.maxBurst {
			l.maxBurst = float64(n)
		}
		burst := float64(rate) / 4
		if burst < l.maxBurst {
			burst = l.maxBurst
		}
		if burst < 1 {
			burst = 1
		}
		if l.tokens > burst {
			l.tokens = burst
		}
		if l.tokens >= float64(n) {
			l.tokens -= float64(n)
			l.mu.Unlock()
			return nil
		}
		shortfall := float64(n) - l.tokens
		l.mu.Unlock()

		wait := time.Duration(shortfall / float64(rate) * float64(time.Second))
		if wait > rateCheckInterval {
			wait = rateCheckInterval
		}
		if wait < time.Millisecond {
			wait = time.Millisecond
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
