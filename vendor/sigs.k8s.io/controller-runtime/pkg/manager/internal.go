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

package manager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	intrec "sigs.k8s.io/controller-runtime/pkg/internal/recorder"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	// Values taken from: https://github.com/kubernetes/apiserver/blob/master/pkg/apis/config/v1alpha1/defaults.go
	defaultLeaseDuration          = 15 * time.Second
	defaultRenewDeadline          = 10 * time.Second
	defaultRetryPeriod            = 2 * time.Second
	defaultGracefulShutdownPeriod = 30 * time.Second

	defaultReadinessEndpoint = "/readyz"
	defaultLivenessEndpoint  = "/healthz"
	defaultMetricsEndpoint   = "/metrics"
)

var _ Runnable = &controllerManager{}

type controllerManager struct {
	// cluster holds a variety of methods to interact with a cluster. Required.
	cluster cluster.Cluster

	// leaderElectionRunnables is the set of Controllers that the controllerManager injects deps into and Starts.
	// These Runnables are managed by lead election.
	leaderElectionRunnables []Runnable
	// nonLeaderElectionRunnables is the set of webhook servers that the controllerManager injects deps into and Starts.
	// These Runnables will not be blocked by lead election.
	nonLeaderElectionRunnables []Runnable

	// recorderProvider is used to generate event recorders that will be injected into Controllers
	// (and EventHandlers, Sources and Predicates).
	recorderProvider *intrec.Provider

	// resourceLock forms the basis for leader election
	resourceLock resourcelock.Interface

	// leaderElectionReleaseOnCancel defines if the manager should step back from the leader lease
	// on shutdown
	leaderElectionReleaseOnCancel bool

	// metricsListener is used to serve prometheus metrics
	metricsListener net.Listener

	// metricsExtraHandlers contains extra handlers to register on http server that serves metrics.
	metricsExtraHandlers map[string]http.Handler

	// healthProbeListener is used to serve liveness probe
	healthProbeListener net.Listener

	// Readiness probe endpoint name
	readinessEndpointName string

	// Liveness probe endpoint name
	livenessEndpointName string

	// Readyz probe handler
	readyzHandler *healthz.Handler

	// Healthz probe handler
	healthzHandler *healthz.Handler

	mu             sync.Mutex
	started        bool
	startedLeader  bool
	healthzStarted bool
	errChan        chan error

	// Logger is the logger that should be used by this manager.
	// If none is set, it defaults to log.Log global logger.
	logger logr.Logger

	// leaderElectionCancel is used to cancel the leader election. It is distinct from internalStopper,
	// because for safety reasons we need to os.Exit() when we lose the leader election, meaning that
	// it must be deferred until after gracefulShutdown is done.
	leaderElectionCancel context.CancelFunc

	// leaderElectionStopped is an internal channel used to signal the stopping procedure that the
	// LeaderElection.Run(...) function has returned and the shutdown can proceed.
	leaderElectionStopped chan struct{}

	// stop procedure engaged. In other words, we should not add anything else to the manager
	stopProcedureEngaged bool

	// elected is closed when this manager becomes the leader of a group of
	// managers, either because it won a leader election or because no leader
	// election was configured.
	elected chan struct{}

	caches []hasCache

	// port is the port that the webhook server serves at.
	port int
	// host is the hostname that the webhook server binds to.
	host string
	// CertDir is the directory that contains the server key and certificate.
	// if not set, webhook server would look up the server key and certificate in
	// {TempDir}/k8s-webhook-server/serving-certs
	certDir string

	webhookServer *webhook.Server

	// leaseDuration is the duration that non-leader candidates will
	// wait to force acquire leadership.
	leaseDuration time.Duration
	// renewDeadline is the duration that the acting controlplane will retry
	// refreshing leadership before giving up.
	renewDeadline time.Duration
	// retryPeriod is the duration the LeaderElector clients should wait
	// between tries of actions.
	retryPeriod time.Duration

	// waitForRunnable is holding the number of runnables currently running so that
	// we can wait for them to exit before quitting the manager
	waitForRunnable sync.WaitGroup

	// gracefulShutdownTimeout is the duration given to runnable to stop
	// before the manager actually returns on stop.
	gracefulShutdownTimeout time.Duration

	// onStoppedLeading is callled when the leader election lease is lost.
	// It can be overridden for tests.
	onStoppedLeading func()

	// shutdownCtx is the context that can be used during shutdown. It will be cancelled
	// after the gracefulShutdownTimeout ended. It must not be accessed before internalStop
	// is closed because it will be nil.
	shutdownCtx context.Context

	internalCtx    context.Context
	internalCancel context.CancelFunc

	// internalProceduresStop channel is used internally to the manager when coordinating
	// the proper shutdown of servers. This channel is also used for dependency injection.
	internalProceduresStop chan struct{}
}

