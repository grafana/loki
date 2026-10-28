// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package session

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	metrics "cloud.google.com/go/bigtable/internal/metrics"
	btransport "cloud.google.com/go/bigtable/internal/transport"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// ErrWriteNotSupported is returned by MutateRow when the resource has
// no write pool — e.g. materialized views, which are read-only.
var ErrWriteNotSupported = errors.New("bigtable/session: write operations not supported on this resource")

// ErrClientClosed is returned when a Read/MutateRow is issued
// against a Client whose Close() has already run. Distinct from
// ErrWriteNotSupported (which is a resource-permanent condition) and
// errReadPoolNil (which flags a bookkeeping bug) so callers can tell a
// closed-client operation from a mis-configured resource.
var ErrClientClosed = errors.New("bigtable/session: Client is closed")

// errReadPoolNil is returned when the READ lazy pool resolves to nil
// with no more specific reason (i.e. the Client isn't closed).
// Reserved for the bookkeeping-drift case — every live resource has a
// read side, so a stray occurrence indicates a caller wired up a
// resource without a read pool or a lazyPool contract broke.
var errReadPoolNil = errors.New("bigtable/session: read pool is nil (bookkeeping drift — every live resource has a read side)")

// sessionTable implements TableAPI. Read and write session
// pools open lazily on first call (see lazyPool). No classic
// fallback — callers that want fallback wrap sessionTable in
// bigtable.TableShim.
type sessionTable struct {
	// tableID is the value stamped as the `table` monitored-resource
	// label on per-attempt metrics. Shape by resource type, matching
	// classic (bigtable/open.go + bigtable/table.go:77):
	//   standard table    → leaf table id ("my-table")
	//   authorized view   → parent table id (Table.table on classic)
	//   materialized view → "" (classic never sets Table.table for MV)
	// Must NOT be the fully-qualified resource name; that would break
	// dashboards that group by short table id across classic + session.
	tableID        string
	readPool       *lazyPool
	writePool      *lazyPool
	readVRpcDesc   btransport.VRpcDescriptor
	writeVRpcDesc  btransport.VRpcDescriptor
	md             metadata.MD
	metricsFactory *metrics.Factory
	// closeRead / closeWrite tear down the specific pools this
	// sessionTable's openers created, on its Close(). Both are supplied by
	// sessionClient as a per-owner poolCloser.Close (see client.go's
	// poolCloser): they close exactly the pool the matching opener minted,
	// by captured identity — never a sibling sessionTable's pool that
	// happens to share a poolKey — and no-op cleanly when the pool was
	// never opened or the client already tore itself down. closeWrite is
	// nil for materialized views (read-only resources), mirroring the nil
	// write side of the openRead/openWrite pair.
	closeRead  func() error
	closeWrite func() error
	// closed is set by Close() BEFORE the releasers run, and re-checked
	// inside the lazy-open wrapper (guardOpen) around openRead/openWrite
	// so an opener whose slow path straddles Close cleans up its own
	// insert instead of leaking a fresh pool that the releaser missed.
	// See guardOpen for the interleaving rules.
	closed atomic.Bool
}

// newSessionTable is the internal constructor. Callers (sessionClient)
// build the lazyPool open + release closures, supply the vRPC
// descriptors, and resource-scoped metadata. metricsFactory may be nil
// to disable per-attempt metrics. closeWrite may be nil for
// materialized views (no write side).
func newSessionTable(
	tableID string,
	openRead func() (Invoker, error),
	openWrite func() (Invoker, error),
	closeRead func() error,
	closeWrite func() error,
	readVRpcDesc btransport.VRpcDescriptor,
	writeVRpcDesc btransport.VRpcDescriptor,
	md metadata.MD,
	metricsFactory *metrics.Factory,
) *sessionTable {
	t := &sessionTable{
		tableID:        tableID,
		readVRpcDesc:   readVRpcDesc,
		writeVRpcDesc:  writeVRpcDesc,
		md:             md,
		metricsFactory: metricsFactory,
		closeRead:      closeRead,
		closeWrite:     closeWrite,
	}
	// Wrap the raw openers with a closed-bit guard so an open racing
	// with Close either bails early or cleans up its own insert. Nil
	// openers stay nil to preserve the MV-write-side "no pool" contract.
	t.readPool = &lazyPool{open: t.guardOpen(openRead, closeRead)}
	t.writePool = &lazyPool{open: t.guardOpen(openWrite, closeWrite)}
	return t
}

