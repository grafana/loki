// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import (
	"context"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	bigtablepb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	btopt "cloud.google.com/go/bigtable/internal/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// minPollingInterval is the minimum interval enforced between successive
// GetClientConfiguration polls, regardless of the server-supplied value. It
// protects the control plane from misconfigured clients overwhelming it.
const minPollingInterval = 1 * time.Minute

// maxBackoffSeconds caps the per-attempt randomized exponential backoff used
// between failed GetClientConfiguration retries. Capping prevents the 1<<i
// shift from overflowing int when the configured retry count is large, and
// keeps a prolonged outage from stretching the retry window to absurd
// durations.
const maxBackoffSeconds = 30

// pollDeadline is the per-RPC timeout applied to every GetClientConfiguration
// attempt — both the eager initial poll spawned by Start() and each periodic
// poll spawned by pollingLoop(). Kept short so a stuck control-plane request
// can't pin a polling slot for the full polling interval.
const pollDeadline = 5 * time.Second

// maxValidityDuration caps the ValidityDuration honored from a server response
// so the manager never adds an overflowing time.Duration to time.Now(). Also
// serves as the sentinel initial value for m.validUntil (via
// validityDuration(defaultClientConfig)).
const maxValidityDuration = 100 * 365 * 24 * time.Hour

// pollingInterval returns the effective poll cadence from cfg's Polling oneof,
// preferring PollingConfiguration.PollingInterval over the deprecated
// ClientConfiguration.polling_interval scalar. Falls back to the default's
// polling interval when neither is set. Always floored at minPollingInterval
// so a misconfigured server can't DDoS the control plane.
func pollingInterval(cfg *bigtablepb.ClientConfiguration) time.Duration {
	var interval time.Duration
	if pi := cfg.GetPollingConfiguration().GetPollingInterval(); pi != nil {
		interval = pi.AsDuration()
	} else if pi := cfg.GetPollingInterval(); pi != nil {
		interval = pi.AsDuration()
	} else if cfg != defaultClientConfig {
		interval = defaultClientConfig.GetPollingConfiguration().GetPollingInterval().AsDuration()
	}
	if interval < minPollingInterval {
		interval = minPollingInterval
	}
	return interval
}

// validityDuration returns PollingConfiguration.ValidityDuration, or the
// default when the field is absent. Capped at maxValidityDuration to avoid
// time.Duration overflow when adding to time.Now().
func validityDuration(cfg *bigtablepb.ClientConfiguration) time.Duration {
	var d time.Duration
	if vd := cfg.GetPollingConfiguration().GetValidityDuration(); vd != nil {
		d = vd.AsDuration()
	} else if cfg != defaultClientConfig {
		d = defaultClientConfig.GetPollingConfiguration().GetValidityDuration().AsDuration()
	}
	if d > maxValidityDuration {
		d = maxValidityDuration
	}
	return d
}

// maxRPCRetryCount returns PollingConfiguration.MaxRpcRetryCount when the
// server sent a PollingConfiguration (honoring an explicit 0 as "no
// retries"), or the default when the server left PollingConfiguration out
// of the Polling oneof entirely.
func maxRPCRetryCount(cfg *bigtablepb.ClientConfiguration) int {
	if pc := cfg.GetPollingConfiguration(); pc != nil {
		return int(pc.GetMaxRpcRetryCount())
	}
	if cfg == defaultClientConfig {
		return 0
	}
	return maxRPCRetryCount(defaultClientConfig)
}

// configListener is a callback function for configuration changes.
//
// seq is the manager's monotonically-increasing configuration sequence
// number (m.configSeq) at the moment of the fire. It is bumped each time
// the polled configuration genuinely changes and on the validity-window
// fallback to default. addListener and poll() both serialize their fires
// through m.mu, so a listener observes seq values in strictly increasing
// order and no explicit listener-side guard is required.
//
// newConfig is a fresh proto.Clone of the manager's currentConfig, safe
// for the listener to store or mutate without racing subsequent polls.
type configListener func(newConfig *bigtablepb.ClientConfiguration, seq int64)

