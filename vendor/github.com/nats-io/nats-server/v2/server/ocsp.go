// Copyright 2021 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ocsp"
)

const (
	defaultOCSPStoreDir      = "ocsp"
	defaultOCSPCheckInterval = 24 * time.Hour
	minOCSPCheckInterval     = 2 * time.Minute
)

type OCSPMode uint8

const (
	// OCSPModeAuto staples a status, only if "status_request" is set in cert.
	OCSPModeAuto OCSPMode = iota

	// OCSPModeAlways enforces OCSP stapling for certs and shuts down the server in
	// case a server is revoked or cannot get OCSP staples.
	OCSPModeAlways

	// OCSPModeNever disables OCSP stapling even if cert has Must-Staple flag.
	OCSPModeNever

	// OCSPModeMust honors the Must-Staple flag from a certificate but also causing shutdown
	// in case the certificate has been revoked.
	OCSPModeMust
)

// OCSPMonitor monitors the state of a staple per certificate.
type OCSPMonitor struct {
	kind     string
	mu       sync.Mutex
	raw      []byte
	srv      *Server
	certFile string
	resp     *ocsp.Response
	hc       *http.Client
	stopCh   chan struct{}
	Leaf     *x509.Certificate
	Issuer   *x509.Certificate

	shutdownOnRevoke bool
}

func (oc *OCSPMonitor) getNextRun() time.Duration {
	oc.mu.Lock()
	nextUpdate := oc.resp.NextUpdate
	oc.mu.Unlock()

	now := time.Now()
	if nextUpdate.IsZero() {
		// If response is missing NextUpdate, we check the day after.
		// Technically, if NextUpdate is missing, we can try whenever.
		// https://tools.ietf.org/html/rfc6960#section-4.2.2.1
		return defaultOCSPCheckInterval
	}
	dur := nextUpdate.Sub(now) / 2

	// If negative, then wait a couple of minutes before getting another staple.
	if dur < 0 {
		return minOCSPCheckInterval
	}

	return dur
}

func (oc *OCSPMonitor) getStatus() ([]byte, *ocsp.Response, error) {
	raw, resp := oc.getCacheStatus()
	if len(raw) > 0 && resp != nil {
		// Check if the OCSP is still valid.
		if err := validOCSPResponse(resp); err == nil {
			return raw, resp, nil
		}
	}
	var err error
	raw, resp, err = oc.getLocalStatus()
	if err == nil {
		return raw, resp, nil
	}

	return oc.getRemoteStatus()
}

func (oc *OCSPMonitor) getCacheStatus() ([]byte, *ocsp.Response) {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	return oc.raw, oc.resp
}

func (oc *OCSPMonitor) getLocalStatus() ([]byte, *ocsp.Response, error) {
	opts := oc.srv.getOpts()
	storeDir := opts.StoreDir
	if storeDir == _EMPTY_ {
		return nil, nil, fmt.Errorf("store_dir not set")
	}

	// This key must be based upon the current full certificate, not the public key,
	// so MUST be on the full raw certificate and not an SPKI or other reduced form.
	key := fmt.Sprintf("%x", sha256.Sum256(oc.Leaf.Raw))

	oc.mu.Lock()
	raw, err := os.ReadFile(filepath.Join(storeDir, defaultOCSPStoreDir, key))
	oc.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}

	resp, err := ocsp.ParseResponse(raw, oc.Issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get local status: %w", err)
	}
	if err := validOCSPResponse(resp); err != nil {
		return nil, nil, err
	}

	// Cache the response.
	oc.mu.Lock()
	oc.raw = raw
	oc.resp = resp
	oc.mu.Unlock()

	return raw, resp, nil
}

