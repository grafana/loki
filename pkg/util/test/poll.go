package test

import (
	"reflect"
	"testing"
	"time"
)

// Poll repeatedly evaluates condition until we either timeout, or it succeeds.
func Poll(t testing.TB, d time.Duration, want interface{}, have func() interface{}) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		if time.Now().After(deadline) {
			break
		}
		if reflect.DeepEqual(want, have()) {
			return
		}
		time.Sleep(d / 100)
	}
	h := have()
	if !reflect.DeepEqual(want, h) {
		t.Fatalf("expected %v, got %v", want, h)
	}
}
