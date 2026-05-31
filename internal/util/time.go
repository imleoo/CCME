package util

import "time"

// Clock abstracts time sourcing so tests can inject a controllable now().
type Clock interface {
	NowMillis() int64
}

// SystemClock is the real-time clock backed by time.Now().
type SystemClock struct{}

func (SystemClock) NowMillis() int64 { return time.Now().UnixMilli() }

// FixedClock returns a fixed timestamp - useful for deterministic tests.
type FixedClock struct{ T int64 }

func (c *FixedClock) NowMillis() int64 { return c.T }

// NowMillis is a convenience wrapper around time.Now().UnixMilli().
func NowMillis() int64 { return time.Now().UnixMilli() }

// AgeSeconds returns how many seconds have elapsed since the given epoch millis.
func AgeSeconds(tsMillis int64) float64 {
	return float64(NowMillis()-tsMillis) / 1000.0
}

// AgeSecondsAt returns AgeSeconds relative to a caller-provided "now".
func AgeSecondsAt(tsMillis, nowMillis int64) float64 {
	return float64(nowMillis-tsMillis) / 1000.0
}
