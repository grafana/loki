package atomicutil

import "sync/atomic"

// String is an atomic string. sync/atomic has no String wrapper.
type String struct {
	v atomic.Pointer[string]
}

// NewString returns a pointer to a String holding v.
func NewString(v string) *String {
	x := new(String)
	if v != "" {
		x.Store(v)
	}
	return x
}

// Load atomically loads the wrapped string.
func (s *String) Load() string {
	if p := s.v.Load(); p != nil {
		return *p
	}
	return ""
}

// Store atomically stores v.
func (s *String) Store(v string) {
	s.v.Store(&v)
}

// Swap atomically stores v and returns the previous value.
func (s *String) Swap(v string) string {
	old := s.v.Swap(&v)
	if old == nil {
		return ""
	}
	return *old
}

// CompareAndSwap atomically compares the current value with old and, if they
// match, stores new. It returns whether the swap was performed.
func (s *String) CompareAndSwap(old, new string) bool {
	for {
		cur := s.v.Load()
		var curVal string
		if cur != nil {
			curVal = *cur
		}
		if curVal != old {
			return false
		}
		if s.v.CompareAndSwap(cur, &new) {
			return true
		}
	}
}
