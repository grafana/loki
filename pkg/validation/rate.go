package validation

import "golang.org/x/time/rate"

// RateLimit is a colocated limit & burst config. It largely exists to
// eliminate accidental misconfigurations due to race conditions when
// requesting the limit & burst config sequentially, between which the
// Limits configuration may have updated.
type RateLimit struct {
	Limit rate.Limit
	Burst int
}

var Unlimited = RateLimit{
	Limit: rate.Inf,
	Burst: 0,
}
