package mtime

import "time"

// Now returns the current time.
var Now = func() time.Time { return time.Now() }

// NowForce sets the time returned by Now to t.
func NowForce(t time.Time) {
	Now = func() time.Time { return t }
}

// NowReset makes Now returns the current time again.
func NowReset() {
	Now = func() time.Time { return time.Now() }
}
