/*
Copyright 2015 Google LLC

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

package bigtable // import "cloud.google.com/go/bigtable"

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	btopt "cloud.google.com/go/bigtable/internal/option"
	"cloud.google.com/go/internal/trace"
	gax "github.com/googleapis/gax-go/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"
	gtransport "google.golang.org/api/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	// Install google-c2p resolver, which is required for direct path.
	_ "google.golang.org/grpc/xds/googledirectpath"
	// Install RLS load balancer policy, which is needed for gRPC RLS.
	_ "google.golang.org/grpc/balancer/rls"
)

const prodAddr = "bigtable.googleapis.com:443"
const mtlsProdAddr = "bigtable.mtls.googleapis.com:443"
const featureFlagsHeaderKey = "bigtable-features"

var errNegativeRowLimit = errors.New("bigtable: row limit cannot be negative")

// Client is a client for reading and writing data to tables in an instance.
//
// A Client is safe to use concurrently, except for its Close method.
type Client struct {
	connPool             gtransport.ConnPool
	client               btpb.BigtableClient
	project, instance    string
	appProfile           string
	metricsTracerFactory *builtinMetricsTracerFactory
}

// ClientConfig has configurations for the client.
type ClientConfig struct {
	// The id of the app profile to associate with all data operations sent from this client.
	// If unspecified, the default app profile for the instance will be used.
	AppProfile string

	// If not set or set to nil, client side metrics will be collected and exported
	//
	// To disable client side metrics, set 'MetricsProvider' to 'NoopMetricsProvider'
	//
	// TODO: support user provided meter provider
	MetricsProvider MetricsProvider
}

// MetricsProvider is a wrapper for built in metrics meter provider
type MetricsProvider interface {
	isMetricsProvider()
}

// NoopMetricsProvider can be used to disable built in metrics
type NoopMetricsProvider struct{}

func (NoopMetricsProvider) isMetricsProvider() {}

// NewClient creates a new Client for a given project and instance.
// The default ClientConfig will be used.
func NewClient(ctx context.Context, project, instance string, opts ...option.ClientOption) (*Client, error) {
	return NewClientWithConfig(ctx, project, instance, ClientConfig{}, opts...)
}

// NewClientWithConfig creates a new client with the given config.
func NewClientWithConfig(ctx context.Context, project, instance string, config ClientConfig, opts ...option.ClientOption) (*Client, error) {
	metricsProvider := config.MetricsProvider
	if emulatorAddr := os.Getenv("BIGTABLE_EMULATOR_HOST"); emulatorAddr != "" {
		// Do not emit metrics when emulator is being used
		metricsProvider = NoopMetricsProvider{}
	}

	// Create a OpenTelemetry metrics configuration
	metricsTracerFactory, err := newBuiltinMetricsTracerFactory(ctx, project, instance, config.AppProfile, metricsProvider, opts...)
	if err != nil {
		return nil, err
	}

	o, err := btopt.DefaultClientOptions(prodAddr, mtlsProdAddr, Scope, clientUserAgent)
	if err != nil {
		return nil, err
	}
	// Add gRPC client interceptors to supply Google client information. No external interceptors are passed.
	o = append(o, btopt.ClientInterceptorOptions(nil, nil)...)

	// Default to a small connection pool that can be overridden.
	o = append(o,
		option.WithGRPCConnectionPool(4),
		// Set the max size to correspond to server-side limits.
		option.WithGRPCDialOption(grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(1<<28), grpc.MaxCallRecvMsgSize(1<<28))),
	)

	// Allow non-default service account in DirectPath.
	o = append(o, internaloption.AllowNonDefaultServiceAccount(true))
	o = append(o, opts...)

	// TODO(b/372244283): Remove after b/358175516 has been fixed
	asyncRefreshMetricAttrs := metricsTracerFactory.clientAttributes
	asyncRefreshMetricAttrs = append(asyncRefreshMetricAttrs,
		attribute.String(metricLabelKeyTag, "async_refresh_dry_run"),
		// Table, cluster and zone are unknown at this point
		// Use default values
		attribute.String(monitoredResLabelKeyTable, defaultTable),
		attribute.String(monitoredResLabelKeyCluster, defaultCluster),
		attribute.String(monitoredResLabelKeyZone, defaultZone),
	)
	o = append(o, internaloption.EnableAsyncRefreshDryRun(func() {
		metricsTracerFactory.debugTags.Add(context.Background(), 1,
			metric.WithAttributes(asyncRefreshMetricAttrs...))
	}))

	connPool, err := gtransport.DialPool(ctx, o...)
	if err != nil {
		return nil, err
	}

	return &Client{
		connPool:             connPool,
		client:               btpb.NewBigtableClient(connPool),
		project:              project,
		instance:             instance,
		appProfile:           config.AppProfile,
		metricsTracerFactory: metricsTracerFactory,
	}, nil
}

// Close closes the Client.
func (c *Client) Close() error {
	if c.metricsTracerFactory != nil {
		c.metricsTracerFactory.shutdown()
	}
	return c.connPool.Close()
}

var (
	idempotentRetryCodes  = []codes.Code{codes.DeadlineExceeded, codes.Unavailable, codes.Aborted}
	isIdempotentRetryCode = make(map[codes.Code]bool)
	retryOptions          = []gax.CallOption{
		gax.WithRetry(func() gax.Retryer {
			backoff := gax.Backoff{
				Initial:    100 * time.Millisecond,
				Max:        2 * time.Second,
				Multiplier: 1.2,
			}
			return &bigtableRetryer{
				Retryer: gax.OnCodes(idempotentRetryCodes, backoff),
				Backoff: backoff,
			}
		}),
	}
	retryableInternalErrMsgs = []string{
		"stream terminated by RST_STREAM", // Retry similar to spanner client. Special case due to https://github.com/googleapis/google-cloud-go/issues/6476
	}
)

// bigtableRetryer extends the generic gax Retryer, but also checks
// error messages to check if operation can be retried
type bigtableRetryer struct {
	gax.Retryer
	gax.Backoff
}

func containsAny(str string, substrs []string) bool {
	for _, substr := range substrs {
		if strings.Contains(str, substr) {
			return true
		}
	}
	return false
}

func (r *bigtableRetryer) Retry(err error) (time.Duration, bool) {
	if status.Code(err) == codes.Internal && containsAny(err.Error(), retryableInternalErrMsgs) {
		return r.Backoff.Pause(), true
	}

	delay, shouldRetry := r.Retryer.Retry(err)
	if !shouldRetry {
		return 0, false
	}

	return delay, true
}

func init() {
	for _, code := range idempotentRetryCodes {
		isIdempotentRetryCode[code] = true
	}
}

// Convert error to grpc status error
func convertToGrpcStatusErr(err error) (codes.Code, error) {
	if err == nil {
		return codes.OK, nil
	}

	if errStatus, ok := status.FromError(err); ok {
		return errStatus.Code(), status.Error(errStatus.Code(), errStatus.Message())
	}

	ctxStatus := status.FromContextError(err)
	if ctxStatus.Code() != codes.Unknown {
		return ctxStatus.Code(), status.Error(ctxStatus.Code(), ctxStatus.Message())
	}

	return codes.Unknown, err
}

func (c *Client) fullTableName(table string) string {
	return fmt.Sprintf("projects/%s/instances/%s/tables/%s", c.project, c.instance, table)
}

func (c *Client) fullAuthorizedViewName(table string, authorizedView string) string {
	return fmt.Sprintf("projects/%s/instances/%s/tables/%s/authorizedViews/%s", c.project, c.instance, table, authorizedView)
}

func (c *Client) requestParamsHeaderValue(table string) string {
	return fmt.Sprintf("table_name=%s&app_profile_id=%s", url.QueryEscape(c.fullTableName(table)), url.QueryEscape(c.appProfile))
}

// mergeOutgoingMetadata returns a context populated by the existing outgoing
// metadata merged with the provided mds.
func mergeOutgoingMetadata(ctx context.Context, mds ...metadata.MD) context.Context {
	ctxMD, _ := metadata.FromOutgoingContext(ctx)
	// The ordering matters, hence why ctxMD comes first.
	allMDs := append([]metadata.MD{ctxMD}, mds...)
	return metadata.NewOutgoingContext(ctx, metadata.Join(allMDs...))
}

// TableAPI interface allows existing data APIs to be applied to either an authorized view or a table.
type TableAPI interface {
	ReadRows(ctx context.Context, arg RowSet, f func(Row) bool, opts ...ReadOption) error
	ReadRow(ctx context.Context, row string, opts ...ReadOption) (Row, error)
	Apply(ctx context.Context, row string, m *Mutation, opts ...ApplyOption) error
	ApplyBulk(ctx context.Context, rowKeys []string, muts []*Mutation, opts ...ApplyOption) ([]error, error)
	SampleRowKeys(ctx context.Context) ([]string, error)
	ApplyReadModifyWrite(ctx context.Context, row string, m *ReadModifyWrite) (Row, error)
}

type tableImpl struct {
	Table
}

// A Table refers to a table.
//
// A Table is safe to use concurrently.
type Table struct {
	c     *Client
	table string

	// Metadata to be sent with each request.
	md             metadata.MD
	authorizedView string
}

// newFeatureFlags creates the feature flags `bigtable-features` header
// to be sent on each request. This includes all features supported and
// and enabled on the client
func (c *Client) newFeatureFlags() metadata.MD {
	ff := btpb.FeatureFlags{
		ReverseScans:             true,
		LastScannedRowResponses:  true,
		ClientSideMetricsEnabled: c.metricsTracerFactory.enabled,
	}

	val := ""
	b, err := proto.Marshal(&ff)
	if err == nil {
		val = base64.URLEncoding.EncodeToString(b)
	}

	return metadata.Pairs(featureFlagsHeaderKey, val)
}

// Open opens a table.
func (c *Client) Open(table string) *Table {
	return &Table{
		c:     c,
		table: table,
		md: metadata.Join(metadata.Pairs(
			resourcePrefixHeader, c.fullTableName(table),
			requestParamsHeader, c.requestParamsHeaderValue(table),
		), c.newFeatureFlags()),
	}
}

// OpenTable opens a table.
func (c *Client) OpenTable(table string) TableAPI {
	return &tableImpl{Table{
		c:     c,
		table: table,
		md: metadata.Join(metadata.Pairs(
			resourcePrefixHeader, c.fullTableName(table),
			requestParamsHeader, c.requestParamsHeaderValue(table),
		), c.newFeatureFlags()),
	}}
}

// OpenAuthorizedView opens an authorized view.
func (c *Client) OpenAuthorizedView(table, authorizedView string) TableAPI {
	return &tableImpl{Table{
		c:     c,
		table: table,
		md: metadata.Join(metadata.Pairs(
			resourcePrefixHeader, c.fullAuthorizedViewName(table, authorizedView),
			requestParamsHeader, c.requestParamsHeaderValue(table),
		), c.newFeatureFlags()),
		authorizedView: authorizedView,
	}}
}

func (ti *tableImpl) ReadRows(ctx context.Context, arg RowSet, f func(Row) bool, opts ...ReadOption) error {
	return ti.Table.ReadRows(ctx, arg, f, opts...)
}

func (ti *tableImpl) Apply(ctx context.Context, row string, m *Mutation, opts ...ApplyOption) error {
	return ti.Table.Apply(ctx, row, m, opts...)
}

func (ti *tableImpl) ApplyBulk(ctx context.Context, rowKeys []string, muts []*Mutation, opts ...ApplyOption) ([]error, error) {
	return ti.Table.ApplyBulk(ctx, rowKeys, muts, opts...)
}

func (ti *tableImpl) SampleRowKeys(ctx context.Context) ([]string, error) {
	return ti.Table.SampleRowKeys(ctx)
}

func (ti *tableImpl) ApplyReadModifyWrite(ctx context.Context, row string, m *ReadModifyWrite) (Row, error) {
	return ti.Table.ApplyReadModifyWrite(ctx, row, m)
}

func (ti *tableImpl) newBuiltinMetricsTracer(ctx context.Context, isStreaming bool) *builtinMetricsTracer {
	return ti.Table.newBuiltinMetricsTracer(ctx, isStreaming)
}

// TODO(dsymonds): Read method that returns a sequence of ReadItems.

// ReadRows reads rows from a table. f is called for each row.
// If f returns false, the stream is shut down and ReadRows returns.
// f owns its argument, and f is called serially in order by row key.
// f will be executed in the same Go routine as the caller.
//
// By default, the yielded rows will contain all values in all cells.
// Use RowFilter to limit the cells returned.
func (t *Table) ReadRows(ctx context.Context, arg RowSet, f func(Row) bool, opts ...ReadOption) (err error) {
	ctx = mergeOutgoingMetadata(ctx, t.md)
	ctx = trace.StartSpan(ctx, "cloud.google.com/go/bigtable.ReadRows")
	defer func() { trace.EndSpan(ctx, err) }()

	mt := t.newBuiltinMetricsTracer(ctx, true)
	defer recordOperationCompletion(mt)

	err = t.readRows(ctx, arg, f, mt, opts...)
	statusCode, statusErr := convertToGrpcStatusErr(err)
	mt.currOp.setStatus(statusCode.String())
	return statusErr
}

func (t *Table) readRows(ctx context.Context, arg RowSet, f func(Row) bool, mt *builtinMetricsTracer, opts ...ReadOption) (err error) {
	var prevRowKey string
	attrMap := make(map[string]interface{})

	numRowsRead := int64(0)
	rowLimitSet := false
	intialRowLimit := int64(0)
	for _, opt := range opts {
		if l, ok := opt.(limitRows); ok {
			rowLimitSet = true
			intialRowLimit = l.limit
		}
	}
	if intialRowLimit < 0 {
		return errNegativeRowLimit
	}

	err = gaxInvokeWithRecorder(ctx, mt, "ReadRows", func(ctx context.Context, headerMD, trailerMD *metadata.MD, _ gax.CallSettings) error {
		if rowLimitSet && numRowsRead >= intialRowLimit {
			return nil
		}

		req := &btpb.ReadRowsRequest{
			AppProfileId: t.c.appProfile,
		}
		if t.authorizedView == "" {
			req.TableName = t.c.fullTableName(t.table)
		} else {
			req.AuthorizedViewName = t.c.fullAuthorizedViewName(t.table, t.authorizedView)
		}

		if arg != nil {
			if !arg.valid() {
				// Empty row set, no need to make an API call.
				// NOTE: we must return early if arg == RowList{} because reading
				// an empty RowList from bigtable returns all rows from that table.
				return nil
			}
			req.Rows = arg.proto()
		}
		settings := makeReadSettings(req, numRowsRead)
		for _, opt := range opts {
			opt.set(&settings)
		}
		ctx, cancel := context.WithCancel(ctx) // for aborting the stream
		defer cancel()

		startTime := time.Now()
		stream, err := t.c.client.ReadRows(ctx, req)
		if err != nil {
			return err
		}

		var cr *chunkReader
		if req.Reversed {
			cr = newReverseChunkReader()
		} else {
			cr = newChunkReader()
		}

		// Ignore error since header is only being used to record builtin metrics
		// Failure to record metrics should not fail the operation
		*headerMD, _ = stream.Header()
		res := new(btpb.ReadRowsResponse)
		for {
			proto.Reset(res)
			err := stream.RecvMsg(res)
			if err == io.EOF {
				*trailerMD = stream.Trailer()
				break
			}
			if err != nil {
				*trailerMD = stream.Trailer()
				// Reset arg for next Invoke call.
				if arg == nil {
					// Should be lowest possible key value, an empty byte array
					arg = InfiniteRange("")
				}
				if req.Reversed {
					arg = arg.retainRowsBefore(prevRowKey)
				} else {
					arg = arg.retainRowsAfter(prevRowKey)
				}
				attrMap["rowKey"] = prevRowKey
				attrMap["error"] = err.Error()
				attrMap["time_secs"] = time.Since(startTime).Seconds()
				trace.TracePrintf(ctx, attrMap, "Retry details in ReadRows")
				return err
			}
			attrMap["time_secs"] = time.Since(startTime).Seconds()
			attrMap["rowCount"] = len(res.Chunks)
			trace.TracePrintf(ctx, attrMap, "Details in ReadRows")

			for _, cc := range res.Chunks {
				row, err := cr.Process(cc)
				if err != nil {
					// No need to prepare for a retry, this is an unretryable error.
					return err
				}
				if row == nil {
					continue
				}
				prevRowKey = row.Key()
				continueReading := f(row)
				numRowsRead++
				if !continueReading {
					// Cancel and drain stream.
					cancel()
					for {
						proto.Reset(res)
						if err := stream.RecvMsg(res); err != nil {
							*trailerMD = stream.Trailer()
							// The stream has ended. We don't return an error
							// because the caller has intentionally interrupted the scan.
							return nil
						}
					}
				}
			}

			if res.LastScannedRowKey != nil {
				prevRowKey = string(res.LastScannedRowKey)
			}

			// Handle any incoming RequestStats. This should happen at most once.
			if res.RequestStats != nil && settings.fullReadStatsFunc != nil {
				stats := makeFullReadStats(res.RequestStats)
				settings.fullReadStatsFunc(&stats)
			}

			if err := cr.Close(); err != nil {
				// No need to prepare for a retry, this is an unretryable error.
				return err
			}
		}
		return err
	}, retryOptions...)

	return err
}

// ReadRow is a convenience implementation of a single-row reader.
// A missing row will return nil for both Row and error.
func (t *Table) ReadRow(ctx context.Context, row string, opts ...ReadOption) (Row, error) {
	var r Row

	opts = append([]ReadOption{LimitRows(1)}, opts...)
	err := t.ReadRows(ctx, SingleRow(row), func(rr Row) bool {
		r = rr
		return true
	}, opts...)
	return r, err
}

// decodeFamilyProto adds the cell data from f to the given row.
func decodeFamilyProto(r Row, row string, f *btpb.Family) {
	fam := f.Name // does not have colon
	for _, col := range f.Columns {
		for _, cell := range col.Cells {
			ri := ReadItem{
				Row:       row,
				Column:    fam + ":" + string(col.Qualifier),
				Timestamp: Timestamp(cell.TimestampMicros),
				Value:     cell.Value,
			}
			r[fam] = append(r[fam], ri)
		}
	}
}

// RowSet is a set of rows to be read. It is satisfied by RowList, RowRange and RowRangeList.
// The serialized size of the RowSet must be no larger than 1MiB.
type RowSet interface {
	proto() *btpb.RowSet

	// retainRowsAfter returns a new RowSet that does not include the
	// given row key or any row key lexicographically less than it.
	retainRowsAfter(lastRowKey string) RowSet

	// retainRowsBefore returns a new RowSet that does not include the
	// given row key or any row key lexicographically greater than it.
	retainRowsBefore(lastRowKey string) RowSet

	// Valid reports whether this set can cover at least one row.
	valid() bool
}

// RowList is a sequence of row keys.
type RowList []string

func (r RowList) proto() *btpb.RowSet {
	keys := make([][]byte, len(r))
	for i, row := range r {
		keys[i] = []byte(row)
	}
	return &btpb.RowSet{RowKeys: keys}
}

func (r RowList) retainRowsAfter(lastRowKey string) RowSet {
	var retryKeys RowList
	for _, key := range r {
		if key > lastRowKey {
			retryKeys = append(retryKeys, key)
		}
	}
	return retryKeys
}

func (r RowList) retainRowsBefore(lastRowKey string) RowSet {
	var retryKeys RowList
	for _, key := range r {
		if key < lastRowKey {
			retryKeys = append(retryKeys, key)
		}
	}
	return retryKeys
}

func (r RowList) valid() bool {
	return len(r) > 0
}

type rangeBoundType int64

const (
	rangeUnbounded rangeBoundType = iota
	rangeOpen
	rangeClosed
)

// A RowRange describes a range of rows between the start and end key. Start and
// end keys may be rangeOpen, rangeClosed or rangeUnbounded.
type RowRange struct {
	startBound rangeBoundType
	start      string
	endBound   rangeBoundType
	end        string
}

// NewRange returns the new RowRange [begin, end).
func NewRange(begin, end string) RowRange {
	return createRowRange(rangeClosed, begin, rangeOpen, end)
}

// NewClosedOpenRange returns the RowRange consisting of all greater than or
// equal to the start and less than the end: [start, end).
func NewClosedOpenRange(start, end string) RowRange {
	return createRowRange(rangeClosed, start, rangeOpen, end)
}

// NewOpenClosedRange returns the RowRange consisting of all keys greater than
// the start and less than or equal to the end: (start, end].
func NewOpenClosedRange(start, end string) RowRange {
	return createRowRange(rangeOpen, start, rangeClosed, end)
}

// NewOpenRange returns the RowRange consisting of all keys greater than the
// start and less than the end: (start, end).
func NewOpenRange(start, end string) RowRange {
	return createRowRange(rangeOpen, start, rangeOpen, end)
}

// NewClosedRange returns the RowRange consisting of all keys greater than or
// equal to the start and less than or equal to the end: [start, end].
func NewClosedRange(start, end string) RowRange {
	return createRowRange(rangeClosed, start, rangeClosed, end)
}

// PrefixRange returns a RowRange consisting of all keys starting with the prefix.
func PrefixRange(prefix string) RowRange {
	end := prefixSuccessor(prefix)
	return createRowRange(rangeClosed, prefix, rangeOpen, end)
}

// InfiniteRange returns the RowRange consisting of all keys at least as
// large as start: [start, ∞).
func InfiniteRange(start string) RowRange {
	return createRowRange(rangeClosed, start, rangeUnbounded, "")
}

// InfiniteReverseRange returns the RowRange consisting of all keys less than or
// equal to the end: (∞, end].
func InfiniteReverseRange(end string) RowRange {
	return createRowRange(rangeUnbounded, "", rangeClosed, end)
}

// createRowRange creates a new RowRange, normalizing start and end
// rangeBoundType to rangeUnbounded if they're empty strings because empty
// strings also represent unbounded keys
func createRowRange(startBound rangeBoundType, start string, endBound rangeBoundType, end string) RowRange {
	// normalize start bound type
	if start == "" {
		startBound = rangeUnbounded
	}
	// normalize end bound type
	if end == "" {
		endBound = rangeUnbounded
	}
	return RowRange{
		startBound: startBound,
		start:      start,
		endBound:   endBound,
		end:        end,
	}
}

// Unbounded tests whether a RowRange is unbounded.
func (r RowRange) Unbounded() bool {
	return r.startBound == rangeUnbounded || r.endBound == rangeUnbounded
}

// Contains says whether the RowRange contains the key.
func (r RowRange) Contains(row string) bool {
	switch r.startBound {
	case rangeOpen:
		if r.start >= row {
			return false
		}
	case rangeClosed:
		if r.start > row {
			return false
		}
	case rangeUnbounded:
	}

	switch r.endBound {
	case rangeOpen:
		if r.end <= row {
			return false
		}
	case rangeClosed:
		if r.end < row {
			return false
		}
	case rangeUnbounded:
	}

	return true
}

// String provides a printable description of a RowRange.
func (r RowRange) String() string {
	var startStr string
	switch r.startBound {
	case rangeOpen:
		startStr = "(" + strconv.Quote(r.start)
	case rangeClosed:
		startStr = "[" + strconv.Quote(r.start)
	case rangeUnbounded:
		startStr = "(∞"
	}

	var endStr string
	switch r.endBound {
	case rangeOpen:
		endStr = strconv.Quote(r.end) + ")"
	case rangeClosed:
		endStr = strconv.Quote(r.end) + "]"
	case rangeUnbounded:
		endStr = "∞)"
	}

	return fmt.Sprintf("%s,%s", startStr, endStr)
}

func (r RowRange) proto() *btpb.RowSet {
	rr := &btpb.RowRange{}

	switch r.startBound {
	case rangeOpen:
		rr.StartKey = &btpb.RowRange_StartKeyOpen{StartKeyOpen: []byte(r.start)}
	case rangeClosed:
		rr.StartKey = &btpb.RowRange_StartKeyClosed{StartKeyClosed: []byte(r.start)}
	case rangeUnbounded:
		// leave unbounded
	}

	switch r.endBound {
	case rangeOpen:
		rr.EndKey = &btpb.RowRange_EndKeyOpen{EndKeyOpen: []byte(r.end)}
	case rangeClosed:
		rr.EndKey = &btpb.RowRange_EndKeyClosed{EndKeyClosed: []byte(r.end)}
	case rangeUnbounded:
		// leave unbounded
	}

	return &btpb.RowSet{RowRanges: []*btpb.RowRange{rr}}
}

func (r RowRange) retainRowsAfter(lastRowKey string) RowSet {
	if lastRowKey == "" || lastRowKey < r.start {
		return r
	}

	return RowRange{
		// Set the beginning of the range to the row after the last scanned.
		startBound: rangeOpen,
		start:      lastRowKey,
		endBound:   r.endBound,
		end:        r.end,
	}
}

func (r RowRange) retainRowsBefore(lastRowKey string) RowSet {
	if lastRowKey == "" || (r.endBound != rangeUnbounded && r.end < lastRowKey) {
		return r
	}

	return RowRange{
		startBound: r.startBound,
		start:      r.start,
		endBound:   rangeOpen,
		end:        lastRowKey,
	}
}

func (r RowRange) valid() bool {
	// If either end is unbounded, then the range is always valid.
	if r.Unbounded() {
		return true
	}

	// If either end is an open interval, then the start must be strictly less
	// than the end and since neither end is unbounded, we don't have to check
	// for empty strings.
	if r.startBound == rangeOpen || r.endBound == rangeOpen {
		return r.start < r.end
	}

	// At this point both endpoints must be closed, which makes [a,a] a valid
	// interval
	return r.start <= r.end
}

// RowRangeList is a sequence of RowRanges representing the union of the ranges.
type RowRangeList []RowRange

func (r RowRangeList) proto() *btpb.RowSet {
	ranges := make([]*btpb.RowRange, len(r))
	for i, rr := range r {
		// RowRange.proto() returns a RowSet with a single element RowRange array
		ranges[i] = rr.proto().RowRanges[0]
	}
	return &btpb.RowSet{RowRanges: ranges}
}

func (r RowRangeList) retainRowsAfter(lastRowKey string) RowSet {
	if lastRowKey == "" {
		return r
	}
	// Return a list of any range that has not yet been completely processed
	var ranges RowRangeList
	for _, rr := range r {
		retained := rr.retainRowsAfter(lastRowKey)
		if retained.valid() {
			ranges = append(ranges, retained.(RowRange))
		}
	}
	return ranges
}

func (r RowRangeList) retainRowsBefore(lastRowKey string) RowSet {
	if lastRowKey == "" {
		return r
	}
	// Return a list of any range that has not yet been completely processed
	var ranges RowRangeList
	for _, rr := range r {
		retained := rr.retainRowsBefore(lastRowKey)
		if retained.valid() {
			ranges = append(ranges, retained.(RowRange))
		}
	}
	return ranges
}

func (r RowRangeList) valid() bool {
	for _, rr := range r {
		if rr.valid() {
			return true
		}
	}
	return false
}

// SingleRow returns a RowSet for reading a single row.
func SingleRow(row string) RowSet {
	return RowList{row}
}

// prefixSuccessor returns the lexically smallest string greater than the
// prefix, if it exists, or "" otherwise.  In either case, it is the string
// needed for the Limit of a RowRange.
func prefixSuccessor(prefix string) string {
	if prefix == "" {
		return "" // infinite range
	}
	n := len(prefix)
	for n--; n >= 0 && prefix[n] == '\xff'; n-- {
	}
	if n == -1 {
		return ""
	}
	ans := []byte(prefix[:n])
	ans = append(ans, prefix[n]+1)
	return string(ans)
}

// ReadIterationStats captures information about the iteration of rows or cells over the course of
// a read, e.g. how many results were scanned in a read operation versus the results returned.
type ReadIterationStats struct {
	// The cells returned as part of the request.
	CellsReturnedCount int64

	// The cells seen (scanned) as part of the request. This includes the count of cells returned, as
	// captured below.
	CellsSeenCount int64

	// The rows returned as part of the request.
	RowsReturnedCount int64

	// The rows seen (scanned) as part of the request. This includes the count of rows returned, as
	// captured below.
	RowsSeenCount int64
}

// RequestLatencyStats provides a measurement of the latency of the request as it interacts with
// different systems over its lifetime, e.g. how long the request took to execute within a frontend
// server.
type RequestLatencyStats struct {
	// The latency measured by the frontend server handling this request, from when the request was
	// received, to when this value is sent back in the response. For more context on the component
	// that is measuring this latency, see: https://cloud.google.com/bigtable/docs/overview
	FrontendServerLatency time.Duration
}

// FullReadStats captures all known information about a read.
type FullReadStats struct {
	// Iteration stats describe how efficient the read is, e.g. comparing rows seen vs. rows
	// returned or cells seen vs cells returned can provide an indication of read efficiency
	// (the higher the ratio of seen to retuned the better).
	ReadIterationStats ReadIterationStats

	// Request latency stats describe the time taken to complete a request, from the server
	// side.
	RequestLatencyStats RequestLatencyStats
}

// Returns a FullReadStats populated from a RequestStats. This assumes the stats view is
// REQUEST_STATS_FULL. That is the only stats view currently supported.
func makeFullReadStats(reqStats *btpb.RequestStats) FullReadStats {
	statsView := reqStats.GetFullReadStatsView()
	readStats := statsView.ReadIterationStats
	latencyStats := statsView.RequestLatencyStats
	return FullReadStats{
		ReadIterationStats: ReadIterationStats{
			CellsReturnedCount: readStats.CellsReturnedCount,
			CellsSeenCount:     readStats.CellsSeenCount,
			RowsReturnedCount:  readStats.RowsReturnedCount,
			RowsSeenCount:      readStats.RowsSeenCount},
		RequestLatencyStats: RequestLatencyStats{
			FrontendServerLatency: latencyStats.FrontendServerLatency.AsDuration()}}
}

// FullReadStatsFunc describes a callback that receives a FullReadStats for evaluation.
type FullReadStatsFunc func(*FullReadStats)

// readSettings is a collection of objects that can be modified by ReadOption instances to apply settings.
type readSettings struct {
	req               *btpb.ReadRowsRequest
	fullReadStatsFunc FullReadStatsFunc
	numRowsRead       int64
}

func makeReadSettings(req *btpb.ReadRowsRequest, numRowsRead int64) readSettings {
	return readSettings{req, nil, numRowsRead}
}

// A ReadOption is an optional argument to ReadRows.
type ReadOption interface {
	// set modifies the request stored in the settings
	set(settings *readSettings)
}

// RowFilter returns a ReadOption that applies f to the contents of read rows.
//
// If multiple RowFilters are provided, only the last is used. To combine filters,
// use ChainFilters or InterleaveFilters instead.
func RowFilter(f Filter) ReadOption { return rowFilter{f} }

type rowFilter struct{ f Filter }

func (rf rowFilter) set(settings *readSettings) { settings.req.Filter = rf.f.proto() }

// LimitRows returns a ReadOption that will end the number of rows to be read.
func LimitRows(limit int64) ReadOption { return limitRows{limit} }

type limitRows struct{ limit int64 }

func (lr limitRows) set(settings *readSettings) {
	// Since 'numRowsRead' out of 'limit' requested rows have already been read,
	// the subsequest requests should fetch only the remaining rows.
	settings.req.RowsLimit = lr.limit - settings.numRowsRead
}

// WithFullReadStats returns a ReadOption that will request FullReadStats
// and invoke the given callback on the resulting FullReadStats.
func WithFullReadStats(f FullReadStatsFunc) ReadOption { return withFullReadStats{f} }

type withFullReadStats struct {
	f FullReadStatsFunc
}

func (wrs withFullReadStats) set(settings *readSettings) {
	settings.req.RequestStatsView = btpb.ReadRowsRequest_REQUEST_STATS_FULL
	settings.fullReadStatsFunc = wrs.f
}

// ReverseScan returns a RadOption that will reverse the results of a Scan.
// The rows will be streamed in reverse lexiographic order of the keys. The row key ranges of the RowSet are
// still expected to be oriented the same way as forwards. ie [a,c] where a <= c. The row content
// will remain unchanged from the ordering forward scans. This is particularly useful to get the
// last N records before a key:
//
//	table.ReadRows(ctx, NewOpenClosedRange("", "key"), func(row bigtable.Row) bool {
//	   return true
//	}, bigtable.ReverseScan(), bigtable.LimitRows(10))
func ReverseScan() ReadOption {
	return reverseScan{}
}

type reverseScan struct{}

func (rs reverseScan) set(settings *readSettings) {
	settings.req.Reversed = true
}

// mutationsAreRetryable returns true if all mutations are idempotent
// and therefore retryable. A mutation is idempotent iff all cell timestamps
// have an explicit timestamp set and do not rely on the timestamp being set on the server.
func mutationsAreRetryable(muts []*btpb.Mutation) bool {
	serverTime := int64(ServerTime)
	for _, mut := range muts {
		setCell := mut.GetSetCell()
		if setCell != nil && setCell.TimestampMicros == serverTime {
			return false
		}
	}
	return true
}

// Overriden in tests
var maxMutations = 100000

// Apply mutates a row atomically. A mutation must contain at least one
// operation and at most 100000 operations.
func (t *Table) Apply(ctx context.Context, row string, m *Mutation, opts ...ApplyOption) (err error) {
	ctx = mergeOutgoingMetadata(ctx, t.md)
	ctx = trace.StartSpan(ctx, "cloud.google.com/go/bigtable/Apply")
	defer func() { trace.EndSpan(ctx, err) }()
	mt := t.newBuiltinMetricsTracer(ctx, false)
	defer recordOperationCompletion(mt)

	err = t.apply(ctx, mt, row, m, opts...)
	statusCode, statusErr := convertToGrpcStatusErr(err)
	mt.currOp.setStatus(statusCode.String())
	return statusErr
}

func (t *Table) apply(ctx context.Context, mt *builtinMetricsTracer, row string, m *Mutation, opts ...ApplyOption) (err error) {
	after := func(res proto.Message) {
		for _, o := range opts {
			o.after(res)
		}
	}

	var callOptions []gax.CallOption
	if !m.isConditional {
		req := &btpb.MutateRowRequest{
			AppProfileId: t.c.appProfile,
			RowKey:       []byte(row),
			Mutations:    m.ops,
		}
		if t.authorizedView == "" {
			req.TableName = t.c.fullTableName(t.table)
		} else {
			req.AuthorizedViewName = t.c.fullAuthorizedViewName(t.table, t.authorizedView)
		}
		if mutationsAreRetryable(m.ops) {
			callOptions = retryOptions
		}
		var res *btpb.MutateRowResponse
		err := gaxInvokeWithRecorder(ctx, mt, "MutateRow", func(ctx context.Context, headerMD, trailerMD *metadata.MD, _ gax.CallSettings) error {
			var err error
			res, err = t.c.client.MutateRow(ctx, req, grpc.Header(headerMD), grpc.Trailer(trailerMD))
			return err
		}, callOptions...)
		if err == nil {
			after(res)
		}
		return err
	}

	req := &btpb.CheckAndMutateRowRequest{
		AppProfileId: t.c.appProfile,
		RowKey:       []byte(row),
	}
	if m.cond != nil {
		req.PredicateFilter = m.cond.proto()
	}
	if t.authorizedView == "" {
		req.TableName = t.c.fullTableName(t.table)
	} else {
		req.AuthorizedViewName = t.c.fullAuthorizedViewName(t.table, t.authorizedView)
	}
	if m.mtrue != nil {
		if m.mtrue.cond != nil {
			return errors.New("bigtable: conditional mutations cannot be nested")
		}
		req.TrueMutations = m.mtrue.ops
	}
	if m.mfalse != nil {
		if m.mfalse.cond != nil {
			return errors.New("bigtable: conditional mutations cannot be nested")
		}
		req.FalseMutations = m.mfalse.ops
	}
	var cmRes *btpb.CheckAndMutateRowResponse
	err = gaxInvokeWithRecorder(ctx, mt, "CheckAndMutateRow", func(ctx context.Context, headerMD, trailerMD *metadata.MD, _ gax.CallSettings) error {
		var err error
		cmRes, err = t.c.client.CheckAndMutateRow(ctx, req, grpc.Header(headerMD), grpc.Trailer(trailerMD))
		return err
	})
	if err == nil {
		after(cmRes)
	}
	return err
}

// An ApplyOption is an optional argument to Apply.
type ApplyOption interface {
	after(res proto.Message)
}

type applyAfterFunc func(res proto.Message)

func (a applyAfterFunc) after(res proto.Message) { a(res) }

// GetCondMutationResult returns an ApplyOption that reports whether the conditional
// mutation's condition matched.
func GetCondMutationResult(matched *bool) ApplyOption {
	return applyAfterFunc(func(res proto.Message) {
		if res, ok := res.(*btpb.CheckAndMutateRowResponse); ok {
			*matched = res.PredicateMatched
		}
	})
}

// Mutation represents a set of changes for a single row of a table.
type Mutation struct {
	ops  []*btpb.Mutation
	cond Filter
	// for conditional mutations
	isConditional bool
	mtrue, mfalse *Mutation
}

// NewMutation returns a new mutation.
func NewMutation() *Mutation {
	return new(Mutation)
}

// NewCondMutation returns a conditional mutation.
// The given row filter determines which mutation is applied:
// If the filter matches any cell in the row, mtrue is applied;
// otherwise, mfalse is applied.
// Either given mutation may be nil.
//
// The application of a ReadModifyWrite is atomic; concurrent ReadModifyWrites will
// be executed serially by the server.
func NewCondMutation(cond Filter, mtrue, mfalse *Mutation) *Mutation {
	return &Mutation{cond: cond, mtrue: mtrue, mfalse: mfalse, isConditional: true}
}

// Set sets a value in a specified column, with the given timestamp.
// The timestamp will be truncated to millisecond granularity.
// A timestamp of ServerTime means to use the server timestamp.
func (m *Mutation) Set(family, column string, ts Timestamp, value []byte) {
	m.ops = append(m.ops, &btpb.Mutation{Mutation: &btpb.Mutation_SetCell_{SetCell: &btpb.Mutation_SetCell{
		FamilyName:      family,
		ColumnQualifier: []byte(column),
		TimestampMicros: int64(ts.TruncateToMilliseconds()),
		Value:           value,
	}}})
}

// DeleteCellsInColumn will delete all the cells whose columns are family:column.
func (m *Mutation) DeleteCellsInColumn(family, column string) {
	m.ops = append(m.ops, &btpb.Mutation{Mutation: &btpb.Mutation_DeleteFromColumn_{DeleteFromColumn: &btpb.Mutation_DeleteFromColumn{
		FamilyName:      family,
		ColumnQualifier: []byte(column),
	}}})
}

// DeleteTimestampRange deletes all cells whose columns are family:column
// and whose timestamps are in the half-open interval [start, end).
// If end is zero, it will be interpreted as infinity.
// The timestamps will be truncated to millisecond granularity.
func (m *Mutation) DeleteTimestampRange(family, column string, start, end Timestamp) {
	m.ops = append(m.ops, &btpb.Mutation{Mutation: &btpb.Mutation_DeleteFromColumn_{DeleteFromColumn: &btpb.Mutation_DeleteFromColumn{
		FamilyName:      family,
		ColumnQualifier: []byte(column),
		TimeRange: &btpb.TimestampRange{
			StartTimestampMicros: int64(start.TruncateToMilliseconds()),
			EndTimestampMicros:   int64(end.TruncateToMilliseconds()),
		},
	}}})
}

// DeleteCellsInFamily will delete all the cells whose columns are family:*.
func (m *Mutation) DeleteCellsInFamily(family string) {
	m.ops = append(m.ops, &btpb.Mutation{Mutation: &btpb.Mutation_DeleteFromFamily_{DeleteFromFamily: &btpb.Mutation_DeleteFromFamily{
		FamilyName: family,
	}}})
}

// DeleteRow deletes the entire row.
func (m *Mutation) DeleteRow() {
	m.ops = append(m.ops, &btpb.Mutation{Mutation: &btpb.Mutation_DeleteFromRow_{DeleteFromRow: &btpb.Mutation_DeleteFromRow{}}})
}

// AddIntToCell adds an int64 value to a cell in an aggregate column family. The column family must
// have an input type of Int64 or this mutation will fail.
func (m *Mutation) AddIntToCell(family, column string, ts Timestamp, value int64) {
	m.addToCell(family, column, ts, &btpb.Value{Kind: &btpb.Value_IntValue{IntValue: value}})
}

func (m *Mutation) addToCell(family, column string, ts Timestamp, value *btpb.Value) {
	m.ops = append(m.ops, &btpb.Mutation{Mutation: &btpb.Mutation_AddToCell_{AddToCell: &btpb.Mutation_AddToCell{
		FamilyName:      family,
		ColumnQualifier: &btpb.Value{Kind: &btpb.Value_RawValue{RawValue: []byte(column)}},
		Timestamp:       &btpb.Value{Kind: &btpb.Value_RawTimestampMicros{RawTimestampMicros: int64(ts.TruncateToMilliseconds())}},
		Input:           value,
	}}})
}

// MergeBytesToCell merges a bytes accumulator value to a cell in an aggregate column family.
func (m *Mutation) MergeBytesToCell(family, column string, ts Timestamp, value []byte) {
	m.mergeToCell(family, column, ts, &btpb.Value{Kind: &btpb.Value_RawValue{RawValue: value}})
}

func (m *Mutation) mergeToCell(family, column string, ts Timestamp, value *btpb.Value) {
	m.ops = append(m.ops, &btpb.Mutation{Mutation: &btpb.Mutation_MergeToCell_{MergeToCell: &btpb.Mutation_MergeToCell{
		FamilyName:      family,
		ColumnQualifier: &btpb.Value{Kind: &btpb.Value_RawValue{RawValue: []byte(column)}},
		Timestamp:       &btpb.Value{Kind: &btpb.Value_RawTimestampMicros{RawTimestampMicros: int64(ts.TruncateToMilliseconds())}},
		Input:           value,
	}}})
}

// entryErr is a container that combines an entry with the error that was returned for it.
// Err may be nil if no error was returned for the Entry, or if the Entry has not yet been processed.
type entryErr struct {
	Entry *btpb.MutateRowsRequest_Entry
	Err   error

	// TopLevelErr is the error received either from
	// 1. client.MutateRows
	// 2. stream.Recv
	TopLevelErr error
}

// ApplyBulk applies multiple Mutations.
// Each mutation is individually applied atomically,
// but the set of mutations may be applied in any order.
//
// Two types of failures may occur. If the entire process
// fails, (nil, err) will be returned. If specific mutations
// fail to apply, ([]err, nil) will be returned, and the errors
// will correspond to the relevant rowKeys/muts arguments.
//
// Conditional mutations cannot be applied in bulk and providing one will result in an error.
func (t *Table) ApplyBulk(ctx context.Context, rowKeys []string, muts []*Mutation, opts ...ApplyOption) (errs []error, err error) {
	ctx = mergeOutgoingMetadata(ctx, t.md)
	ctx = trace.StartSpan(ctx, "cloud.google.com/go/bigtable/ApplyBulk")
	defer func() { trace.EndSpan(ctx, err) }()

	if len(rowKeys) != len(muts) {
		return nil, fmt.Errorf("mismatched rowKeys and mutation array lengths: %d, %d", len(rowKeys), len(muts))
	}

	origEntries := make([]*entryErr, len(rowKeys))
	for i, key := range rowKeys {
		mut := muts[i]
		if mut.isConditional {
			return nil, errors.New("conditional mutations cannot be applied in bulk")
		}
		origEntries[i] = &entryErr{Entry: &btpb.MutateRowsRequest_Entry{RowKey: []byte(key), Mutations: mut.ops}}
	}

	var firstGroupErr error
	numFailed := 0
	groups := groupEntries(origEntries, maxMutations)
	for _, group := range groups {
		err := t.applyGroup(ctx, group, opts...)
		if err != nil {
			if firstGroupErr == nil {
				firstGroupErr = err
			}
			numFailed++
		}
	}

	if numFailed == len(groups) {
		return nil, firstGroupErr
	}

	// All the errors are accumulated into an array and returned, interspersed with nils for successful
	// entries. The absence of any errors means we should return nil.
	var foundErr bool
	for _, entry := range origEntries {
		if entry.Err == nil && entry.TopLevelErr != nil {
			// Populate per mutation error if top level error is not nil
			entry.Err = entry.TopLevelErr
		}
		if entry.Err != nil {
			foundErr = true
		}
		errs = append(errs, entry.Err)
	}
	if foundErr {
		return errs, nil
	}
	return nil, nil
}

func (t *Table) applyGroup(ctx context.Context, group []*entryErr, opts ...ApplyOption) (err error) {
	attrMap := make(map[string]interface{})
	mt := t.newBuiltinMetricsTracer(ctx, true)
	defer recordOperationCompletion(mt)

	err = gaxInvokeWithRecorder(ctx, mt, "MutateRows", func(ctx context.Context, headerMD, trailerMD *metadata.MD, _ gax.CallSettings) error {
		attrMap["rowCount"] = len(group)
		trace.TracePrintf(ctx, attrMap, "Row count in ApplyBulk")
		err := t.doApplyBulk(ctx, group, headerMD, trailerMD, opts...)
		if err != nil {
			// We want to retry the entire request with the current group
			return err
		}
		// Get the entries that need to be retried
		group = t.getApplyBulkRetries(group)
		if len(group) > 0 && len(idempotentRetryCodes) > 0 {
			// We have at least one mutation that needs to be retried.
			// Return an arbitrary error that is retryable according to callOptions.
			return status.Errorf(idempotentRetryCodes[0], "Synthetic error: partial failure of ApplyBulk")
		}
		return nil
	}, retryOptions...)

	statusCode, statusErr := convertToGrpcStatusErr(err)
	mt.currOp.setStatus(statusCode.String())
	return statusErr
}

// getApplyBulkRetries returns the entries that need to be retried
func (t *Table) getApplyBulkRetries(entries []*entryErr) []*entryErr {
	var retryEntries []*entryErr
	for _, entry := range entries {
		err := entry.Err
		if err != nil && isIdempotentRetryCode[status.Code(err)] && mutationsAreRetryable(entry.Entry.Mutations) {
			// There was an error and the entry is retryable.
			retryEntries = append(retryEntries, entry)
		}
	}
	return retryEntries
}

// doApplyBulk does the work of a single ApplyBulk invocation
func (t *Table) doApplyBulk(ctx context.Context, entryErrs []*entryErr, headerMD, trailerMD *metadata.MD, opts ...ApplyOption) error {
	after := func(res proto.Message) {
		for _, o := range opts {
			o.after(res)
		}
	}

	var topLevelErr error
	defer func() {
		populateTopLevelError(entryErrs, topLevelErr)
	}()

	entries := make([]*btpb.MutateRowsRequest_Entry, len(entryErrs))
	for i, entryErr := range entryErrs {
		entries[i] = entryErr.Entry
	}
	req := &btpb.MutateRowsRequest{
		AppProfileId: t.c.appProfile,
		Entries:      entries,
	}
	if t.authorizedView == "" {
		req.TableName = t.c.fullTableName(t.table)
	} else {
		req.AuthorizedViewName = t.c.fullAuthorizedViewName(t.table, t.authorizedView)
	}

	stream, err := t.c.client.MutateRows(ctx, req)
	if err != nil {
		_, topLevelErr = convertToGrpcStatusErr(err)
		return err
	}

	// Ignore error since header is only being used to record builtin metrics
	// Failure to record metrics should not fail the operation
	*headerMD, _ = stream.Header()
	for {
		res, err := stream.Recv()
		if err == io.EOF {
			*trailerMD = stream.Trailer()
			break
		}
		if err != nil {
			*trailerMD = stream.Trailer()
			_, topLevelErr = convertToGrpcStatusErr(err)
			return err
		}

		for _, entry := range res.Entries {
			s := entry.Status
			if s.Code == int32(codes.OK) {
				entryErrs[entry.Index].Err = nil
			} else {
				entryErrs[entry.Index].Err = status.Errorf(codes.Code(s.Code), s.Message)
			}
		}
		after(res)
	}
	return nil
}

func populateTopLevelError(entries []*entryErr, topLevelErr error) {
	for _, entry := range entries {
		entry.TopLevelErr = topLevelErr
	}
}

// groupEntries groups entries into groups of a specified size without breaking up
// individual entries.
func groupEntries(entries []*entryErr, maxSize int) [][]*entryErr {
	var (
		res   [][]*entryErr
		start int
		gmuts int
	)
	addGroup := func(end int) {
		if end-start > 0 {
			res = append(res, entries[start:end])
			start = end
			gmuts = 0
		}
	}
	for i, e := range entries {
		emuts := len(e.Entry.Mutations)
		if gmuts+emuts > maxSize {
			addGroup(i)
		}
		gmuts += emuts
	}
	addGroup(len(entries))
	return res
}

// Timestamp is in units of microseconds since 1 January 1970.
type Timestamp int64

// ServerTime is a specific Timestamp that may be passed to (*Mutation).Set.
// It indicates that the server's timestamp should be used.
const ServerTime Timestamp = -1

// Time converts a time.Time into a Timestamp.
func Time(t time.Time) Timestamp { return Timestamp(t.UnixNano() / 1e3) }

// Now returns the Timestamp representation of the current time on the client.
func Now() Timestamp { return Time(time.Now()) }

// Time converts a Timestamp into a time.Time.
func (ts Timestamp) Time() time.Time { return time.Unix(int64(ts)/1e6, int64(ts)%1e6*1e3) }

// TruncateToMilliseconds truncates a Timestamp to millisecond granularity,
// which is currently the only granularity supported.
func (ts Timestamp) TruncateToMilliseconds() Timestamp {
	if ts == ServerTime {
		return ts
	}
	return ts - ts%1000
}

// ApplyReadModifyWrite applies a ReadModifyWrite to a specific row.
// It returns the newly written cells.
func (t *Table) ApplyReadModifyWrite(ctx context.Context, row string, m *ReadModifyWrite) (Row, error) {
	ctx = mergeOutgoingMetadata(ctx, t.md)

	mt := t.newBuiltinMetricsTracer(ctx, false)
	defer recordOperationCompletion(mt)

	updatedRow, err := t.applyReadModifyWrite(ctx, mt, row, m)
	statusCode, statusErr := convertToGrpcStatusErr(err)
	mt.currOp.setStatus(statusCode.String())
	return updatedRow, statusErr
}

func (t *Table) applyReadModifyWrite(ctx context.Context, mt *builtinMetricsTracer, row string, m *ReadModifyWrite) (Row, error) {
	req := &btpb.ReadModifyWriteRowRequest{
		AppProfileId: t.c.appProfile,
		RowKey:       []byte(row),
		Rules:        m.ops,
	}
	if t.authorizedView == "" {
		req.TableName = t.c.fullTableName(t.table)
	} else {
		req.AuthorizedViewName = t.c.fullAuthorizedViewName(t.table, t.authorizedView)
	}

	var r Row
	err := gaxInvokeWithRecorder(ctx, mt, "ReadModifyWriteRow", func(ctx context.Context, headerMD, trailerMD *metadata.MD, _ gax.CallSettings) error {
		res, err := t.c.client.ReadModifyWriteRow(ctx, req, grpc.Header(headerMD), grpc.Trailer(trailerMD))
		if err != nil {
			return err
		}
		if res.Row == nil {
			return errors.New("unable to apply ReadModifyWrite: res.Row=nil")
		}
		r = make(Row)
		for _, fam := range res.Row.Families { // res is *btpb.Row, fam is *btpb.Family
			decodeFamilyProto(r, row, fam)
		}
		return nil
	})
	return r, err
}

// ReadModifyWrite represents a set of operations on a single row of a table.
// It is like Mutation but for non-idempotent changes.
// When applied, these operations operate on the latest values of the row's cells,
// and result in a new value being written to the relevant cell with a timestamp
// that is max(existing timestamp, current server time).
//
// The application of a ReadModifyWrite is atomic; concurrent ReadModifyWrites will
// be executed serially by the server.
type ReadModifyWrite struct {
	ops []*btpb.ReadModifyWriteRule
}

// NewReadModifyWrite returns a new ReadModifyWrite.
func NewReadModifyWrite() *ReadModifyWrite { return new(ReadModifyWrite) }

// AppendValue appends a value to a specific cell's value.
// If the cell is unset, it will be treated as an empty value.
func (m *ReadModifyWrite) AppendValue(family, column string, v []byte) {
	m.ops = append(m.ops, &btpb.ReadModifyWriteRule{
		FamilyName:      family,
		ColumnQualifier: []byte(column),
		Rule:            &btpb.ReadModifyWriteRule_AppendValue{AppendValue: v},
	})
}

// Increment interprets the value in a specific cell as a 64-bit big-endian signed integer,
// and adds a value to it. If the cell is unset, it will be treated as zero.
// If the cell is set and is not an 8-byte value, the entire ApplyReadModifyWrite
// operation will fail.
func (m *ReadModifyWrite) Increment(family, column string, delta int64) {
	m.ops = append(m.ops, &btpb.ReadModifyWriteRule{
		FamilyName:      family,
		ColumnQualifier: []byte(column),
		Rule:            &btpb.ReadModifyWriteRule_IncrementAmount{IncrementAmount: delta},
	})
}

// SampleRowKeys returns a sample of row keys in the table. The returned row keys will delimit contiguous sections of
// the table of approximately equal size, which can be used to break up the data for distributed tasks like mapreduces.
func (t *Table) SampleRowKeys(ctx context.Context) ([]string, error) {
	ctx = mergeOutgoingMetadata(ctx, t.md)

	mt := t.newBuiltinMetricsTracer(ctx, true)
	defer recordOperationCompletion(mt)

	rowKeys, err := t.sampleRowKeys(ctx, mt)
	statusCode, statusErr := convertToGrpcStatusErr(err)
	mt.currOp.setStatus(statusCode.String())
	return rowKeys, statusErr
}

func (t *Table) sampleRowKeys(ctx context.Context, mt *builtinMetricsTracer) ([]string, error) {
	var sampledRowKeys []string
	err := gaxInvokeWithRecorder(ctx, mt, "SampleRowKeys", func(ctx context.Context, headerMD, trailerMD *metadata.MD, _ gax.CallSettings) error {
		sampledRowKeys = nil
		req := &btpb.SampleRowKeysRequest{
			AppProfileId: t.c.appProfile,
		}
		if t.authorizedView == "" {
			req.TableName = t.c.fullTableName(t.table)
		} else {
			req.AuthorizedViewName = t.c.fullAuthorizedViewName(t.table, t.authorizedView)
		}
		ctx, cancel := context.WithCancel(ctx) // for aborting the stream
		defer cancel()

		stream, err := t.c.client.SampleRowKeys(ctx, req)
		if err != nil {
			return err
		}

		// Ignore error since header is only being used to record builtin metrics
		// Failure to record metrics should not fail the operation
		*headerMD, _ = stream.Header()
		for {
			res, err := stream.Recv()
			if err == io.EOF {
				*trailerMD = stream.Trailer()
				break
			}
			if err != nil {
				*trailerMD = stream.Trailer()
				return err
			}

			key := string(res.RowKey)
			if key == "" {
				continue
			}

			sampledRowKeys = append(sampledRowKeys, key)
		}
		return nil
	}, retryOptions...)

	return sampledRowKeys, err
}

func (t *Table) newBuiltinMetricsTracer(ctx context.Context, isStreaming bool) *builtinMetricsTracer {
	mt := t.c.metricsTracerFactory.createBuiltinMetricsTracer(ctx, t.table, isStreaming)
	return &mt
}

// recordOperationCompletion records as many operation specific metrics as it can
// Ignores error seen while creating metric attributes since metric can still
// be recorded with rest of the attributes
func recordOperationCompletion(mt *builtinMetricsTracer) {
	if !mt.builtInEnabled {
		return
	}

	// Calculate elapsed time
	elapsedTimeMs := convertToMs(time.Since(mt.currOp.startTime))

	// Record operation_latencies
	opLatAttrs, _ := mt.toOtelMetricAttrs(metricNameOperationLatencies)
	mt.instrumentOperationLatencies.Record(mt.ctx, elapsedTimeMs, metric.WithAttributes(opLatAttrs...))

	// Record retry_count
	retryCntAttrs, _ := mt.toOtelMetricAttrs(metricNameRetryCount)
	if mt.currOp.attemptCount > 1 {
		// Only record when retry count is greater than 0 so the retry
		// graph will be less confusing
		mt.instrumentRetryCount.Add(mt.ctx, mt.currOp.attemptCount-1, metric.WithAttributes(retryCntAttrs...))
	}
}

// gaxInvokeWithRecorder:
// - wraps 'f' in a new function 'callWrapper' that:
//   - updates tracer state and records built in attempt specific metrics
//   - does not return errors seen while recording the metrics
//
// - then, calls gax.Invoke with 'callWrapper' as an argument
func gaxInvokeWithRecorder(ctx context.Context, mt *builtinMetricsTracer, method string,
	f func(ctx context.Context, headerMD, trailerMD *metadata.MD, _ gax.CallSettings) error, opts ...gax.CallOption) error {
	attemptHeaderMD := metadata.New(nil)
	attempTrailerMD := metadata.New(nil)
	mt.setMethod(method)

	var callWrapper func(context.Context, gax.CallSettings) error
	if !mt.builtInEnabled {
		callWrapper = func(ctx context.Context, callSettings gax.CallSettings) error {
			// f makes calls to CBT service
			return f(ctx, &attemptHeaderMD, &attempTrailerMD, callSettings)
		}
	} else {
		callWrapper = func(ctx context.Context, callSettings gax.CallSettings) error {
			// Increment number of attempts
			mt.currOp.incrementAttemptCount()

			mt.currOp.currAttempt = attemptTracer{}

			// record start time
			mt.currOp.currAttempt.setStartTime(time.Now())

			// f makes calls to CBT service
			err := f(ctx, &attemptHeaderMD, &attempTrailerMD, callSettings)

			// Set attempt status
			statusCode, _ := convertToGrpcStatusErr(err)
			mt.currOp.currAttempt.setStatus(statusCode.String())

			// Get location attributes from metadata and set it in tracer
			// Ignore get location error since the metric can still be recorded with rest of the attributes
			clusterID, zoneID, _ := extractLocation(attemptHeaderMD, attempTrailerMD)
			mt.currOp.currAttempt.setClusterID(clusterID)
			mt.currOp.currAttempt.setZoneID(zoneID)

			// Set server latency in tracer
			serverLatency, serverLatencyErr := extractServerLatency(attemptHeaderMD, attempTrailerMD)
			mt.currOp.currAttempt.setServerLatencyErr(serverLatencyErr)
			mt.currOp.currAttempt.setServerLatency(serverLatency)

			// Record attempt specific metrics
			recordAttemptCompletion(mt)
			return err
		}
	}
	return gax.Invoke(ctx, callWrapper, opts...)
}

// recordAttemptCompletion records as many attempt specific metrics as it can
// Ignore errors seen while creating metric attributes since metric can still
// be recorded with rest of the attributes
func recordAttemptCompletion(mt *builtinMetricsTracer) {
	if !mt.builtInEnabled {
		return
	}

	// Calculate elapsed time
	elapsedTime := convertToMs(time.Since(mt.currOp.currAttempt.startTime))

	// Record attempt_latencies
	attemptLatAttrs, _ := mt.toOtelMetricAttrs(metricNameAttemptLatencies)
	mt.instrumentAttemptLatencies.Record(mt.ctx, elapsedTime, metric.WithAttributes(attemptLatAttrs...))

	// Record server_latencies
	serverLatAttrs, _ := mt.toOtelMetricAttrs(metricNameServerLatencies)
	if mt.currOp.currAttempt.serverLatencyErr == nil {
		mt.instrumentServerLatencies.Record(mt.ctx, mt.currOp.currAttempt.serverLatency, metric.WithAttributes(serverLatAttrs...))
	}
}