func (oc *OCSPMonitor) getRemoteStatus() ([]byte, *ocsp.Response, error) {
	opts := oc.srv.getOpts()
	var overrideURLs []string
	if config := opts.OCSPConfig; config != nil {
		overrideURLs = config.OverrideURLs
	}
	getRequestBytes := func(u string, hc *http.Client) ([]byte, error) {
		resp, err := hc.Get(u)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("non-ok http status: %d", resp.StatusCode)
		}

		return io.ReadAll(resp.Body)
	}

	// Request documentation:
	// https://tools.ietf.org/html/rfc6960#appendix-A.1

	reqDER, err := ocsp.CreateRequest(oc.Leaf, oc.Issuer, nil)
	if err != nil {
		return nil, nil, err
	}

	reqEnc := base64.StdEncoding.EncodeToString(reqDER)

	responders := oc.Leaf.OCSPServer
	if len(overrideURLs) > 0 {
		responders = overrideURLs
	}
	if len(responders) == 0 {
		return nil, nil, fmt.Errorf("no available ocsp servers")
	}

	oc.mu.Lock()
	hc := oc.hc
	oc.mu.Unlock()
	var raw []byte
	for _, u := range responders {
		u = strings.TrimSuffix(u, "/")
		raw, err = getRequestBytes(fmt.Sprintf("%s/%s", u, reqEnc), hc)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, nil, fmt.Errorf("exhausted ocsp servers: %w", err)
	}

	resp, err := ocsp.ParseResponse(raw, oc.Issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get remote status: %w", err)
	}
	if err := validOCSPResponse(resp); err != nil {
		return nil, nil, err
	}

	if storeDir := opts.StoreDir; storeDir != _EMPTY_ {
		key := fmt.Sprintf("%x", sha256.Sum256(oc.Leaf.Raw))
		if err := oc.writeOCSPStatus(storeDir, key, raw); err != nil {
			return nil, nil, fmt.Errorf("failed to write ocsp status: %w", err)
		}
	}

	oc.mu.Lock()
	oc.raw = raw
	oc.resp = resp
	oc.mu.Unlock()

	return raw, resp, nil
}

func (oc *OCSPMonitor) run() {
	s := oc.srv
	s.mu.Lock()
	quitCh := s.quitCh
	s.mu.Unlock()

	var doShutdown bool
	defer func() {
		// Need to decrement before shuting down, otherwise shutdown
		// would be stuck waiting on grWG to go down to 0.
		s.grWG.Done()
		if doShutdown {
			s.Shutdown()
		}
	}()

	oc.mu.Lock()
	shutdownOnRevoke := oc.shutdownOnRevoke
	certFile := oc.certFile
	stopCh := oc.stopCh
	kind := oc.kind
	oc.mu.Unlock()

	var nextRun time.Duration
	_, resp, err := oc.getStatus()
	if err == nil && resp.Status == ocsp.Good {
		nextRun = oc.getNextRun()
		t := resp.NextUpdate.Format(time.RFC3339Nano)
		s.Noticef(
			"Found OCSP status for %s certificate at '%s': good, next update %s, checking again in %s",
			kind, certFile, t, nextRun,
		)
	} else if err == nil && shutdownOnRevoke {
		// If resp.Status is ocsp.Revoked, ocsp.Unknown, or any other value.
		s.Errorf("Found OCSP status for %s certificate at '%s': %s", kind, certFile, ocspStatusString(resp.Status))
		doShutdown = true
		return
	}

	for {
		// On reload, if the certificate changes then need to stop this monitor.
		select {
		case <-time.After(nextRun):
		case <-stopCh:
			// In case of reload and have to restart the OCSP stapling monitoring.
			return
		case <-quitCh:
			// Server quit channel.
			return
		}
		_, resp, err := oc.getRemoteStatus()
		if err != nil {
			nextRun = oc.getNextRun()
			s.Errorf("Bad OCSP status update for certificate '%s': %s, trying again in %v", certFile, err, nextRun)
			continue
		}

		switch n := resp.Status; n {
		case ocsp.Good:
			nextRun = oc.getNextRun()
			t := resp.NextUpdate.Format(time.RFC3339Nano)
			s.Noticef(
				"Received OCSP status for %s certificate '%s': good, next update %s, checking again in %s",
				kind, certFile, t, nextRun,
			)
			continue
		default:
			s.Errorf("Received OCSP status for %s certificate '%s': %s", kind, certFile, ocspStatusString(n))
			if shutdownOnRevoke {
				doShutdown = true
			}
			return
		}
	}
}

