// Copyright (c) 2016, 2018, 2025, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.

package common

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// OciHTTPTransportWrapper is a http.RoundTripper that periodically refreshes
// the underlying http.Transport according to its templates.
// Upon the first use (or once the RefreshRate duration is elapsed),
// a new transport will be created from the TransportTemplate (if set).
type OciHTTPTransportWrapper struct {
	// RefreshRate specifies the duration at which http.Transport
	// (with its tls.Config) must be refreshed.
	// Defaults to 5 minutes.
	RefreshRate time.Duration

	// TLSConfigProvider creates a new tls.Config.
	// If not set, nil tls.Config is returned.
	TLSConfigProvider TLSConfigProvider

	// ClientTemplate is responsible for creating a new http.Client with
	// a given tls.Config.
	//
	// If not set, a new http.Client with a cloned http.DefaultTransport is returned.
	TransportTemplate TransportTemplateProvider

	// mutable properties
	mux             sync.RWMutex
	lastRefreshedAt time.Time
	delegate        http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *OciHTTPTransportWrapper) RoundTrip(req *http.Request) (*http.Response, error) {
	delegate, err := t.refreshDelegate(false /* force */)
	if err != nil {
		return nil, err
	}

	return delegate.RoundTrip(req)
}

// Refresh forces refresh of the underlying delegate.
func (t *OciHTTPTransportWrapper) Refresh(force bool) error {
	_, err := t.refreshDelegate(force)
	return err
}

// Delegate returns the currently active http.RoundTripper.
// Might be nil.
func (t *OciHTTPTransportWrapper) Delegate() http.RoundTripper {
	t.mux.RLock()
	defer t.mux.RUnlock()

	return t.delegate
}

// refreshDelegate refreshes the delegate (and its TLS config) if:
//   - force is true
//   - it's been more than RefreshRate since the last time the client was refreshed.
func (t *OciHTTPTransportWrapper) refreshDelegate(force bool) (http.RoundTripper, error) {
	// read-lock first, since it's cheaper than write lock
	t.mux.RLock()
	if !t.shouldRefreshLocked(force) {
		delegate := t.delegate
		t.mux.RUnlock()

		return delegate, nil
	}

	// upgrade to write-lock, and we'll need to check again for the same condition as above
	// to avoid multiple initializations by multiple "refresher" goroutines
	t.mux.RUnlock()
	t.mux.Lock()
	defer t.mux.Unlock()
	if !t.shouldRefreshLocked(force) {
		return t.delegate, nil
	}

	// For this check we need the delegate to be set once before we check for change in cert files
	if t.delegate != nil && !t.TLSConfigProvider.WatchedFilesModified() {
		Debug("No modification in custom certs or ca bundle skipping refresh")
		// Updating the last refresh time to make sure the next check is only done after the refresh interval has passed
		t.lastRefreshedAt = time.Now()
		return t.delegate, nil
	}

	Logf("Loading tls config from TLSConfigProvider")
	tlsConfig, err := t.TLSConfigProvider.NewOrDefault()
	if err != nil {
		return nil, fmt.Errorf("refreshing tls.Config from template: %w", err)
	}

	t.delegate, err = t.TransportTemplate.NewOrDefault(tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("refreshing http.RoundTripper from template: %w", err)
	}

	t.lastRefreshedAt = time.Now()
	return t.delegate, nil
}

// shouldRefreshLocked returns whether the client (and its TLS config)
// needs to be refreshed.
func (t *OciHTTPTransportWrapper) shouldRefreshLocked(force bool) bool {
	if force || t.delegate == nil {
		return true
	}
	return t.refreshRate() > 0 && time.Since(t.lastRefreshedAt) > t.refreshRate()
}

func (t *OciHTTPTransportWrapper) refreshRate() time.Duration {
	return t.RefreshRate
}
