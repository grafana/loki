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
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	btopt "cloud.google.com/go/bigtable/internal/option"
	"cloud.google.com/go/internal/trace"
	"github.com/golang/protobuf/proto"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/api/option"
	gtransport "google.golang.org/api/transport/grpc"
	btpb "google.golang.org/genproto/googleapis/bigtable/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const prodAddr = "bigtable.googleapis.com:443"

// Client is a client for reading and writing data to tables in an instance.
//
// A Client is safe to use concurrently, except for its Close method.
type Client struct {
	conn              *grpc.ClientConn
	client            btpb.BigtableClient
	project, instance string
	appProfile        string
}

// ClientConfig has configurations for the client.
type ClientConfig struct {
	// The id of the app profile to associate with all data operations sent from this client.
	// If unspecified, the default app profile for the instance will be used.
	AppProfile string
}

// NewClient creates a new Client for a given project and instance.
// The default ClientConfig will be used.
func NewClient(ctx context.Context, project, instance string, opts ...option.ClientOption) (*Client, error) {
	return NewClientWithConfig(ctx, project, instance, ClientConfig{}, opts...)
}

// NewClientWithConfig creates a new client with the given config.
func NewClientWithConfig(ctx context.Context, project, instance string, config ClientConfig, opts ...option.ClientOption) (*Client, error) {
	o, err := btopt.DefaultClientOptions(prodAddr, Scope, clientUserAgent)
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
		// TODO(grpc/grpc-go#1388) using connection pool without WithBlock
		// can cause RPCs to fail randomly. We can delete this after the issue is fixed.
		option.WithGRPCDialOption(grpc.WithBlock()))
	o = append(o, opts...)
	conn, err := gtransport.Dial(ctx, o...)
	if err != nil {
		return nil, fmt.Errorf("dialing: %v", err)
	}

	return &Client{
		conn:       conn,
		client:     btpb.NewBigtableClient(conn),
		project:    project,
		instance:   instance,
		appProfile: config.AppProfile,
	}, nil
}

// Close closes the Client.
func (c *Client) Close() error {
	return c.conn.Close()
}

var (
	idempotentRetryCodes  = []codes.Code{codes.DeadlineExceeded, codes.Unavailable, codes.Aborted}
	isIdempotentRetryCode = make(map[codes.Code]bool)
	retryOptions          = []gax.CallOption{
		gax.WithRetry(func() gax.Retryer {
			return gax.OnCodes(idempotentRetryCodes, gax.Backoff{
				Initial:    100 * time.Millisecond,
				Max:        2 * time.Second,
				Multiplier: 1.2,
			})
		}),
	}
)

func init() {
	for _, code := range idempotentRetryCodes {
		isIdempotentRetryCode[code] = true
	}
}

func (c *Client) fullTableName(table string) string {
	return fmt.Sprintf("projects/%s/instances/%s/tables/%s", c.project, c.instance, table)
}

// mergeOutgoingMetadata returns a context populated by the existing outgoing
// metadata merged with the provided mds.
func mergeOutgoingMetadata(ctx context.Context, mds ...metadata.MD) context.Context {
	ctxMD, _ := metadata.FromOutgoingContext(ctx)
	// The ordering matters, hence why ctxMD comes first.
	allMDs := append([]metadata.MD{ctxMD}, mds...)
	return metadata.NewOutgoingContext(ctx, metadata.Join(allMDs...))
}

// A Table refers to a table.
//
// A Table is safe to use concurrently.
type Table struct {
	c     *Client
	table string

	// Metadata to be sent with each request.
	md metadata.MD
}

// Open opens a table.
func (c *Client) Open(table string) *Table {
	return &Table{
		c:     c,
		table: table,
		md:    metadata.Pairs(resourcePrefixHeader, c.fullTableName(table)),
	}
}

// TODO(dsymonds): Read method that returns a sequence of ReadItems.