// guardOpen wraps a lazy pool opener with a check-before + check-after
// against t.closed. Interleaving cases:
//
//   - Close ran first: early check trips, opener never runs. No leak.
//   - Close runs while opener's slow path holds sessionPoolsMu: post-check
//     trips after opener returns; guardOpen invokes release itself to tear
//     down the pool the opener just inserted. The poolCloser behind
//     release is single-shot, so Close's parallel call harmlessly no-ops
//     (and the opener's own poolCloser.set already handles the case where
//     Close fully preceded set, closing the fresh pool on the spot).
//   - Close runs after guardOpen returns: normal path — Close's releaser
//     closes the pool the opener created. In-flight Invoke on the returned
//     pool may fail with a "pool closed" error; the client was
//     concurrently being torn down, so that's expected.
//
// A nil opener passes through as nil so the MV write side (no write pool)
// stays a lazyPool-with-nil-open ("no session support"), not a
// guarded-nil that would try to Load t.closed on every get.
func (t *sessionTable) guardOpen(open func() (Invoker, error), release func() error) func() (Invoker, error) {
	if open == nil {
		return nil
	}
	return func() (Invoker, error) {
		if t.closed.Load() {
			return nil, ErrClientClosed
		}
		inv, err := open()
		if err != nil {
			return nil, err
		}
		if t.closed.Load() {
			if release != nil {
				_ = release()
			}
			return nil, ErrClientClosed
		}
		return inv, nil
	}
}

// ReadRow dispatches a proto-native ReadRow through a lazily-opened
// READ session pool. Wraps the invoke in RetryingVRpc (idempotent =
// true — reads are always retryable).
//
// Metrics stamping: if ctx already carries a *metrics.Tracer (e.g.
// TableShim stashed one on the classic client path), the per-attempt
// stamps go there. Otherwise sessionTable constructs a new tracer
// from the Client's factory so standalone session.Client users
// still get metrics.
func (t *sessionTable) ReadRow(ctx context.Context, req *btpb.SessionReadRowRequest) (*btpb.SessionReadRowResponse, error) {
	if req == nil {
		return nil, errors.New("bigtable/session: SessionReadRowRequest is nil")
	}
	return dispatch[btransport.ReadRowArgs, btpb.SessionReadRowResponse, *btpb.SessionReadRowResponse](ctx, t, opSpec[btransport.ReadRowArgs, btpb.SessionReadRowResponse, *btpb.SessionReadRowResponse]{
		method:     "ReadRow",
		pool:       t.readPool,
		desc:       t.readVRpcDesc,
		idempotent: true,
		args: btransport.ReadRowArgs{
			RowKey: string(req.GetKey()),
			Filter: req.GetFilter(),
		},
		errNoPool: errReadPoolNil,
	})
}

// MutateRow dispatches a proto-native MutateRow through a lazily-
// opened WRITE session pool. Errors with ErrWriteNotSupported when
// the resource has no write pool (materialized views).
//
// Idempotency is computed from the mutation shape: a SetCell with
// ServerTime is non-idempotent (retrying would create duplicate cells
// with different server timestamps).
func (t *sessionTable) MutateRow(ctx context.Context, req *btpb.SessionMutateRowRequest) (*btpb.SessionMutateRowResponse, error) {
	if req == nil {
		return nil, errors.New("bigtable/session: SessionMutateRowRequest is nil")
	}
	return dispatch[btransport.MutateRowArgs, btpb.SessionMutateRowResponse, *btpb.SessionMutateRowResponse](ctx, t, opSpec[btransport.MutateRowArgs, btpb.SessionMutateRowResponse, *btpb.SessionMutateRowResponse]{
		method:     "MutateRow",
		pool:       t.writePool,
		desc:       t.writeVRpcDesc,
		idempotent: mutationsAreRetryable(req.GetMutations()),
		args: btransport.MutateRowArgs{
			RowKey:    string(req.GetKey()),
			Mutations: req.GetMutations(),
		},
		errNoPool: ErrWriteNotSupported,
	})
}

