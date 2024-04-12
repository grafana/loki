package compactor

import "sync"

type lockWaiterChan chan struct{}

type tableLocker struct {
	lockedTables    map[string]lockWaiterChan
	lockedTablesMtx sync.RWMutex
}

func newTableLocker() *tableLocker {
	return &tableLocker{
		lockedTables: map[string]lockWaiterChan{},
	}
}

// lockTable attempts to lock a table. It returns true if the lock gets acquired for the caller.
// It also returns a channel which the caller can watch to detect unlocking of table if it was already locked by some other caller.
func (t *tableLocker) lockTable(tableName string) (bool, <-chan struct{}) {
	locked := false

	t.lockedTablesMtx.RLock()
	c, ok := t.lockedTables[tableName]
	t.lockedTablesMtx.RUnlock()
	if ok {
		return false, c
	}

	t.lockedTablesMtx.Lock()
	defer t.lockedTablesMtx.Unlock()

	c, ok = t.lockedTables[tableName]
	if !ok {
		t.lockedTables[tableName] = make(chan struct{})
		c = t.lockedTables[tableName]
		locked = true
	}

	return locked, c
}

func (t *tableLocker) unlockTable(tableName string) {
	t.lockedTablesMtx.Lock()
	defer t.lockedTablesMtx.Unlock()

	c, ok := t.lockedTables[tableName]
	if ok {
		close(c)
	}
	delete(t.lockedTables, tableName)
}