// ClientConfigurationManager manages the dynamic client configuration for
// Bigtable. It periodically polls for client configuration updates via
// GetClientConfiguration RPCs and fans changes out to registered listeners.
type ClientConfigurationManager struct {
	client       bigtablepb.BigtableClient
	instanceName string
	appProfileID string
	metadata     metadata.MD
	// defaultConfig is the ClientConfiguration proto served before the first
	// successful poll and used as the fallback when a poll fails after the
	// previous server-supplied configuration's validity window has expired.
	defaultConfig *bigtablepb.ClientConfiguration
	logger        *log.Logger

	// ctx scopes the polling loop and every per-poll RPC. Start() derives it
	// from its parent ctx; Close() invokes cancel to wake pollingLoop and
	// abort any in-flight poll(). cancel is initialized to a no-op so a
	// Close-before-Start call is safe.
	ctx    context.Context
	cancel context.CancelFunc
	// closed is flipped true by the first Close() via CompareAndSwap, which
	// both makes Close idempotent and arms the read-side gate consulted by
	// poll() before listener fan-out. pollsWG.Wait() guarantees poll() has
	// returned before Close() returns, but not that listeners haven't already
	// fired in the interim — closed closes that window.
	closed atomic.Bool
	// pollsWG tracks in-flight poll() invocations spawned by Start() and
	// pollingLoop(). Close() waits on it so callers can rely on no listener
	// callbacks firing after Close() returns.
	pollsWG sync.WaitGroup

	mu sync.RWMutex
	// currentConfig is the manager's authoritative view of the client config.
	// Starts as defaultConfig; every successful poll replaces it with the
	// server's raw response (accessors like pollingInterval() do field-level
	// fallback to defaultConfig for unset fields). Always non-nil.
	currentConfig *bigtablepb.ClientConfiguration
	// configSeq is a monotonically increasing sequence number incremented every time the configuration changes.
	configSeq      int64
	validUntil     time.Time
	listeners      map[int]configListener
	nextListenerID int

	// lastResponse / lastFetchedAt / lastErr / lastErrAt mirror the most
	// recent poll() outcome — kept verbatim (cloned proto) so the configz
	// debug UI can render the server's raw GetClientConfiguration response
	// without re-deriving it from currentConfig.
	lastResponse  *bigtablepb.ClientConfiguration
	lastFetchedAt time.Time
	lastErr       error
	lastErrAt     time.Time
	// pollHistory is a ring buffer of the last poll outcomes, capped at
	// maxPollHistory so memory stays bounded over a long-lived client.
	pollHistory []PollEvent
}

// PollEvent records one GetClientConfiguration poll outcome surfaced by
// configz so an operator can see "polls stopped 4 minutes ago" or "the last
// three polls failed" without trawling logs.
type PollEvent struct {
	At        time.Time
	Duration  time.Duration
	Err       string // empty on success
	ConfigSeq int64  // post-poll seq; 0 if the poll didn't bump it
}

// maxPollHistory bounds the per-manager ring buffer. At the default polling
// interval (5 min) this covers at least the last ~8 hours of activity.
const maxPollHistory = 100

// recordPoll appends a poll outcome to the ring buffer, evicting the oldest
// when full. Caller must hold m.mu.
func (m *ClientConfigurationManager) recordPoll(ev PollEvent) {
	if len(m.pollHistory) >= maxPollHistory {
		copy(m.pollHistory, m.pollHistory[1:])
		m.pollHistory = m.pollHistory[:len(m.pollHistory)-1]
	}
	m.pollHistory = append(m.pollHistory, ev)
}

// NewClientConfigurationManager creates a new ClientConfigurationManager.
func NewClientConfigurationManager(
	client bigtablepb.BigtableClient,
	instanceName string,
	appProfileID string,
	md metadata.MD,
	logger *log.Logger,
) *ClientConfigurationManager {
	return &ClientConfigurationManager{
		client:        client,
		instanceName:  instanceName,
		appProfileID:  appProfileID,
		metadata:      md,
		defaultConfig: defaultClientConfig,
		currentConfig: defaultClientConfig,
		// Derived from defaultClientConfig so the sentinel far-future time is
		// defined once, in the defaults table — not open-coded here.
		validUntil: time.Now().Add(validityDuration(defaultClientConfig)),
		listeners:  make(map[int]configListener),
		logger:     logger,
		// No-op until Start() wires the real cancel; makes Close-before-Start safe.
		cancel: func() {},
	}
}