type hasCache interface {
	Runnable
	GetCache() cache.Cache
}

// Add sets dependencies on i, and adds it to the list of Runnables to start.
func (cm *controllerManager) Add(r Runnable) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.stopProcedureEngaged {
		return errors.New("can't accept new runnable as stop procedure is already engaged")
	}

	// Set dependencies on the object
	if err := cm.SetFields(r); err != nil {
		return err
	}

	var shouldStart bool

	// Add the runnable to the leader election or the non-leaderelection list
	if leRunnable, ok := r.(LeaderElectionRunnable); ok && !leRunnable.NeedLeaderElection() {
		shouldStart = cm.started
		cm.nonLeaderElectionRunnables = append(cm.nonLeaderElectionRunnables, r)
	} else if hasCache, ok := r.(hasCache); ok {
		cm.caches = append(cm.caches, hasCache)
	} else {
		shouldStart = cm.startedLeader
		cm.leaderElectionRunnables = append(cm.leaderElectionRunnables, r)
	}

	if shouldStart {
		// If already started, start the controller
		cm.startRunnable(r)
	}

	return nil
}

// Deprecated: use the equivalent Options field to set a field. This method will be removed in v0.10.
func (cm *controllerManager) SetFields(i interface{}) error {
	if _, err := inject.InjectorInto(cm.SetFields, i); err != nil {
		return err
	}
	if _, err := inject.StopChannelInto(cm.internalProceduresStop, i); err != nil {
		return err
	}
	if _, err := inject.LoggerInto(cm.logger, i); err != nil {
		return err
	}
	if err := cm.cluster.SetFields(i); err != nil {
		return err
	}

	return nil
}

// AddMetricsExtraHandler adds extra handler served on path to the http server that serves metrics.
func (cm *controllerManager) AddMetricsExtraHandler(path string, handler http.Handler) error {
	if path == defaultMetricsEndpoint {
		return fmt.Errorf("overriding builtin %s endpoint is not allowed", defaultMetricsEndpoint)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	_, found := cm.metricsExtraHandlers[path]
	if found {
		return fmt.Errorf("can't register extra handler by duplicate path %q on metrics http server", path)
	}

	cm.metricsExtraHandlers[path] = handler
	cm.logger.V(2).Info("Registering metrics http server extra handler", "path", path)
	return nil
}

// AddHealthzCheck allows you to add Healthz checker
func (cm *controllerManager) AddHealthzCheck(name string, check healthz.Checker) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.stopProcedureEngaged {
		return errors.New("can't accept new healthCheck as stop procedure is already engaged")
	}

	if cm.healthzStarted {
		return fmt.Errorf("unable to add new checker because healthz endpoint has already been created")
	}

	if cm.healthzHandler == nil {
		cm.healthzHandler = &healthz.Handler{Checks: map[string]healthz.Checker{}}
	}

	cm.healthzHandler.Checks[name] = check
	return nil
}

// AddReadyzCheck allows you to add Readyz checker
func (cm *controllerManager) AddReadyzCheck(name string, check healthz.Checker) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.stopProcedureEngaged {
		return errors.New("can't accept new ready check as stop procedure is already engaged")
	}

	if cm.healthzStarted {
		return fmt.Errorf("unable to add new checker because readyz endpoint has already been created")
	}

	if cm.readyzHandler == nil {
		cm.readyzHandler = &healthz.Handler{Checks: map[string]healthz.Checker{}}
	}

	cm.readyzHandler.Checks[name] = check
	return nil
}

