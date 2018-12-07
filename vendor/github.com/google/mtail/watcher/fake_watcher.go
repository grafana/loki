// Copyright 2015 Google Inc. All Rights Reserved.
// This file is available under the Apache license.

package watcher

import (
	"path"
	"sync"

	"github.com/golang/glog"
	"github.com/pkg/errors"
)

// FakeWatcher implements an in-memory Watcher.
type FakeWatcher struct {
	watchesMu sync.RWMutex
	watches   map[string]int

	eventsMu sync.RWMutex // locks events and isClosed
	events   []chan Event

	isClosed bool
}

// NewFakeWatcher returns a fake Watcher for use in tests.
func NewFakeWatcher() *FakeWatcher {
	return &FakeWatcher{
		watches: make(map[string]int)}
}

// Add adds a watch to the FakeWatcher
func (w *FakeWatcher) Add(name string, handle int) error {
	w.eventsMu.RLock()
	if handle > len(w.events) {
		return errors.Errorf("no such event handle %d", handle)
	}
	w.watchesMu.Lock()
	w.watches[name] = handle
	w.watchesMu.Unlock()
	w.eventsMu.RUnlock()
	return nil
}

// Close closes down the FakeWatcher
func (w *FakeWatcher) Close() error {
	w.eventsMu.Lock()
	defer w.eventsMu.Unlock()
	if w.isClosed {
		return nil
	}
	for _, c := range w.events {
		close(c)
	}
	w.isClosed = true
	return nil
}

// Remove removes a watch from the FakeWatcher
func (w *FakeWatcher) Remove(name string) error {
	w.watchesMu.Lock()
	delete(w.watches, name)
	w.watchesMu.Unlock()
	return nil
}

// Events returns a new channel of messages.
func (w *FakeWatcher) Events() (int, <-chan Event) {
	w.eventsMu.Lock()
	defer w.eventsMu.Unlock()
	if w.isClosed {
		panic("closed")
	}
	ch := make(chan Event, 1)
	handle := len(w.events)
	w.events = append(w.events, ch)
	return handle, ch
}

// InjectCreate lets a test inject a fake creation event.
func (w *FakeWatcher) InjectCreate(name string) {
	dirname := path.Dir(name)
	w.watchesMu.RLock()
	h, dirWatched := w.watches[dirname]
	w.watchesMu.RUnlock()
	if !dirWatched {
		glog.Warningf("not watching %s to see %s", dirname, name)
		return
	}
	w.eventsMu.RLock()
	w.events[h] <- Event{Create, name}
	w.eventsMu.RUnlock()
	if err := w.Add(name, h); err != nil {
		glog.Warning(err)
	}
}

// InjectUpdate lets a test inject a fake update event.
func (w *FakeWatcher) InjectUpdate(name string) {
	w.watchesMu.RLock()
	h, watched := w.watches[name]
	w.watchesMu.RUnlock()
	if !watched {
		glog.Warningf("can't update: not watching %s", name)
		return
	}
	w.eventsMu.RLock()
	w.events[h] <- Event{Update, name}
	w.eventsMu.RUnlock()
}

// InjectDelete lets a test inject a fake deletion event.
func (w *FakeWatcher) InjectDelete(name string) {
	w.watchesMu.RLock()
	h, watched := w.watches[name]
	w.watchesMu.RUnlock()
	if !watched {
		glog.Warningf("can't delete: not watching %s", name)
		return
	}
	w.eventsMu.RLock()
	w.events[h] <- Event{Delete, name}
	w.eventsMu.RUnlock()
	if err := w.Remove(name); err != nil {
		glog.Warning(err)
	}
}