// Start begins the polling process. The passed-in ctx is the parent for the
// manager's own lifetime ctx — cancelling it (or calling Close) tears down
// the polling loop and any in-flight poll RPC.
func (m *ClientConfigurationManager) Start(ctx context.Context) {
	btopt.Debugf(m.logger, "bigtable: starting client configuration manager for instance %q, app profile %q", m.instanceName, m.appProfileID)
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Eager initial poll, scoped to m.ctx so Close()'s cancel reaches it too.
	m.pollsWG.Add(1)
	go func() {
		defer m.pollsWG.Done()
		pollCtx, cancel := context.WithTimeout(m.ctx, pollDeadline)
		defer cancel()
		m.poll(pollCtx)
	}()

	// Start background polling
	go m.pollingLoop()
}

// Close stops the polling process.
//
// Close is safe to call multiple times; only the first call performs teardown
// (the CompareAndSwap on m.closed is the idempotency gate). It also waits for
// any in-flight poll() invocations spawned by Start() and pollingLoop() to
// return before it itself returns. Combined with the closed gate that poll()
// consults before firing listeners, this guarantees no listener callbacks
// fire after Close() returns — so SessionPools registered via
// AddSessionPoolListener can be Close'd immediately after this returns
// without racing against a late configuration callback.
func (m *ClientConfigurationManager) Close() {
	// CAS makes Close idempotent; it also arms the read-side gate inside
	// poll() before we cancel and start waiting.
	if !m.closed.CompareAndSwap(false, true) {
		return
	}
	btopt.Debugf(m.logger, "bigtable: closing client configuration manager")
	m.cancel()
	// Wait for in-flight polls (including their listener callbacks, which
	// are short-circuited by isClosed()) to finish before returning.
	m.pollsWG.Wait()
}

// isClosed reports whether Close() has begun teardown. poll() consults this
// before invoking listeners so callbacks do not fire after Close() returns.
func (m *ClientConfigurationManager) isClosed() bool {
	return m.closed.Load()
}

// getConfig returns a clone of the current configuration. Safe for the caller
// to store or mutate without racing subsequent polls.
func (m *ClientConfigurationManager) getConfig() *bigtablepb.ClientConfiguration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return proto.Clone(m.currentConfig).(*bigtablepb.ClientConfiguration)
}

// ConfigSnapshot is an immutable view of the most recent
// GetClientConfiguration poll outcome — what the configz debug UI renders.
//
// Both Response and LastErr can be set: Response holds the last successful
// response (if any), LastErr the most recent failure (if any). The two
// timestamps are zero if the corresponding event has never occurred.
type ConfigSnapshot struct {
	InstanceName string
	AppProfileID string
	ConfigSeq    int64
	ValidUntil   time.Time
	Response     *bigtablepb.ClientConfiguration
	FetchedAt    time.Time
	LastErr      error
	LastErrAt    time.Time
	// PollHistory carries the last few poll outcomes (oldest first).
	PollHistory []PollEvent
	CapturedAt  time.Time
}

// Snapshot returns a ConfigSnapshot capturing the manager's most recent poll
// outcome. The returned ClientConfiguration proto is a clone — safe for the
// caller to marshal without racing with subsequent polls.
func (m *ClientConfigurationManager) Snapshot() ConfigSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var respClone *bigtablepb.ClientConfiguration
	if m.lastResponse != nil {
		respClone = proto.Clone(m.lastResponse).(*bigtablepb.ClientConfiguration)
	}
	history := make([]PollEvent, len(m.pollHistory))
	copy(history, m.pollHistory)
	return ConfigSnapshot{
		InstanceName: m.instanceName,
		AppProfileID: m.appProfileID,
		ConfigSeq:    m.configSeq,
		ValidUntil:   m.validUntil,
		Response:     respClone,
		FetchedAt:    m.lastFetchedAt,
		LastErr:      m.lastErr,
		LastErrAt:    m.lastErrAt,
		PollHistory:  history,
		CapturedAt:   time.Now(),
	}
}

