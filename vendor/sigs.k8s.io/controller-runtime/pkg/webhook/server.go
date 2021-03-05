/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/metrics"
)

// DefaultPort is the default port that the webhook server serves.
var DefaultPort = 9443

// Server is an admission webhook server that can serve traffic and
// generates related k8s resources for deploying.
type Server struct {
	// Host is the address that the server will listen on.
	// Defaults to "" - all addresses.
	Host string

	// Port is the port number that the server will serve.
	// It will be defaulted to 9443 if unspecified.
	Port int

	// CertDir is the directory that contains the server key and certificate. The
	// server key and certificate.
	CertDir string

	// CertName is the server certificate name. Defaults to tls.crt.
	CertName string

	// KeyName is the server key name. Defaults to tls.key.
	KeyName string

	// ClientCAName is the CA certificate name which server used to verify remote(client)'s certificate.
	// Defaults to "", which means server does not verify client's certificate.
	ClientCAName string

	// WebhookMux is the multiplexer that handles different webhooks.
	WebhookMux *http.ServeMux

	// webhooks keep track of all registered webhooks for dependency injection,
	// and to provide better panic messages on duplicate webhook registration.
	webhooks map[string]http.Handler

	// setFields allows injecting dependencies from an external source
	setFields inject.Func

	// defaultingOnce ensures that the default fields are only ever set once.
	defaultingOnce sync.Once

	// mu protects access to the webhook map & setFields for Start, Register, etc
	mu sync.Mutex
}

// setDefaults does defaulting for the Server.
func (s *Server) setDefaults() {
	s.webhooks = map[string]http.Handler{}
	if s.WebhookMux == nil {
		s.WebhookMux = http.NewServeMux()
	}

	if s.Port <= 0 {
		s.Port = DefaultPort
	}

	if len(s.CertDir) == 0 {
		s.CertDir = filepath.Join(os.TempDir(), "k8s-webhook-server", "serving-certs")
	}

	if len(s.CertName) == 0 {
		s.CertName = "tls.crt"
	}

	if len(s.KeyName) == 0 {
		s.KeyName = "tls.key"
	}
}

// NeedLeaderElection implements the LeaderElectionRunnable interface, which indicates
// the webhook server doesn't need leader election.
func (*Server) NeedLeaderElection() bool {
	return false
}

// Register marks the given webhook as being served at the given path.
// It panics if two hooks are registered on the same path.
func (s *Server) Register(path string, hook http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.defaultingOnce.Do(s.setDefaults)
	_, found := s.webhooks[path]
	if found {
		panic(fmt.Errorf("can't register duplicate path: %v", path))
	}
	// TODO(directxman12): call setfields if we've already started the server
	s.webhooks[path] = hook
	s.WebhookMux.Handle(path, instrumentedHook(path, hook))

	regLog := log.WithValues("path", path)
	regLog.Info("registering webhook")

	// we've already been "started", inject dependencies here.
	// Otherwise, InjectFunc will do this for us later.
	if s.setFields != nil {
		if err := s.setFields(hook); err != nil {
			// TODO(directxman12): swallowing this error isn't great, but we'd have to
			// change the signature to fix that
			regLog.Error(err, "unable to inject fields into webhook during registration")
		}

		baseHookLog := log.WithName("webhooks")

		// NB(directxman12): we don't propagate this further by wrapping setFields because it's
		// unclear if this is how we want to deal with log propagation.  In this specific instance,
		// we want to be able to pass a logger to webhooks because they don't know their own path.
		if _, err := inject.LoggerInto(baseHookLog.WithValues("webhook", path), hook); err != nil {
			regLog.Error(err, "unable to logger into webhook during registration")
		}
	}
}

// instrumentedHook adds some instrumentation on top of the given webhook.
func instrumentedHook(path string, hookRaw http.Handler) http.Handler {
	lbl := prometheus.Labels{"webhook": path}

	lat := metrics.RequestLatency.MustCurryWith(lbl)
	cnt := metrics.RequestTotal.MustCurryWith(lbl)
	gge := metrics.RequestInFlight.With(lbl)

	// Initialize the most likely HTTP status codes.
	cnt.WithLabelValues("200")
	cnt.WithLabelValues("500")

	return promhttp.InstrumentHandlerDuration(
		lat,
		promhttp.InstrumentHandlerCounter(
			cnt,
			promhttp.InstrumentHandlerInFlight(gge, hookRaw),
		),
	)
}

// Start runs the server.
// It will install the webhook related resources depend on the server configuration.
func (s *Server) Start(ctx context.Context) error {
	s.defaultingOnce.Do(s.setDefaults)

	baseHookLog := log.WithName("webhooks")
	baseHookLog.Info("starting webhook server")

	certPath := filepath.Join(s.CertDir, s.CertName)
	keyPath := filepath.Join(s.CertDir, s.KeyName)

	certWatcher, err := certwatcher.New(certPath, keyPath)
	if err != nil {
		return err
	}

	go func() {
		if err := certWatcher.Start(ctx); err != nil {
			log.Error(err, "certificate watcher error")
		}
	}()

	cfg := &tls.Config{
		NextProtos:     []string{"h2"},
		GetCertificate: certWatcher.GetCertificate,
	}

	// load CA to verify client certificate
	if s.ClientCAName != "" {
		certPool := x509.NewCertPool()
		clientCABytes, err := ioutil.ReadFile(filepath.Join(s.CertDir, s.ClientCAName))
		if err != nil {
			return fmt.Errorf("failed to read client CA cert: %v", err)
		}

		ok := certPool.AppendCertsFromPEM(clientCABytes)
		if !ok {
			return fmt.Errorf("failed to append client CA cert to CA pool")
		}

		cfg.ClientCAs = certPool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	listener, err := tls.Listen("tcp", net.JoinHostPort(s.Host, strconv.Itoa(int(s.Port))), cfg)
	if err != nil {
		return err
	}

	log.Info("serving webhook server", "host", s.Host, "port", s.Port)

	srv := &http.Server{
		Handler: s.WebhookMux,
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		<-ctx.Done()
		log.Info("shutting down webhook server")

		// TODO: use a context with reasonable timeout
		if err := srv.Shutdown(context.Background()); err != nil {
			// Error from closing listeners, or context timeout
			log.Error(err, "error shutting down the HTTP server")
		}
		close(idleConnsClosed)
	}()

	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}

	<-idleConnsClosed
	return nil
}

// InjectFunc injects the field setter into the server.
func (s *Server) InjectFunc(f inject.Func) error {
	s.setFields = f

	// inject fields here that weren't injected in Register because we didn't have setFields yet.
	baseHookLog := log.WithName("webhooks")
	for hookPath, webhook := range s.webhooks {
		if err := s.setFields(webhook); err != nil {
			return err
		}

		// NB(directxman12): we don't propagate this further by wrapping setFields because it's
		// unclear if this is how we want to deal with log propagation.  In this specific instance,
		// we want to be able to pass a logger to webhooks because they don't know their own path.
		if _, err := inject.LoggerInto(baseHookLog.WithValues("webhook", hookPath), webhook); err != nil {
			return err
		}
	}
	return nil
}
