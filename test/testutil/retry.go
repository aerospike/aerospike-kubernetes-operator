package testutil

import "time"

// RetryDefaults holds common retry settings for tests that need to poll or retry.
// Shared so envtests, cluster, and other packages use the same intervals.
type RetryDefaults struct {
	Interval time.Duration
	Timeout  time.Duration
}

// DefaultRetry returns retry defaults used across test packages.
func DefaultRetry() RetryDefaults {
	return RetryDefaults{
		Interval: 1 * time.Second,
		Timeout:  30 * time.Second,
	}
}