// opSpec parameterises dispatch over the six axes that used to differ
// between ReadRow and MutateRow: RPC method label (for tracer + error
// context), pool, descriptor, idempotence, encoded args, and the
// error to return when the lazy pool resolves to nil.
//
// Resp is constrained to *R (a pointer type whose element implements
// proto.Message) so the response type-assertion stays compile-time
// safe AND `resp == nil` remains a legal comparison inside dispatch.
type opSpec[Args any, R any, Resp interface {
	*R
	proto.Message
}] struct {
	method     string
	pool       *lazyPool
	desc       btransport.VRpcDescriptor
	idempotent bool
	args       Args
	errNoPool  error
}

// dispatch is the shared body that ReadRow/MutateRow (and any future
// per-op session RPC — Scan, ReadModifyWrite when they land) reduce
// to. Return type is the caller-instantiated Resp so the response
// coerce stays compile-time safe.
func dispatch[Args any, R any, Resp interface {
	*R
	proto.Message
}](ctx context.Context, t *sessionTable, spec opSpec[Args, R, Resp]) (Resp, error) {
	var zero Resp
	pool, poolErr := spec.pool.get()
	if poolErr != nil {
		return zero, fmt.Errorf("session %s pool open: %w", spec.method, poolErr)
	}
	if pool == nil {
		return zero, spec.errNoPool
	}

	ctx = attachOutgoingMetadata(ctx, t.md)
	ctx, mt, ownedTracer := t.ensureTracer(ctx, spec.method)
	if ownedTracer {
		defer mt.RecordOperationCompletion()
	}

	// RetryingOptions is server-driven only — no client-side backoff
	// knobs. MaxAttempts caps non-server-directed retries;
	// server-attached RetryInfo bypasses the cap.
	retryInterceptor := btransport.RetryingVRpc(btransport.RetryingOptions{
		MaxAttempts: 3,
		Idempotent:  spec.idempotent,
	})

	baseHandler := func(attemptCtx context.Context, request interface{}) (interface{}, error) {
		attemptTracer := metrics.FromContext(attemptCtx)
		attemptTracer.RecordAttemptStart()
		result, err := pool.Invoke(attemptCtx, spec.desc, request)
		stampAttempt(attemptCtx, result)
		attemptTracer.RecordAttemptCompletionWithMetadata(nil, nil, err)
		if err != nil {
			return nil, err
		}
		return result.Response, nil
	}

	ctx = btransport.WithVRpcMetadata(ctx, spec.desc.Method(), 0)
	chained := btransport.ChainInterceptors(retryInterceptor)
	res, err := chained(ctx, spec.args, baseHandler)
	if ownedTracer {
		mt.SetCurrOpStatus(metrics.GrpcCodeOf(err))
	}
	if err != nil {
		return zero, fmt.Errorf("session %s vRPC: %w", spec.method, err)
	}
	resp, ok := res.(Resp)
	if !ok || resp == nil {
		return zero, fmt.Errorf("session %s: missing response payload (%T)", spec.method, res)
	}
	return resp, nil
}