func (cm *controllerManager) GetConfig() *rest.Config {
	return cm.cluster.GetConfig()
}

func (cm *controllerManager) GetClient() client.Client {
	return cm.cluster.GetClient()
}

func (cm *controllerManager) GetScheme() *runtime.Scheme {
	return cm.cluster.GetScheme()
}

func (cm *controllerManager) GetFieldIndexer() client.FieldIndexer {
	return cm.cluster.GetFieldIndexer()
}

func (cm *controllerManager) GetCache() cache.Cache {
	return cm.cluster.GetCache()
}

func (cm *controllerManager) GetEventRecorderFor(name string) record.EventRecorder {
	return cm.cluster.GetEventRecorderFor(name)
}

func (cm *controllerManager) GetRESTMapper() meta.RESTMapper {
	return cm.cluster.GetRESTMapper()
}

func (cm *controllerManager) GetAPIReader() client.Reader {
	return cm.cluster.GetAPIReader()
}

func (cm *controllerManager) GetWebhookServer() *webhook.Server {
	server, wasNew := func() (*webhook.Server, bool) {
		cm.mu.Lock()
		defer cm.mu.Unlock()

		if cm.webhookServer != nil {
			return cm.webhookServer, false
		}

		cm.webhookServer = &webhook.Server{
			Port:    cm.port,
			Host:    cm.host,
			CertDir: cm.certDir,
		}
		return cm.webhookServer, true
	}()

	// only add the server if *we ourselves* just registered it.
	// Add has its own lock, so just do this separately -- there shouldn't
	// be a "race" in this lock gap because the condition is the population
	// of cm.webhookServer, not anything to do with Add.
	if wasNew {
		if err := cm.Add(server); err != nil {
			panic("unable to add webhook server to the controller manager")
		}
	}
	return server
}

func (cm *controllerManager) GetLogger() logr.Logger {
	return cm.logger
}

func (cm *controllerManager) serveMetrics() {
	handler := promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.HTTPErrorOnError,
	})
	// TODO(JoelSpeed): Use existing Kubernetes machinery for serving metrics
	mux := http.NewServeMux()
	mux.Handle(defaultMetricsEndpoint, handler)

	func() {
		cm.mu.Lock()
		defer cm.mu.Unlock()

		for path, extraHandler := range cm.metricsExtraHandlers {
			mux.Handle(path, extraHandler)
		}
	}()

	server := http.Server{
		Handler: mux,
	}
	// Run the server
	cm.startRunnable(RunnableFunc(func(_ context.Context) error {
		cm.logger.Info("starting metrics server", "path", defaultMetricsEndpoint)
		if err := server.Serve(cm.metricsListener); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}))

	// Shutdown the server when stop is closed
	<-cm.internalProceduresStop
	if err := server.Shutdown(cm.shutdownCtx); err != nil {
		cm.errChan <- err
	}
}

func (cm *controllerManager) serveHealthProbes() {
	mux := http.NewServeMux()
	server := http.Server{
		Handler: mux,
	}

	func() {
		cm.mu.Lock()
		defer cm.mu.Unlock()

		if cm.readyzHandler != nil {
			mux.Handle(cm.readinessEndpointName, http.StripPrefix(cm.readinessEndpointName, cm.readyzHandler))
			// Append '/' suffix to handle subpaths
			mux.Handle(cm.readinessEndpointName+"/", http.StripPrefix(cm.readinessEndpointName, cm.readyzHandler))
		}
		if cm.healthzHandler != nil {
			mux.Handle(cm.livenessEndpointName, http.StripPrefix(cm.livenessEndpointName, cm.healthzHandler))
			// Append '/' suffix to handle subpaths
			mux.Handle(cm.livenessEndpointName+"/", http.StripPrefix(cm.livenessEndpointName, cm.healthzHandler))
		}

		// Run server
		cm.startRunnable(RunnableFunc(func(_ context.Context) error {
			if err := server.Serve(cm.healthProbeListener); err != nil && err != http.ErrServerClosed {
				return err
			}
			return nil
		}))
		cm.healthzStarted = true
	}()

	// Shutdown the server when stop is closed
	<-cm.internalProceduresStop
	if err := server.Shutdown(cm.shutdownCtx); err != nil {
		cm.errChan <- err
	}
}

