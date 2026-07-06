//go:build !synctests

// Package xsync provides Mutex and RWMutex types that are aliases for
// sync.Mutex / sync.RWMutex in normal builds and channel-backed
// reimplementations under the "synctests" build tag. The channel-backed
// versions let testing/synctest treat mutex waits as durably blocked so the
// bubble's virtual clock can advance.
package xsync

import "sync"

type Mutex = sync.Mutex
type RWMutex = sync.RWMutex
