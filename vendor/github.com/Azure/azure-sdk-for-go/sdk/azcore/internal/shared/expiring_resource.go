//go:build go1.16
// +build go1.16

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package shared

import (
	"sync"
	"time"
)

// AcquireResource abstracts a method for refreshing an expiring resource.
type AcquireResource func(state interface{}) (newResource interface{}, newExpiration time.Time, err error)

// ExpiringResource is a temporal resource (usually a credential), that requires periodic refreshing.
type ExpiringResource struct {
	// cond is used to synchronize access to the shared resource embodied by the remaining fields
	cond *sync.Cond

	// acquiring indicates that some thread/goroutine is in the process of acquiring/updating the resource
	acquiring bool

	// resource contains the value of the shared resource
	resource interface{}

	// expiration indicates when the shared resource expires; it is 0 if the resource was never acquired
	expiration time.Time

	// acquireResource is the callback function that actually acquires the resource
	acquireResource AcquireResource
}

// NewExpiringResource creates a new ExpiringResource that uses the specified AcquireResource for refreshing.
func NewExpiringResource(ar AcquireResource) *ExpiringResource {
	return &ExpiringResource{cond: sync.NewCond(&sync.Mutex{}), acquireResource: ar}
}

// GetResource returns the underlying resource.
// If the resource is fresh, no refresh is performed.
func (er *ExpiringResource) GetResource(state interface{}) (interface{}, error) {
	// If the resource is expiring within this time window, update it eagerly.
	// This allows other threads/goroutines to keep running by using the not-yet-expired
	// resource value while one thread/goroutine updates the resource.
	const window = 2 * time.Minute // This example updates the resource 2 minutes prior to expiration

	now, acquire, resource := time.Now(), false, er.resource
	// acquire exclusive lock
	er.cond.L.Lock()
	for {
		if er.expiration.IsZero() || er.expiration.Before(now) {
			// The resource was never acquired or has expired
			if !er.acquiring {
				// If another thread/goroutine is not acquiring/updating the resource, this thread/goroutine will do it
				er.acquiring, acquire = true, true
				break
			}
			// Getting here means that this thread/goroutine will wait for the updated resource
		} else if er.expiration.Add(-window).Before(now) {
			// The resource is valid but is expiring within the time window
			if !er.acquiring {
				// If another thread/goroutine is not acquiring/renewing the resource, this thread/goroutine will do it
				er.acquiring, acquire = true, true
				break
			}
			// This thread/goroutine will use the existing resource value while another updates it
			resource = er.resource
			break
		} else {
			// The resource is not close to expiring, this thread/goroutine should use its current value
			resource = er.resource
			break
		}
		// If we get here, wait for the new resource value to be acquired/updated
		er.cond.Wait()
	}
	er.cond.L.Unlock() // Release the lock so no threads/goroutines are blocked

	var err error
	if acquire {
		// This thread/goroutine has been selected to acquire/update the resource
		var expiration time.Time
		resource, expiration, err = er.acquireResource(state)

		// Atomically, update the shared resource's new value & expiration.
		er.cond.L.Lock()
		if err == nil {
			// No error, update resource & expiration
			er.resource, er.expiration = resource, expiration
		}
		er.acquiring = false // Indicate that no thread/goroutine is currently acquiring the resrouce

		// Wake up any waiting threads/goroutines since there is a resource they can ALL use
		er.cond.L.Unlock()
		er.cond.Broadcast()
	}
	return resource, err // Return the resource this thread/goroutine can use
}