func (cm *controllerManager) Start(ctx context.Context) (err error) {
	if err := cm.Add(cm.cluster); err != nil {
		return fmt.Errorf("failed to add cluster to runnables: %w", err)
	}
	cm.internalCtx, cm.internalCancel = context.WithCancel(ctx)

	// This chan indicates that stop is complete, in other words all runnables have returned or timeout on stop request
	stopComplete := make(chan struct{})
	defer close(stopComplete)
	// This must be deferred after closing stopComplete, otherwise we deadlock.
	defer func() {
		// https://hips.hearstapps.com/hmg-prod.s3.amazonaws.com/images/gettyimages-459889618-1533579787.jpg
		stopErr := cm.engageStopProcedure(stopComplete)
		if stopErr != nil {
			if err != nil {
				// Utilerrors.Aggregate allows to use errors.Is for all contained errors
				// whereas fmt.Errorf allows wrapping at most one error which means the
				// other one can not be found anymore.
				err = utilerrors.NewAggregate([]error{err, stopErr})
			} else {
				err = stopErr
			}
		}
	}()

	// initialize this here so that we reset the signal channel state on every start
	// Everything that might write into this channel must be started in a new goroutine,
	// because otherwise we might block this routine trying to write into the full channel
	// and will not be able to enter the deferred cm.engageStopProcedure() which drains
	// it.
	cm.errChan = make(chan error)

	// Metrics should be served whether the controller is leader or not.
	// (If we don't serve metrics for non-leaders, prometheus will still scrape
	// the pod but will get a connection refused)
	if cm.metricsListener != nil {
		go cm.serveMetrics()
	}

	// Serve health probes
	if cm.healthProbeListener != nil {
		go cm.serveHealthProbes()
	}

	go cm.startNonLeaderElectionRunnables()

	go func() {
		if cm.resourceLock != nil {
			err := cm.startLeaderElection()
			if err != nil {
				cm.errChan <- err
			}
		} else {
			// Treat not having leader election enabled the same as being elected.
			cm.startLeaderElectionRunnables()
			close(cm.elected)
		}
	}()

	select {
	case <-ctx.Done():
		// We are done
		return nil
	case err := <-cm.errChan:
		// Error starting or running a runnable
		return err
	}
}

// engageStopProcedure signals all runnables to stop, reads potential errors
// from the errChan and waits for them to end. It must not be called more than once.
func (cm *controllerManager) engageStopProcedure(stopComplete <-chan struct{}) error {
	// Populate the shutdown context.
	var shutdownCancel context.CancelFunc
	if cm.gracefulShutdownTimeout > 0 {
		cm.shutdownCtx, shutdownCancel = context.WithTimeout(context.Background(), cm.gracefulShutdownTimeout)
	} else {
		cm.shutdownCtx, shutdownCancel = context.WithCancel(context.Background())
	}
	defer shutdownCancel()

	// Cancel the internal stop channel and wait for the procedures to stop and complete.
	close(cm.internalProceduresStop)
	cm.internalCancel()

	// Start draining the errors before acquiring the lock to make sure we don't deadlock
	// if something that has the lock is blocked on trying to write into the unbuffered
	// channel after something else already wrote into it.
	go func() {
		for {
			select {
			case err, ok := <-cm.errChan:
				if ok {
					cm.logger.Error(err, "error received after stop sequence was engaged")
				}
			case <-stopComplete:
				return
			}
		}
	}()
	if cm.gracefulShutdownTimeout == 0 {
		return nil
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.stopProcedureEngaged = true

	// we want to close this after the other runnables stop, because we don't
	// want things like leader election to try and emit events on a closed
	// channel
	defer cm.recorderProvider.Stop(cm.shutdownCtx)
	return cm.waitForRunnableToEnd(shutdownCancel)
}

// waitForRunnableToEnd blocks until all runnables ended or the
// tearDownTimeout was reached. In the latter case, an error is returned.
func (cm *controllerManager) waitForRunnableToEnd(shutdownCancel context.CancelFunc) (retErr error) {
	// Cancel leader election only after we waited. It will os.Exit() the app for safety.
	defer func() {
		if retErr == nil && cm.leaderElectionCancel != nil {
			// After asking the context to be cancelled, make sure
			// we wait for the leader stopped channel to be closed, otherwise
			// we might encounter race conditions between this code
			// and the event recorder, which is used within leader election code.
			cm.leaderElectionCancel()
			<-cm.leaderElectionStopped
		}
	}()

	go func() {
		cm.waitForRunnable.Wait()
		shutdownCancel()
	}()

	<-cm.shutdownCtx.Done()
	if err := cm.shutdownCtx.Err(); err != nil && err != context.Canceled {
		return fmt.Errorf("failed waiting for all runnables to end within grace period of %s: %w", cm.gracefulShutdownTimeout, err)
	}
	return nil
}

func (cm *controllerManager) startNonLeaderElectionRunnables() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.waitForCache(cm.internalCtx)

	// Start the non-leaderelection Runnables after the cache has synced
	for _, c := range cm.nonLeaderElectionRunnables {
		// Controllers block, but we want to return an error if any have an error starting.
		// Write any Start errors to a channel so we can return them
		cm.startRunnable(c)
	}
}

