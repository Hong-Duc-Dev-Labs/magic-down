package engine

import (
	"sync"
	"time"
)

type speedSample struct {
	timestamp time.Time
	bytes     int64
}

// speedCalculator computes smoothed transfer rate and estimated time of arrival.
type speedCalculator struct {
	mu          sync.Mutex
	samples     []speedSample
	maxSamples  int
	window      time.Duration
	lastBytes   int64
	cachedSpeed int64
}

func newSpeedCalculator(initialBytes int64) *speedCalculator {
	now := time.Now()
	return &speedCalculator{
		samples: []speedSample{
			{timestamp: now, bytes: initialBytes},
		},
		maxSamples: 10,
		window:     3 * time.Second,
		lastBytes:  initialBytes,
	}
}

// Record updates the calculator with the latest total downloaded bytes and returns (speed, eta).
func (sc *speedCalculator) Record(currentBytes, totalBytes int64) (speed int64, eta int64) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	now := time.Now()
	sc.lastBytes = currentBytes
	sc.samples = append(sc.samples, speedSample{timestamp: now, bytes: currentBytes})

	// Trim samples older than the rolling window
	cutoff := now.Add(-sc.window)
	filtered := sc.samples[:0]
	for _, s := range sc.samples {
		if s.timestamp.After(cutoff) || len(filtered) == 0 {
			filtered = append(filtered, s)
		}
	}
	if len(filtered) > sc.maxSamples {
		filtered = filtered[len(filtered)-sc.maxSamples:]
	}
	sc.samples = filtered

	if len(sc.samples) >= 2 {
		first := sc.samples[0]
		last := sc.samples[len(sc.samples)-1]
		elapsed := last.timestamp.Sub(first.timestamp).Seconds()
		deltaBytes := last.bytes - first.bytes

		if elapsed > 0.1 && deltaBytes >= 0 {
			sc.cachedSpeed = int64(float64(deltaBytes) / elapsed)
		}
	}

	speed = sc.cachedSpeed
	if speed > 0 && totalBytes > currentBytes {
		remaining := totalBytes - currentBytes
		eta = remaining / speed
	} else {
		eta = 0
	}

	return speed, eta
}

// Current returns the last calculated speed.
func (sc *speedCalculator) Current() int64 {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.cachedSpeed
}