func (oc *OCSPMonitor) stop() {
	oc.mu.Lock()
	stopCh := oc.stopCh
	oc.mu.Unlock()
	stopCh <- struct{}{}
}

// NewOCSPMonitor takes a TLS configuration then wraps it with the callbacks set for OCSP verification
// along with a monitor that will periodically fetch OCSP staples.
func (srv *Server) NewOCSPMonitor(config *tlsConfigKind) (*tls.Config, *OCSPMonitor, error) {
	kind := config.kind
	tc := config.tlsConfig
	tcOpts := config.tlsOpts
	opts := srv.getOpts()
	oc := opts.OCSPConfig

	// We need to track the CA certificate in case the CA is not present
	// in the chain to be able to verify the signature of the OCSP staple.
	var (
		certFile string
		caFile   string
	)
	if kind == kindStringMap[CLIENT] {
		tcOpts = opts.tlsConfigOpts
		if opts.TLSCert != _EMPTY_ {
			certFile = opts.TLSCert
		}
		if opts.TLSCaCert != _EMPTY_ {
			caFile = opts.TLSCaCert
		}
	}
	if tcOpts != nil {
		certFile = tcOpts.CertFile
		caFile = tcOpts.CaFile
	}

	// NOTE: Currently OCSP Stapling is enabled only for the first certificate found.
	var mon *OCSPMonitor
	for _, currentCert := range tc.Certificates {
		// Create local copy since this will be used in the GetCertificate callback.
		cert := currentCert

		// This is normally non-nil, but can still be nil here when in tests
		// or in some embedded scenarios.
		if cert.Leaf == nil {
			if len(cert.Certificate) <= 0 {
				return nil, nil, fmt.Errorf("no certificate found")
			}
			var err error
			cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				return nil, nil, fmt.Errorf("error parsing certificate: %v", err)
			}
		}
		var shutdownOnRevoke bool
		mustStaple := hasOCSPStatusRequest(cert.Leaf)
		if oc != nil {
			switch {
			case oc.Mode == OCSPModeNever:
				if mustStaple {
					srv.Warnf("Certificate at '%s' has MustStaple but OCSP is disabled", certFile)
				}
				return tc, nil, nil
			case oc.Mode == OCSPModeAlways:
				// Start the monitor for this cert even if it does not have
				// the MustStaple flag and shutdown the server in case the
				// staple ever gets revoked.
				mustStaple = true
				shutdownOnRevoke = true
			case oc.Mode == OCSPModeMust && mustStaple:
				shutdownOnRevoke = true
			case oc.Mode == OCSPModeAuto && !mustStaple:
				// "status_request" MustStaple flag not set in certificate. No need to do anything.
				return tc, nil, nil
			}
		}
		if !mustStaple {
			// No explicit OCSP config and cert does not have MustStaple flag either.
			return tc, nil, nil
		}

		if err := srv.setupOCSPStapleStoreDir(); err != nil {
			return nil, nil, err
		}

		// TODO: Add OCSP 'responder_cert' option in case CA cert not available.
		issuers, err := getOCSPIssuer(caFile, cert.Certificate)
		if err != nil {
			return nil, nil, err
		}

		mon = &OCSPMonitor{
			kind:             kind,
			srv:              srv,
			hc:               &http.Client{Timeout: 30 * time.Second},
			shutdownOnRevoke: shutdownOnRevoke,
			certFile:         certFile,
			stopCh:           make(chan struct{}, 1),
			Leaf:             cert.Leaf,
			Issuer:           issuers[len(issuers)-1],
		}

		// Get the certificate status from the memory, then remote OCSP responder.
		if _, resp, err := mon.getStatus(); err != nil {
			return nil, nil, fmt.Errorf("bad OCSP status update for certificate at '%s': %s", certFile, err)
		} else if err == nil && resp != nil && resp.Status != ocsp.Good && shutdownOnRevoke {
			return nil, nil, fmt.Errorf("found existing OCSP status for certificate at '%s': %s", certFile, ocspStatusString(resp.Status))
		}

		// Callbacks below will be in charge of returning the certificate instead,
		// so this has to be nil.
		tc.Certificates = nil

		// GetCertificate returns a certificate that's presented to a client.
		tc.GetCertificate = func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
			raw, _, err := mon.getStatus()
			if err != nil {
				return nil, err
			}

			return &tls.Certificate{
				OCSPStaple:                   raw,
				Certificate:                  cert.Certificate,
				PrivateKey:                   cert.PrivateKey,
				SupportedSignatureAlgorithms: cert.SupportedSignatureAlgorithms,
				SignedCertificateTimestamps:  cert.SignedCertificateTimestamps,
				Leaf:                         cert.Leaf,
			}, nil
		}

		// Check whether need to verify staples from a peer router or gateway connection.
		switch kind {
		case kindStringMap[ROUTER], kindStringMap[GATEWAY]:
			tc.VerifyConnection = func(s tls.ConnectionState) error {
				oresp := s.OCSPResponse
				if oresp == nil {
					return fmt.Errorf("%s peer missing OCSP Staple", kind)
				}

				// Peer connections will verify the response of the staple.
				if len(s.VerifiedChains) == 0 {
					return fmt.Errorf("%s peer missing TLS verified chains", kind)
				}

				chain := s.VerifiedChains[0]
				leaf := chain[0]
				parent := issuers[len(issuers)-1]

				resp, err := ocsp.ParseResponseForCert(oresp, leaf, parent)
				if err != nil {
					return fmt.Errorf("failed to parse OCSP response from %s peer: %w", kind, err)
				}
				if resp.Certificate == nil {
					if err := resp.CheckSignatureFrom(parent); err != nil {
						return fmt.Errorf("OCSP staple not issued by issuer: %w", err)
					}
				} else {
					if err := resp.Certificate.CheckSignatureFrom(parent); err != nil {
						return fmt.Errorf("OCSP staple's signer not signed by issuer: %w", err)
					}
					ok := false
					for _, eku := range resp.Certificate.ExtKeyUsage {
						if eku == x509.ExtKeyUsageOCSPSigning {
							ok = true
							break
						}
					}
					if !ok {
						return fmt.Errorf("OCSP staple's signer missing authorization by CA to act as OCSP signer")
					}
				}
				if resp.Status != ocsp.Good {
					return fmt.Errorf("bad status for OCSP Staple from %s peer: %s", kind, ocspStatusString(resp.Status))
				}

				return nil
			}

			// When server makes a peer connection, need to also present an OCSP Staple.
			tc.GetClientCertificate = func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
				raw, _, err := mon.getStatus()
				if err != nil {
					return nil, err
				}
				cert.OCSPStaple = raw

				return &cert, nil
			}
		default:
			// GetClientCertificate returns a certificate that's presented to a server.
			tc.GetClientCertificate = func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &cert, nil
			}
		}

	}
	return tc, mon, nil
}

