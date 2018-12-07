// Copyright 2015 Google Inc. All Rights Reserved.
// This file is available under the Apache license.

// Package watcher provides a way of watching for filesystem events and
// notifying observers when they occur.
package watcher

type OpType int

const (
	Create OpType = iota
	Update
	Delete
)

// Event is a generalisation of events sent from the watcher to its listeners.
type Event struct {
	Op       OpType
	Pathname string
}

// Watcher describes an interface for filesystem watching.
type Watcher interface {
	Add(name string, handle int) error
	Close() error
	Remove(name string) error
	Events() (handle int, ch <-chan Event)
}