// addListener adds a listener for configuration changes.
//
// The registration-time fire happens under m.mu, so it is serialized
// with poll()'s fan-out: poll() cannot snapshot the listener map (and
// therefore cannot fire this listener with a newer seq) while we are
// delivering the initial (cfg, seq) here. That guarantees the listener
// observes seq values in strictly increasing order without any per-
// listener wrapping.
//
// As a consequence, the listener callback runs under m.mu. Listeners
// must not call back into the manager (e.g. getConfig, or any other
// method that takes m.mu) from within the callback or they will
// deadlock. The wrappers AddSessionPoolListener / AddSessionLoadListener
// and their downstream consumers stay self-contained, so the invariant
// holds today.
func (m *ClientConfigurationManager) addListener(listener configListener) func() {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextListenerID
	m.nextListenerID++
	if m.listeners == nil {
		m.listeners = make(map[int]configListener)
	}
	m.listeners[id] = listener

	btopt.Debugf(m.logger, "bigtable: adding configuration listener (id: %d)", id)
	listener(proto.Clone(m.currentConfig).(*bigtablepb.ClientConfiguration), m.configSeq)

	return func() {
		m.mu.Lock()
		delete(m.listeners, id)
		m.mu.Unlock()
	}
}

// AddSessionPoolListener registers a callback that receives raw
// SessionPoolConfiguration updates.
//
// The wrapper extracts the SessionPool slice the server sent (or the default
// when the server didn't set one) and short-circuits when that slice is
// unchanged from the previous delivery (proto-equality), so unrelated changes
// — e.g., a new PollingInterval — won't redundantly fire a SessionPool
// resize. Mirrors the per-listener diff in Java's ListenerEntry.maybeNotify.
//
// Since currentConfig is now a live proto, the extractor forwards the
// server's full SessionPoolConfiguration verbatim — every field (Headroom,
// NewSessionCreationBudget, LoadBalancingOptions, etc.) reaches the listener,
// not just the size bounds the old Go-struct wrapper happened to copy.
func (m *ClientConfigurationManager) AddSessionPoolListener(listener func(*bigtablepb.SessionClientConfiguration_SessionPoolConfiguration)) func() {
	var (
		diffMu  sync.Mutex
		hasPrev bool
		prev    *bigtablepb.SessionClientConfiguration_SessionPoolConfiguration
	)
	return m.addListener(func(cfg *bigtablepb.ClientConfiguration, _ int64) {
		sp := cfg.GetSessionConfiguration().GetSessionPoolConfiguration()
		if sp == nil {
			sp = defaultClientConfig.GetSessionConfiguration().GetSessionPoolConfiguration()
		}
		diffMu.Lock()
		if hasPrev && proto.Equal(prev, sp) {
			diffMu.Unlock()
			return
		}
		hasPrev = true
		prev = sp
		diffMu.Unlock()
		listener(sp)
	})
}

// TODO(sushanb): plumb TelemetryConfiguration. The server-side
// ClientConfiguration proto also carries a TelemetryConfiguration message
// (see google/bigtable/v2/session.proto) that we currently ignore. Once the
// telemetry surface stabilizes, add an AddTelemetryConfigurationListener
// sibling of AddSessionPoolListener / AddSessionLoadListener and route the
// server-supplied value through it. Raised by mutianf on PR #19986.

// AddSessionLoadListener registers a callback that receives the server-driven
// SessionLoad value (0.0 = all-classic, 1.0 = all-session) on every
// configuration update from a successful poll. Returns an unregister thunk.
//
// Unlike AddSessionPoolListener, this skips the immediate registration-time
// fire that would otherwise deliver the default config (seq=0). Callers rely
// on the bootstrap value they passed to NewDiverter remaining in effect until
// the control plane actually responds — firing with seq=0's default
// SessionLoad=0 would silently clobber that bootstrap.
//
// Subsequent fires are filtered by per-listener load diff: if the polled
// SessionLoad equals the value last delivered to this listener, the inner
// callback is skipped so SessionLoad reassignment doesn't ripple to the
// Diverter on polls where only unrelated fields (e.g., PollingInterval)
// changed.
func (m *ClientConfigurationManager) AddSessionLoadListener(listener func(load float64)) func() {
	var (
		diffMu  sync.Mutex
		hasPrev bool
		prev    float64
	)
	return m.addListener(func(cfg *bigtablepb.ClientConfiguration, seq int64) {
		if seq == 0 {
			return
		}
		load := float64(cfg.GetSessionConfiguration().GetSessionLoad())
		diffMu.Lock()
		if hasPrev && prev == load {
			diffMu.Unlock()
			return
		}
		hasPrev = true
		prev = load
		diffMu.Unlock()
		listener(load)
	})
}