// ReadRows reads rows from a table. f is called for each row.
// If f returns false, the stream is shut down and ReadRows returns.
// f owns its argument, and f is called serially in order by row key.
//
// By default, the yielded rows will contain all values in all cells.
// Use RowFilter to limit the cells returned.
func (t *Table) ReadRows(ctx context.Context, arg RowSet, f func(Row) bool, opts ...ReadOption) (err error) {
	ctx = mergeOutgoingMetadata(ctx, t.md)
	ctx = trace.StartSpan(ctx, "cloud.google.com/go/bigtable.ReadRows")
	defer func() { trace.EndSpan(ctx, err) }()

	var prevRowKey string
	attrMap := make(map[string]interface{})
	err = gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		if !arg.valid() {
			// Empty row set, no need to make an API call.
			// NOTE: we must return early if arg == RowList{} because reading
			// an empty RowList from bigtable returns all rows from that table.
			return nil
		}
		req := &btpb.ReadRowsRequest{
			TableName:    t.c.fullTableName(t.table),
			AppProfileId: t.c.appProfile,
			Rows:         arg.proto(),
		}
		for _, opt := range opts {
			opt.set(req)
		}
		ctx, cancel := context.WithCancel(ctx) // for aborting the stream
		defer cancel()

		startTime := time.Now()
		stream, err := t.c.client.ReadRows(ctx, req)
		if err != nil {
			return err
		}
		cr := newChunkReader()
		for {
			res, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				// Reset arg for next Invoke call.
				arg = arg.retainRowsAfter(prevRowKey)
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
				if !f(row) {
					// Cancel and drain stream.
					cancel()
					for {
						if _, err := stream.Recv(); err != nil {
							// The stream has ended. We don't return an error
							// because the caller has intentionally interrupted the scan.
							return nil
						}
					}
				}
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
// A missing row will return a zero-length map and a nil error.
func (t *Table) ReadRow(ctx context.Context, row string, opts ...ReadOption) (Row, error) {
	var r Row
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

func (r RowList) valid() bool {
	return len(r) > 0
}

// A RowRange is a half-open interval [Start, Limit) encompassing
// all the rows with keys at least as large as Start, and less than Limit.
// (Bigtable string comparison is the same as Go's.)
// A RowRange can be unbounded, encompassing all keys at least as large as Start.
type RowRange struct {
	start string
	limit string
}

// NewRange returns the new RowRange [begin, end).
func NewRange(begin, end string) RowRange {
	return RowRange{
		start: begin,
		limit: end,
	}
}

// Unbounded tests whether a RowRange is unbounded.
func (r RowRange) Unbounded() bool {
	return r.limit == ""
}

// Contains says whether the RowRange contains the key.
func (r RowRange) Contains(row string) bool {
	return r.start <= row && (r.limit == "" || r.limit > row)
}

// String provides a printable description of a RowRange.
func (r RowRange) String() string {
	a := strconv.Quote(r.start)
	if r.Unbounded() {
		return fmt.Sprintf("[%s,∞)", a)
	}
	return fmt.Sprintf("[%s,%q)", a, r.limit)
}

func (r RowRange) proto() *btpb.RowSet {
	rr := &btpb.RowRange{
		StartKey: &btpb.RowRange_StartKeyClosed{StartKeyClosed: []byte(r.start)},
	}
	if !r.Unbounded() {
		rr.EndKey = &btpb.RowRange_EndKeyOpen{EndKeyOpen: []byte(r.limit)}
	}
	return &btpb.RowSet{RowRanges: []*btpb.RowRange{rr}}
}

func (r RowRange) retainRowsAfter(lastRowKey string) RowSet {
	if lastRowKey == "" || lastRowKey < r.start {
		return r
	}
	// Set the beginning of the range to the row after the last scanned.
	start := lastRowKey + "\x00"
	if r.Unbounded() {
		return InfiniteRange(start)
	}
	return NewRange(start, r.limit)
}

func (r RowRange) valid() bool {
	return r.Unbounded() || r.start < r.limit
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

// PrefixRange returns a RowRange consisting of all keys starting with the prefix.
func PrefixRange(prefix string) RowRange {
	return RowRange{
		start: prefix,
		limit: prefixSuccessor(prefix),
	}
}

// InfiniteRange returns the RowRange consisting of all keys at least as
// large as start.
func InfiniteRange(start string) RowRange {
	return RowRange{
		start: start,
		limit: "",
	}
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

// A ReadOption is an optional argument to ReadRows.
type ReadOption interface {
	set(req *btpb.ReadRowsRequest)
}

// RowFilter returns a ReadOption that applies f to the contents of read rows.
//
// If multiple RowFilters are provided, only the last is used. To combine filters,
// use ChainFilters or InterleaveFilters instead.
func RowFilter(f Filter) ReadOption { return rowFilter{f} }

type rowFilter struct{ f Filter }

func (rf rowFilter) set(req *btpb.ReadRowsRequest) { req.Filter = rf.f.proto() }

// LimitRows returns a ReadOption that will limit the number of rows to be read.
func LimitRows(limit int64) ReadOption { return limitRows{limit} }

type limitRows struct{ limit int64 }

func (lr limitRows) set(req *btpb.ReadRowsRequest) { req.RowsLimit = lr.limit }

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

const maxMutations = 100000

// Apply mutates a row atomically. A mutation must contain at least one
// operation and at most 100000 operations.
func (t *Table) Apply(ctx context.Context, row string, m *Mutation, opts ...ApplyOption) (err error) {
	ctx = mergeOutgoingMetadata(ctx, t.md)
	ctx = trace.StartSpan(ctx, "cloud.google.com/go/bigtable/Apply")
	defer func() { trace.EndSpan(ctx, err) }()

	after := func(res proto.Message) {
		for _, o := range opts {
			o.after(res)
		}
	}

	var callOptions []gax.CallOption
	if m.cond == nil {
		req := &btpb.MutateRowRequest{
			TableName:    t.c.fullTableName(t.table),
			AppProfileId: t.c.appProfile,
			RowKey:       []byte(row),
			Mutations:    m.ops,
		}
		if mutationsAreRetryable(m.ops) {
			callOptions = retryOptions
		}
		var res *btpb.MutateRowResponse
		err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
			var err error
			res, err = t.c.client.MutateRow(ctx, req)
			return err
		}, callOptions...)
		if err == nil {
			after(res)
		}
		return err
	}

	req := &btpb.CheckAndMutateRowRequest{
		TableName:       t.c.fullTableName(t.table),
		AppProfileId:    t.c.appProfile,
		RowKey:          []byte(row),
		PredicateFilter: m.cond.proto(),
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
	if mutationsAreRetryable(req.TrueMutations) && mutationsAreRetryable(req.FalseMutations) {
		callOptions = retryOptions
	}
	var cmRes *btpb.CheckAndMutateRowResponse
	err = gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		var err error
		cmRes, err = t.c.client.CheckAndMutateRow(ctx, req)
		return err
	}, callOptions...)
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
	ops []*btpb.Mutation

	// for conditional mutations
	cond          Filter
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
	return &Mutation{cond: cond, mtrue: mtrue, mfalse: mfalse}
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

// entryErr is a container that combines an entry with the error that was returned for it.
// Err may be nil if no error was returned for the Entry, or if the Entry has not yet been processed.
type entryErr struct {
	Entry *btpb.MutateRowsRequest_Entry
	Err   error
}

// ApplyBulk applies multiple Mutations, up to a maximum of 100,000.
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
		if mut.cond != nil {
			return nil, errors.New("conditional mutations cannot be applied in bulk")
		}
		origEntries[i] = &entryErr{Entry: &btpb.MutateRowsRequest_Entry{RowKey: []byte(key), Mutations: mut.ops}}
	}

	for _, group := range groupEntries(origEntries, maxMutations) {
		attrMap := make(map[string]interface{})
		err = gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
			attrMap["rowCount"] = len(group)
			trace.TracePrintf(ctx, attrMap, "Row count in ApplyBulk")
			err := t.doApplyBulk(ctx, group, opts...)
			if err != nil {
				// We want to retry the entire request with the current group
				return err
			}
			group = t.getApplyBulkRetries(group)
			if len(group) > 0 && len(idempotentRetryCodes) > 0 {
				// We have at least one mutation that needs to be retried.
				// Return an arbitrary error that is retryable according to callOptions.
				return status.Errorf(idempotentRetryCodes[0], "Synthetic error: partial failure of ApplyBulk")
			}
			return nil
		}, retryOptions...)
		if err != nil {
			return nil, err
		}
	}

	// All the errors are accumulated into an array and returned, interspersed with nils for successful
	// entries. The absence of any errors means we should return nil.
	var foundErr bool
	for _, entry := range origEntries {
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
func (t *Table) doApplyBulk(ctx context.Context, entryErrs []*entryErr, opts ...ApplyOption) error {
	after := func(res proto.Message) {
		for _, o := range opts {
			o.after(res)
		}
	}

	entries := make([]*btpb.MutateRowsRequest_Entry, len(entryErrs))
	for i, entryErr := range entryErrs {
		entries[i] = entryErr.Entry
	}
	req := &btpb.MutateRowsRequest{
		TableName:    t.c.fullTableName(t.table),
		AppProfileId: t.c.appProfile,
		Entries:      entries,
	}
	stream, err := t.c.client.MutateRows(ctx, req)
	if err != nil {
		return err
	}
	for {
		res, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		for i, entry := range res.Entries {
			s := entry.Status
			if s.Code == int32(codes.OK) {
				entryErrs[i].Err = nil
			} else {
				entryErrs[i].Err = status.Errorf(codes.Code(s.Code), s.Message)
			}
		}
		after(res)
	}
	return nil
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
func (ts Timestamp) Time() time.Time { return time.Unix(0, int64(ts)*1e3) }

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
	req := &btpb.ReadModifyWriteRowRequest{
		TableName:    t.c.fullTableName(t.table),
		AppProfileId: t.c.appProfile,
		RowKey:       []byte(row),
		Rules:        m.ops,
	}
	res, err := t.c.client.ReadModifyWriteRow(ctx, req)
	if err != nil {
		return nil, err
	}
	if res.Row == nil {
		return nil, errors.New("unable to apply ReadModifyWrite: res.Row=nil")
	}
	r := make(Row)
	for _, fam := range res.Row.Families { // res is *btpb.Row, fam is *btpb.Family
		decodeFamilyProto(r, row, fam)
	}
	return r, nil
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
	var sampledRowKeys []string
	err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		sampledRowKeys = nil
		req := &btpb.SampleRowKeysRequest{
			TableName:    t.c.fullTableName(t.table),
			AppProfileId: t.c.appProfile,
		}
		ctx, cancel := context.WithCancel(ctx) // for aborting the stream
		defer cancel()

		stream, err := t.c.client.SampleRowKeys(ctx, req)
		if err != nil {
			return err
		}
		for {
			res, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
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