// Close releases this sessionTable's underlying read + write session
// pools from the sessionClient's per-resource keyed map. Both release
// closures are idempotent (second call finds the map entry absent
// and no-ops) so double-close from the cache sweeper + explicit
// caller is safe.
//
// Ordering matters: closed is Store'd BEFORE the releasers run so an
// opener whose slow path is straddling this Close (already past its
// early check, still dialing) sees closed=true on its post-insert
// re-check inside guardOpen and cleans up its own insert. See guardOpen.
// Otherwise the releaser would no-op on the empty map and the
// post-insert would leak a fresh pool.
//
// Safety of per-handle pool teardown also rests on a caller-side
// invariant: bigtable.Client's session.TableCache guarantees at-most-one
// sessionTable per resource at any moment, so no co-owner sharing a
// pool with us can be mid-flight when we tear it down. If session.Client
// ever gains a caller that bypasses that cache, this method must gain
// a refcount on the sessionClient side (or the caller must add its own).
//
// Returns the joined read + write teardown errors (nil for the
// materialized-view case where closeWrite is nil).
func (t *sessionTable) Close() error {
	t.closed.Store(true)
	var errs []error
	if t.closeRead != nil {
		if err := t.closeRead(); err != nil {
			errs = append(errs, err)
		}
	}
	if t.closeWrite != nil {
		if err := t.closeWrite(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ensureTracer returns a Tracer stashed on ctx (via metrics.NewContext
// upstream, typically by TableShim on the mixed-mode client path), or
// constructs a fresh one from the Client's factory so
// standalone-Client callers still get metrics exported.
// The bool signals whether sessionTable owns the tracer lifecycle
// (i.e. must call RecordOperationCompletion + SetCurrOpStatus itself).
func (t *sessionTable) ensureTracer(ctx context.Context, method string) (context.Context, *metrics.Tracer, bool) {
	if mt := metrics.FromContext(ctx); mt.BuiltInEnabled {
		return ctx, mt, false
	}
	if t.metricsFactory == nil {
		return ctx, metrics.FromContext(ctx), false // disabled Tracer sentinel; no ownership
	}
	// PR-2 metrics.Factory.CreateTracer returns *Tracer (pointer). Sessionz
	// returned a value and took &fresh; we use fresh directly.
	fresh := t.metricsFactory.CreateTracer(ctx, t.tableID, false)
	fresh.SetMethod(method)
	ctx = metrics.NewContext(ctx, fresh)
	return ctx, fresh, true
}

// stampAttempt copies per-attempt fields off the InvokeResult onto the
// active per-attempt tracer. No-op when metrics are disabled or when
// the field is empty (nil ClusterInfo, zero SentAt, etc.).
//
// Fires observation-only debug tags when ClusterInformation is absent
// on the result — these tags let us confirm whether the "no
// cluster_id label on failed session attempts" pathway drives the
// reported attempt_latencies2 / connectivity_error_count mismatch,
// before landing a behavior-changing fix.
func stampAttempt(ctx context.Context, result btransport.InvokeResult) {
	att := metrics.FromContext(ctx).CurrAttempt()
	if att == nil {
		return
	}
	if result.ClusterInfo != nil {
		att.SetClusterID(result.ClusterInfo.ClusterId)
		att.SetZoneID(result.ClusterInfo.ZoneId)
	}
	// ClientBlockingLatency = SentAt - AttemptStartTime: elapsed time
	// from attempt-start to when the request was Sent on the bidi
	// stream. Analogous to gax's per-attempt blocking latency on the
	// classic unary path, but measured across the vRPC dispatch instead
	// of the gRPC unary call.
	if !result.SentAt.IsZero() && !att.StartTime().IsZero() {
		att.SetClientBlockingLatency(metrics.ConvertToMs(result.SentAt.Sub(att.StartTime())))
	}
	// Session path leaves ServerLatency at its zero value; tracer.go still
	// emits the histogram sample because serverLatencyErr is nil. Follow-up:
	// gate emission on a serverLatencySet flag so session attempts truly
	// don't contribute to server_latencies. See CLIENT_SIDE_METRICS_SPEC.md
	// #2 ("server_latencies" + connectivity interlock).
	if result.PeerInfo != nil {
		att.SetTransportType(btransport.TransportTypeName(result.PeerInfo.GetTransportType()))
		att.SetTransportRegion(result.PeerInfo.GetApplicationFrontendRegion())
		att.SetTransportZone(result.PeerInfo.GetApplicationFrontendZone())
		att.SetTransportSubZone(result.PeerInfo.GetApplicationFrontendSubzone())
	}
}

// attachOutgoingMetadata is the internal-session equivalent of
// bigtable.mergeOutgoingMetadata: joins the resource-scoped headers
// onto the outgoing gRPC context so any downstream inspection
// (interceptor, tracer) sees them.
func attachOutgoingMetadata(ctx context.Context, md metadata.MD) context.Context {
	if md == nil {
		return ctx
	}
	if existing, ok := metadata.FromOutgoingContext(ctx); ok {
		return metadata.NewOutgoingContext(ctx, metadata.Join(existing, md))
	}
	return metadata.NewOutgoingContext(ctx, md)
}

// mutationsAreRetryable determines whether a mutation set is safe to
// replay. A mutation is idempotent iff:
//   - every SetCell carries an explicit timestamp (not ServerTime = -1), and
//   - no AddToCell / MergeToCell operations are present (both are
//     inherently non-idempotent — replaying them changes the result).
//
// Based on bigtable.mutationsAreRetryable (duplicated to avoid an
// import cycle); session enforces the AddToCell / MergeToCell check
// that the classic helper is missing today.
func mutationsAreRetryable(muts []*btpb.Mutation) bool {
	const serverTime int64 = -1
	for _, mut := range muts {
		if setCell := mut.GetSetCell(); setCell != nil && setCell.TimestampMicros == serverTime {
			return false
		}
		if mut.GetAddToCell() != nil || mut.GetMergeToCell() != nil {
			return false
		}
	}
	return true
}