func (s *Server) setupOCSPStapleStoreDir() error {
	opts := s.getOpts()
	storeDir := opts.StoreDir
	if storeDir == _EMPTY_ {
		return nil
	}
	storeDir = filepath.Join(storeDir, defaultOCSPStoreDir)
	if stat, err := os.Stat(storeDir); os.IsNotExist(err) {
		if err := os.MkdirAll(storeDir, defaultDirPerms); err != nil {
			return fmt.Errorf("could not create OCSP storage directory - %v", err)
		}
	} else if stat == nil || !stat.IsDir() {
		return fmt.Errorf("OCSP storage directory is not a directory")
	}
	return nil
}

type tlsConfigKind struct {
	tlsConfig *tls.Config
	tlsOpts   *TLSConfigOpts
	kind      string
	apply     func(*tls.Config)
}

func (s *Server) configureOCSP() []*tlsConfigKind {
	sopts := s.getOpts()

	configs := make([]*tlsConfigKind, 0)

	if config := sopts.TLSConfig; config != nil {
		opts := sopts.tlsConfigOpts
		o := &tlsConfigKind{
			kind:      kindStringMap[CLIENT],
			tlsConfig: config,
			tlsOpts:   opts,
			apply:     func(tc *tls.Config) { sopts.TLSConfig = tc },
		}
		configs = append(configs, o)
	}
	if config := sopts.Cluster.TLSConfig; config != nil {
		opts := sopts.Cluster.tlsConfigOpts
		o := &tlsConfigKind{
			kind:      kindStringMap[ROUTER],
			tlsConfig: config,
			tlsOpts:   opts,
			apply:     func(tc *tls.Config) { sopts.Cluster.TLSConfig = tc },
		}
		configs = append(configs, o)
	}
	if config := sopts.LeafNode.TLSConfig; config != nil {
		opts := sopts.LeafNode.tlsConfigOpts
		o := &tlsConfigKind{
			kind:      kindStringMap[LEAF],
			tlsConfig: config,
			tlsOpts:   opts,
			apply: func(tc *tls.Config) {
				// RequireAndVerifyClientCert is used to tell a client that it
				// should send the client cert to the server.
				if opts.Verify {
					tc.ClientAuth = tls.RequireAndVerifyClientCert
				}
				// We're a leaf hub server, so we must not set this.
				tc.GetClientCertificate = nil
				sopts.LeafNode.TLSConfig = tc
			},
		}
		configs = append(configs, o)
	}
	for _, remote := range sopts.LeafNode.Remotes {
		if config := remote.TLSConfig; config != nil {
			// Use a copy of the remote here since will be used
			// in the apply func callback below.
			r, opts := remote, remote.tlsConfigOpts
			o := &tlsConfigKind{
				kind:      kindStringMap[LEAF],
				tlsConfig: config,
				tlsOpts:   opts,
				apply: func(tc *tls.Config) {
					// We're a leaf client, so we must not set this.
					tc.GetCertificate = nil
					r.TLSConfig = tc
				},
			}
			configs = append(configs, o)
		}
	}
	if config := sopts.Gateway.TLSConfig; config != nil {
		opts := sopts.Gateway.tlsConfigOpts
		o := &tlsConfigKind{
			kind:      kindStringMap[GATEWAY],
			tlsConfig: config,
			tlsOpts:   opts,
			apply:     func(tc *tls.Config) { sopts.Gateway.TLSConfig = tc },
		}
		configs = append(configs, o)
	}
	for _, remote := range sopts.Gateway.Gateways {
		if config := remote.TLSConfig; config != nil {
			gw, opts := remote, remote.tlsConfigOpts
			o := &tlsConfigKind{
				kind:      kindStringMap[GATEWAY],
				tlsConfig: config,
				tlsOpts:   opts,
				apply: func(tc *tls.Config) {
					gw.TLSConfig = tc
				},
			}
			configs = append(configs, o)
		}
	}
	return configs
}