// pollingLoop continuously polls the Bigtable control plane at the configured interval.
// It enforces a minimum interval (minPollingInterval) to protect the control plane from DDoSes.
func (m *ClientConfigurationManager) pollingLoop() {
	for {
		m.mu.RLock()
		cfg := m.currentConfig
		m.mu.RUnlock()

		if cfg.GetStopPolling() {
			btopt.Debugf(m.logger, "bigtable: server requested polling stop; exiting pollingLoop")
			return
		}

		interval := pollingInterval(cfg)

		select {
		case <-m.ctx.Done():
			return
		case <-time.After(interval):
			// Track each poll with pollsWG so Close() can wait for in-flight
			// polls (and their listener callbacks) to finish before returning.
			m.pollsWG.Add(1)
			pollCtx, pollCancel := context.WithTimeout(m.ctx, pollDeadline)
			m.poll(pollCtx)
			pollCancel()
			m.pollsWG.Done()
		}
	}
}

// fetchClientConfiguration issues GetClientConfiguration with randomized
// exponential backoff between attempts (per the current config's
// MaxRpcRetryCount; backoff capped at maxBackoffSeconds). It returns the
// server's response on the first successful attempt, or the last RPC error
// after exhausting retries. If ctx is cancelled mid-backoff it returns
// ctx.Err() immediately so the caller can distinguish a shutdown from a
// real RPC failure and skip any fallback-to-default work.
func (m *ClientConfigurationManager) fetchClientConfiguration(ctx context.Context) (*bigtablepb.ClientConfiguration, error) {
	req := &bigtablepb.GetClientConfigurationRequest{
		InstanceName: m.instanceName,
		AppProfileId: m.appProfileID,
	}
	ctx = metadata.NewOutgoingContext(ctx, m.metadata)

	m.mu.RLock()
	maxRetries := maxRPCRetryCount(m.currentConfig)
	m.mu.RUnlock()

	var resp *bigtablepb.ClientConfiguration
	var err error
	for i := 0; i <= maxRetries; i++ {
		var header, trailer metadata.MD
		rpcStart := time.Now()
		resp, err = m.client.GetClientConfiguration(ctx, req, grpc.Header(&header), grpc.Trailer(&trailer))
		rpcDuration := time.Since(rpcStart)
		if err == nil {
			btopt.Debugf(m.logger, "bigtable: GetClientConfiguration RPC attempt %d completed successfully in %v", i, rpcDuration)
			return resp, nil
		}
		btopt.Debugf(m.logger, "bigtable: GetClientConfiguration RPC attempt %d failed in %v: %v", i, rpcDuration, err)
		if i == maxRetries {
			break
		}
		// Cap the shift width so 1<<i can't overflow on absurd
		// MaxRpcRetryCount values, then cap the wait at maxBackoffSeconds
		// so prolonged outages don't stretch a single retry into hours.
		backoffLimit := min(1<<min(i, 30), maxBackoffSeconds)
		delay := time.Duration(rand.Intn(backoffLimit)) * time.Second
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, err
}

// poll queries the GetClientConfiguration API and triggers registered listeners with the new configuration.
// If the poll fails and the previous configuration's validity has expired, it falls back to the default config.
func (m *ClientConfigurationManager) poll(ctx context.Context) {
	btopt.Debugf(m.logger, "bigtable: polling client configuration...")
	pollStart := time.Now()
	resp, err := m.fetchClientConfiguration(ctx)

	// Caller cancelled (typically Close()). Don't trigger fallback-to-default
	// or fire any listeners — pools they target are about to be torn down.
	if ctx.Err() != nil {
		return
	}

	if err != nil {
		btopt.Debugf(m.logger, "bigtable: failed to poll client configuration: %v", err)
		m.mu.Lock()
		m.lastErr = err
		m.lastErrAt = time.Now()
		m.recordPoll(PollEvent{
			At:        pollStart,
			Duration:  time.Since(pollStart),
			Err:       err.Error(),
			ConfigSeq: m.configSeq,
		})
		var listeners []configListener
		var cfgToNotify *bigtablepb.ClientConfiguration
		var seq int64
		// Fall back to default configuration if validity window has expired.
		// Reset validUntil via the default's own ValidityDuration (which is
		// the far-future sentinel) so subsequent failed polls don't
		// re-trigger this branch every interval and spam listeners with the
		// same default.
		if time.Now().After(m.validUntil) {
			btopt.Debugf(m.logger, "bigtable: client configuration validity window expired, falling back to default config")
			m.currentConfig = m.defaultConfig
			m.configSeq++
			seq = m.configSeq
			m.validUntil = time.Now().Add(validityDuration(m.defaultConfig))
			listeners = make([]configListener, 0, len(m.listeners))
			for _, l := range m.listeners {
				listeners = append(listeners, l)
			}
			cfgToNotify = m.currentConfig
		}
		m.mu.Unlock()

		// Suppress listener fires after Close() has begun teardown — the
		// SessionPools they target may already be Close'd.
		if listeners != nil && !m.isClosed() {
			for _, l := range listeners {
				l(proto.Clone(cfgToNotify).(*bigtablepb.ClientConfiguration), seq)
			}
		}
		return
	}

	btopt.Debugf(m.logger, "bigtable: successfully polled new client configuration. validityDuration: %v", validityDuration(resp))

	m.mu.Lock()
	// Always refresh the validity window — the server's reply re-affirms
	// the current config, so we shouldn't later fall back to default just
	// because the prior ValidityDuration elapsed during a no-op poll.
	m.validUntil = time.Now().Add(validityDuration(resp))
	// Capture the raw response for configz + poll-history observability.
	// Clone so subsequent server-side mutations cannot reach debug readers
	// that are inspecting the snapshot.
	if resp != nil {
		m.lastResponse = proto.Clone(resp).(*bigtablepb.ClientConfiguration)
	}
	m.lastFetchedAt = time.Now()
	m.lastErr = nil
	m.lastErrAt = time.Time{}

	// proto.Equal short-circuits the seq bump + listener fan-out when the
	// server returns the same config as before. Downstream consumers don't
	// re-run idempotent work (channel-pool reshapes, session-pool resizes)
	// every poll. Note: proto.Equal treats "unset field" and "explicitly
	// set to default" as distinct, so responses that vary only in whether
	// an explicit 0/false is on the wire will not short-circuit here — a
	// safe false-positive (listeners refire with equivalent state).
	if proto.Equal(resp, m.currentConfig) {
		seq := m.configSeq
		m.recordPoll(PollEvent{
			At:        pollStart,
			Duration:  time.Since(pollStart),
			ConfigSeq: seq,
		})
		m.mu.Unlock()
		btopt.Debugf(m.logger, "bigtable: client configuration unchanged (seq=%d), refreshed validity, skipping listener fan-out", seq)
		return
	}
	m.currentConfig = resp
	m.configSeq++
	seq := m.configSeq
	m.recordPoll(PollEvent{
		At:        pollStart,
		Duration:  time.Since(pollStart),
		ConfigSeq: seq,
	})

	listeners := make([]configListener, 0, len(m.listeners))
	for _, l := range m.listeners {
		listeners = append(listeners, l)
	}
	cfgToNotify := m.currentConfig
	m.mu.Unlock()

	// Suppress listener fires after Close() has begun teardown — the
	// SessionPools they target may already be Close'd.
	if m.isClosed() {
		return
	}
	for _, l := range listeners {
		l(proto.Clone(cfgToNotify).(*bigtablepb.ClientConfiguration), seq)
	}
}
