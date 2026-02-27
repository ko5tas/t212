package api

import "time"

// RateLimitInfo contains the parsed x-ratelimit-* response headers.
type RateLimitInfo struct {
	Remaining int
	Reset     time.Time
}