func (cm *controllerManager) startLeaderElectionRunnables() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.waitForCache(cm.internalCtx)

	// Start the leader election Runnables after the cache has synced
	for _, c := range cm.leaderElectionRunnables {
		// Controllers block, but we want to return an error if any have an error starting.
		// Write any Start errors to a channel so we can return them
		cm.startRunnable(c)
	}

	cm.startedLeader = true
}

func (cm *controllerManager) waitForCache(ctx context.Context) {
	if cm.started {
		return
	}

	for _, cache := range cm.caches {
		cm.startRunnable(cache)
	}

	// Wait for the caches to sync.
	// TODO(community): Check the return value and write a test
	for _, cache := range cm.caches {
		cache.GetCache().WaitForCacheSync(ctx)
	}
	// TODO: This should be the return value of cm.cache.WaitForCacheSync but we abuse
	// cm.started as check if we already started the cache so it must always become true.
	// Making sure that the cache doesn't get started twice is needed to not get a "close
	// of closed channel" panic
	cm.started = true
}

func (cm *controllerManager) startLeaderElection() (err error) {
	ctx, cancel := context.WithCancel(context.Background())
	cm.mu.Lock()
	cm.leaderElectionCancel = cancel
	cm.mu.Unlock()

	if cm.onStoppedLeading == nil {
		cm.onStoppedLeading = func() {
			// Make sure graceful shutdown is skipped if we lost the leader lock without
			// intending to.
			cm.gracefulShutdownTimeout = time.Duration(0)
			// Most implementations of leader election log.Fatal() here.
			// Since Start is wrapped in log.Fatal when called, we can just return
			// an error here which will cause the program to exit.
			cm.errChan <- errors.New("leader election lost")
		}
	}
	l, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          cm.resourceLock,
		LeaseDuration: cm.leaseDuration,
		RenewDeadline: cm.renewDeadline,
		RetryPeriod:   cm.retryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(_ context.Context) {
				cm.startLeaderElectionRunnables()
				close(cm.elected)
			},
			OnStoppedLeading: cm.onStoppedLeading,
		},
		ReleaseOnCancel: cm.leaderElectionReleaseOnCancel,
	})
	if err != nil {
		return err
	}

	// Start the leader elector process
	go func() {
		l.Run(ctx)
		<-ctx.Done()
		close(cm.leaderElectionStopped)
	}()
	return nil
}

func (cm *controllerManager) Elected() <-chan struct{} {
	return cm.elected
}

func (cm *controllerManager) startRunnable(r Runnable) {
	cm.waitForRunnable.Add(1)
	go func() {
		defer cm.waitForRunnable.Done()
		if err := r.Start(cm.internalCtx); err != nil {
			cm.errChan <- err
		}
	}()
}