func (s *Server) enableOCSP() error {
	configs := s.configureOCSP()

	for _, config := range configs {
		tc, mon, err := s.NewOCSPMonitor(config)
		if err != nil {
			return err
		}
		// Check if an OCSP stapling monitor is required for this certificate.
		if mon != nil {
			s.ocsps = append(s.ocsps, mon)

			// Override the TLS config with one that follows OCSP.
			config.apply(tc)
		}
	}

	return nil
}

func (s *Server) startOCSPMonitoring() {
	s.mu.Lock()
	ocsps := s.ocsps
	s.mu.Unlock()
	if ocsps == nil {
		return
	}
	for _, mon := range ocsps {
		m := mon
		m.mu.Lock()
		kind := m.kind
		m.mu.Unlock()
		s.Noticef("OCSP Stapling enabled for %s connections", kind)
		s.startGoRoutine(func() { m.run() })
	}
}

func (s *Server) reloadOCSP() error {
	if err := s.setupOCSPStapleStoreDir(); err != nil {
		return err
	}

	s.mu.Lock()
	ocsps := s.ocsps
	s.mu.Unlock()

	// Stop all OCSP Stapling monitors in case there were any running.
	for _, oc := range ocsps {
		oc.stop()
	}

	configs := s.configureOCSP()

	// Restart the monitors under the new configuration.
	ocspm := make([]*OCSPMonitor, 0)
	for _, config := range configs {
		tc, mon, err := s.NewOCSPMonitor(config)
		if err != nil {
			return err
		}
		// Check if an OCSP stapling monitor is required for this certificate.
		if mon != nil {
			ocspm = append(ocspm, mon)

			// Apply latest TLS configuration.
			config.apply(tc)
		}
	}

	// Replace stopped monitors with the new ones.
	s.mu.Lock()
	s.ocsps = ocspm
	s.mu.Unlock()

	// Dispatch all goroutines once again.
	s.startOCSPMonitoring()

	return nil
}

