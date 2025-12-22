// Copyright (c) 2016, 2018, 2025, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.

package common

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
)

// GetTLSConfigTemplateForTransport returns the TLSConfigTemplate to used depending on whether any additional
// CA Bundle or client side certs have been configured
func GetTLSConfigTemplateForTransport() TLSConfigProvider {
	certPath := os.Getenv(ociDefaultClientCertsPath)
	keyPath := os.Getenv(ociDefaultClientCertsPrivateKeyPath)
	caBundlePath := os.Getenv(ociDefaultCertsPath)
	if certPath != "" && keyPath != "" {
		return &DefaultMTLSConfigProvider{
			caBundlePath:         caBundlePath,
			clientCertPath:       certPath,
			clientKeyPath:        keyPath,
			watchedFilesStatsMap: make(map[string]os.FileInfo),
		}
	}
	return &DefaultTLSConfigProvider{
		caBundlePath: caBundlePath,
	}
}

// TLSConfigProvider is an interface the defines a function that creates a new *tls.Config.
type TLSConfigProvider interface {
	NewOrDefault() (*tls.Config, error)
	WatchedFilesModified() bool
}

// DefaultTLSConfigProvider is a provider that provides a TLS tls.config for the HTTPTransport
type DefaultTLSConfigProvider struct {
	caBundlePath string
	mux          sync.Mutex
	currentStat  os.FileInfo
}

// NewOrDefault returns a default tls.Config which
// sets its RootCAs to be a *x509.CertPool from caBundlePath.
func (t *DefaultTLSConfigProvider) NewOrDefault() (*tls.Config, error) {
	if t.caBundlePath == "" {
		return &tls.Config{}, nil
	}

	// Keep the current Stat info from the ca bundle in a map
	Debugf("Getting Initial Stats for file: %s", t.caBundlePath)
	caBundleStat, err := os.Stat(t.caBundlePath)
	if err != nil {
		return nil, err
	}
	t.mux.Lock()
	defer t.mux.Unlock()
	t.currentStat = caBundleStat

	rootCAs, err := CertPoolFrom(t.caBundlePath)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		RootCAs: rootCAs,
	}, nil
}

// WatchedFilesModified returns true if any files in the watchedFilesStatsMap has been modified else returns false
func (t *DefaultTLSConfigProvider) WatchedFilesModified() bool {
	modified := false
	if t.caBundlePath != "" {
		newStat, err := os.Stat(t.caBundlePath)
		if err == nil && (t.currentStat.Size() != newStat.Size() || t.currentStat.ModTime() != newStat.ModTime()) {
			Logf("Modification detected in cert/ca-bundle file: %s", t.caBundlePath)
			modified = true
			t.mux.Lock()
			defer t.mux.Unlock()
			t.currentStat = newStat
		}
	}
	return modified
}

// DefaultMTLSConfigProvider is a provider that provides a MTLS tls.config for the HTTPTransport
type DefaultMTLSConfigProvider struct {
	caBundlePath         string
	clientCertPath       string
	clientKeyPath        string
	mux                  sync.Mutex
	watchedFilesStatsMap map[string]os.FileInfo
}

// NewOrDefault returns a default tls.Config which sets its RootCAs
// to be a *x509.CertPool from caBundlePath and calls
// tls.LoadX509KeyPair(clientCertPath, clientKeyPath) to set mtls client certs.
func (t *DefaultMTLSConfigProvider) NewOrDefault() (*tls.Config, error) {
	rootCAs, err := CertPoolFrom(t.caBundlePath)
	if err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(t.clientCertPath, t.clientKeyPath)
	if err != nil {
		return nil, err
	}

	// Configure the initial certs file stats, error skipped because we error out before this if the files don't exist
	t.mux.Lock()
	defer t.mux.Unlock()
	t.watchedFilesStatsMap[t.caBundlePath], _ = os.Stat(t.caBundlePath)
	t.watchedFilesStatsMap[t.clientCertPath], _ = os.Stat(t.clientCertPath)
	t.watchedFilesStatsMap[t.clientKeyPath], _ = os.Stat(t.clientKeyPath)

	return &tls.Config{
		RootCAs:      rootCAs,
		Certificates: []tls.Certificate{cert},
	}, nil
}

// WatchedFilesModified returns true if any files in the watchedFilesStatsMap has been modified else returns false
func (t *DefaultMTLSConfigProvider) WatchedFilesModified() bool {
	modified := false

	t.mux.Lock()
	defer t.mux.Unlock()
	for k, v := range t.watchedFilesStatsMap {
		if k != "" {
			currentStat, err := os.Stat(k)
			if err == nil && (v.Size() != currentStat.Size() || v.ModTime() != currentStat.ModTime()) {
				modified = true
				Logf("Modification detected in cert/ca-bundle file: %s", k)
				t.watchedFilesStatsMap[k] = currentStat
			}
		}
	}

	return modified
}

// CertPoolFrom creates a new x509.CertPool from a given file.
func CertPoolFrom(caBundleFile string) (*x509.CertPool, error) {
	pemCerts, err := os.ReadFile(caBundleFile)
	if err != nil {
		return nil, err
	}

	trust := x509.NewCertPool()
	if !trust.AppendCertsFromPEM(pemCerts) {
		return nil, fmt.Errorf("creating a new x509.CertPool from %s: no certs added", caBundleFile)
	}

	return trust, nil
}
