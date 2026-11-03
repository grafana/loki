package test

import "sync"

// CapturingLogger is a log.Logger that records every call, for tests asserting a specific log fired.
// It is safe for concurrent use.
type CapturingLogger struct {
	mu   sync.Mutex
	logs [][]interface{}
}

func (l *CapturingLogger) Log(keyvals ...interface{}) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.logs = append(l.logs, keyvals)
	return nil
}

// Entries returns a copy of every call recorded so far.
func (l *CapturingLogger) Entries() [][]interface{} {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([][]interface{}(nil), l.logs...)
}