func hasOCSPStatusRequest(cert *x509.Certificate) bool {
	// OID for id-pe-tlsfeature defined in RFC here:
	// https://datatracker.ietf.org/doc/html/rfc7633
	tlsFeatures := asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 24}
	const statusRequestExt = 5

	// Example values:
	// * [48 3 2 1 5] - seen when creating own certs locally
	// * [30 3 2 1 5] - seen in the wild
	// Documentation:
	// https://tools.ietf.org/html/rfc6066

	for _, ext := range cert.Extensions {
		if !ext.Id.Equal(tlsFeatures) {
			continue
		}

		var val []int
		rest, err := asn1.Unmarshal(ext.Value, &val)
		if err != nil || len(rest) > 0 {
			return false
		}

		for _, n := range val {
			if n == statusRequestExt {
				return true
			}
		}
		break
	}

	return false
}

// writeOCSPStatus writes an OCSP status to a temporary file then moves it to a
// new path, in an attempt to avoid corrupting existing data.
func (oc *OCSPMonitor) writeOCSPStatus(storeDir, file string, data []byte) error {
	storeDir = filepath.Join(storeDir, defaultOCSPStoreDir)
	tmp, err := os.CreateTemp(storeDir, "tmp-cert-status")
	if err != nil {
		return err
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	oc.mu.Lock()
	err = os.Rename(tmp.Name(), filepath.Join(storeDir, file))
	oc.mu.Unlock()
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}

	return nil
}

func parseCertPEM(name string) ([]*x509.Certificate, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}

	var pemBytes []byte

	var block *pem.Block
	for len(data) != 0 {
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("unexpected PEM certificate type: %s", block.Type)
		}

		pemBytes = append(pemBytes, block.Bytes...)
	}

	return x509.ParseCertificates(pemBytes)
}

// getOCSPIssuer returns a CA cert from the given path. If the path is empty,
// then this checks a given cert chain. If both are empty, then it returns an
// error.
func getOCSPIssuer(issuerCert string, chain [][]byte) ([]*x509.Certificate, error) {
	var issuers []*x509.Certificate
	var err error
	switch {
	case len(chain) == 1 && issuerCert == _EMPTY_:
		err = fmt.Errorf("ocsp ca required in chain or configuration")
	case issuerCert != _EMPTY_:
		issuers, err = parseCertPEM(issuerCert)
	case len(chain) > 1 && issuerCert == _EMPTY_:
		issuers, err = x509.ParseCertificates(chain[1])
	default:
		err = fmt.Errorf("invalid ocsp ca configuration")
	}
	if err != nil {
		return nil, err
	}

	if len(issuers) == 0 {
		return nil, fmt.Errorf("no issuers found")
	}

	for _, issuer := range issuers {
		if !issuer.IsCA {
			return nil, fmt.Errorf("%s invalid ca basic constraints: is not ca", issuer.Subject)
		}
	}

	return issuers, nil
}

func ocspStatusString(n int) string {
	switch n {
	case ocsp.Good:
		return "good"
	case ocsp.Revoked:
		return "revoked"
	default:
		return "unknown"
	}
}

func validOCSPResponse(r *ocsp.Response) error {
	// Time validation not handled by ParseResponse.
	// https://tools.ietf.org/html/rfc6960#section-4.2.2.1
	if !r.NextUpdate.IsZero() && r.NextUpdate.Before(time.Now()) {
		t := r.NextUpdate.Format(time.RFC3339Nano)
		return fmt.Errorf("invalid ocsp NextUpdate, is past time: %s", t)
	}
	if r.ThisUpdate.After(time.Now()) {
		t := r.ThisUpdate.Format(time.RFC3339Nano)
		return fmt.Errorf("invalid ocsp ThisUpdate, is future time: %s", t)
	}

	return nil
}
