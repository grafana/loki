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

package bigtable

import (
	"context"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"cloud.google.com/go/bigtable/internal/session"
	btransport "cloud.google.com/go/bigtable/internal/transport"
)

// TableShim implements TableAPI by routing between a classic gRPC
// data-plane and a proto-native session data-plane
// (session.TableAPI). Traffic direction is decided per-call by the
// Diverter's SessionLoad ratio. TableShim owns the proto ↔
// bigtable.Row conversion so the session package can stay proto-native.
//
// Methods with no session equivalent (SampleRowKeys, ApplyBulk,
// ApplyReadModifyWrite) always delegate to classic. ReadRows delegates
// to classic for the general multi-row case; the single-key shape
// (see singleRowKey) is served by the session ReadRow vRPC since it is
// semantically identical to Table.ReadRow. Conditional mutations always
// route to classic — session vRPC does not support the CheckAndMutateRow
// shape today.
//
// ReadRow, single-key ReadRows, and Apply pass their session call's
// error to session.UnimplementedBreaker.ShouldFallback: a
// codes.Unimplemented response falls back to classic, and after enough
// consecutive Unimplementeds the breaker's sticky trip gates
// useSession() so future calls skip session entirely.
type TableShim struct {
	classic  TableAPI
	session  session.TableAPI
	diverter *btransport.Diverter
	breaker  *session.UnimplementedBreaker
}

// NewTableShim wraps a classic TableAPI + a proto-native session API
// with a Diverter-gated router. Any of session or diverter may be nil,
// in which case the shim behaves like classic-only.
func NewTableShim(classic TableAPI, sessionAPI session.TableAPI, diverter *btransport.Diverter) TableAPI {
	return &TableShim{
		classic:  classic,
		session:  sessionAPI,
		diverter: diverter,
		breaker:  session.NewUnimplementedBreaker(session.DefaultUnimplementedThreshold),
	}
}

// ReadRow implements TableAPI. Routes through the session path when
// the diverter allows and the session API is available; otherwise
// delegates to classic.
func (t *TableShim) ReadRow(ctx context.Context, row string, opts ...ReadOption) (Row, error) {
	if !t.useSession() {
		return t.classic.ReadRow(ctx, row, opts...)
	}
	r, err := t.readRowSession(ctx, row, opts...)
	if t.breaker.ShouldFallback(err) {
		return t.classic.ReadRow(ctx, row, opts...)
	}
	return r, err
}

// readRowSession runs the single-row read on the session data plane.
// Shared by ReadRow and single-key ReadRows so both build the same
// request. It does not fall back on its own: the two callers fall back
// to different classic methods, so each asks the breaker itself.
func (t *TableShim) readRowSession(ctx context.Context, row string, opts ...ReadOption) (Row, error) {
	// Parse opts using the classic settings shape so filter + full-read
	// stats callback plumbing stays in one place.
	tmpReq := &btpb.ReadRowsRequest{}
	settings := makeReadSettings(tmpReq, 0)
	for _, opt := range opts {
		opt.set(&settings)
	}
	resp, err := t.session.ReadRow(ctx, &btpb.SessionReadRowRequest{
		Key:    []byte(row),
		Filter: tmpReq.Filter,
	})
	if err != nil {
		return nil, err
	}
	if resp.GetStats() != nil && settings.fullReadStatsFunc != nil {
		stats := makeFullReadStats(resp.GetStats())
		settings.fullReadStatsFunc(&stats)
	}
	return protoRowToRow(resp.GetRow()), nil
}

// Apply implements TableAPI. Non-conditional mutations may route
// through the session path (subject to the diverter); conditional
// mutations always go to classic — CheckAndMutateRow has no session
// equivalent. Nil mutations also route to classic so the classic
// client's nil-handling error surfaces exactly as it always has,
// rather than panicking here on m.ops.
func (t *TableShim) Apply(ctx context.Context, row string, m *Mutation, opts ...ApplyOption) error {
	if m == nil || m.isConditional {
		return t.classic.Apply(ctx, row, m, opts...)
	}
	if !t.useSession() {
		return t.classic.Apply(ctx, row, m, opts...)
	}
	req := &btpb.SessionMutateRowRequest{
		Key:       []byte(row),
		Mutations: m.ops,
	}
	_, err := t.session.MutateRow(ctx, req)
	if t.breaker.ShouldFallback(err) {
		return t.classic.Apply(ctx, row, m, opts...)
	}
	return err
}

// ReadRows sends a single-key read through the session data plane and
// everything else to classic. See singleRowKey for which shapes count.
//
// Every classic branch here calls classic.ReadRows, never
// classic.ReadRow. The two are not interchangeable: ReadRow buffers the
// stream into one return value, so it loses rows that already arrived
// when the stream later fails, and keeps only the last row if the
// server sends several. ReadRows hands each row to f as it arrives and
// returns the error afterwards, which is what callers expect. Routing
// the classic side through ReadRow is what broke the conformance suite.
//
// LimitRows(<1) is left to classic so ownership of its no-dial
// short-circuit (LimitRows(0) returns nil, negative → errNegativeRowLimit)
// stays in the ReadRows body it was designed for.
func (t *TableShim) ReadRows(ctx context.Context, arg RowSet, f func(Row) bool, opts ...ReadOption) error {
	key, ok := singleRowKey(arg)
	if !ok || !t.useSession() || deferToClassicForLimit(opts) {
		return t.classic.ReadRows(ctx, arg, f, opts...)
	}
	row, err := t.readRowSession(ctx, key, opts...)
	if t.breaker.ShouldFallback(err) {
		return t.classic.ReadRows(ctx, arg, f, opts...)
	}
	if err != nil {
		return err
	}
	if row != nil {
		// Row is nil for row-not-found (protoRowToRow collapses
		// empty responses to nil) — mirror classic ReadRows, which
		// does not invoke f on absent keys. f's bool return is
		// meaningful for multi-row streams; with at most one row
		// there is no next iteration to gate, so we discard it.
		f(row)
	}
	return nil
}

// singleRowKey reports whether arg addresses exactly one row and returns
// that key. Three shapes qualify:
//
//   - RowList of length 1 (what SingleRow(k) returns).
//   - A closed-closed RowRange [k,k] (NewClosedRange(k,k)) where start
//     equals end; the range covers exactly one key. Other bound shapes
//     of a same-start/end pair — [k,k), (k,k], (k,k) — are all empty and
//     must stay classic so their empty-result semantics are unchanged.
//   - A RowRangeList of length 1 whose sole element is a single-row
//     RowRange as above. RowRangeList of length 0 or >1 stays classic.
//
// A RowRange or RowRangeList element with an unbounded side is never a
// single-row case (Unbounded ranges span more than one key).
func singleRowKey(arg RowSet) (string, bool) {
	switch v := arg.(type) {
	case RowList:
		if len(v) == 1 {
			return v[0], true
		}
	case RowRange:
		return singleRowRangeKey(v)
	case RowRangeList:
		if len(v) == 1 {
			return singleRowRangeKey(v[0])
		}
	}
	return "", false
}

// singleRowRangeKey reports whether r covers exactly one key ([k,k])
// and returns that key. Only rangeClosed on BOTH sides plus start==end
// qualifies; any open bound or unbounded side, or a mismatched
// start/end, means the range covers zero or more-than-one keys.
func singleRowRangeKey(r RowRange) (string, bool) {
	if r.startBound == rangeClosed && r.endBound == rangeClosed && r.start == r.end {
		return r.start, true
	}
	return "", false
}

// deferToClassicForLimit reports whether opts contain a LimitRows
// whose limit is < 1 — the shapes the single-row session dispatch
// MUST leave to classic. Only the last LimitRows in opts wins
// (matching classic ReadRows' loop behavior), so scan back-to-front
// and stop at the first hit.
//
// The guard matters: without it a LimitRows(0) that classic would
// short-circuit into "no dial, return nil" would instead fire a
// session ReadRow and deliver a row through the callback, and a
// LimitRows(<0) that classic would reject with errNegativeRowLimit
// would silently succeed. Leaving both cases to classic preserves
// their existing user-visible semantics on the diverted path.
func deferToClassicForLimit(opts []ReadOption) bool {
	for i := len(opts) - 1; i >= 0; i-- {
		if lr, ok := opts[i].(limitRows); ok {
			return lr.limit < 1
		}
	}
	return false
}

// SampleRowKeys delegates to classic.
func (t *TableShim) SampleRowKeys(ctx context.Context) ([]string, error) {
	return t.classic.SampleRowKeys(ctx)
}

// ApplyBulk delegates to classic.
func (t *TableShim) ApplyBulk(ctx context.Context, rowKeys []string, muts []*Mutation, opts ...ApplyOption) ([]error, error) {
	return t.classic.ApplyBulk(ctx, rowKeys, muts, opts...)
}

// ApplyReadModifyWrite delegates to classic.
func (t *TableShim) ApplyReadModifyWrite(ctx context.Context, row string, m *ReadModifyWrite) (Row, error) {
	return t.classic.ApplyReadModifyWrite(ctx, row, m)
}

// useSession returns true when session + diverter are configured, the
// Unimplemented breaker hasn't tripped, and the diverter
// says this call should go over session.
func (t *TableShim) useSession() bool {
	return t.session != nil &&
		t.diverter != nil &&
		!t.breaker.Bypass() &&
		t.diverter.UseSession()
}

// protoRowToRow converts a proto Row (the wire shape returned by a
// proto-native session backend's SessionReadRowResponse) into the
// classic bigtable.Row map shape that TableAPI.ReadRow callers expect.
//
// Contract: preserves wire order for cells within a column and columns
// within a family; a row with no cells (or nil input) returns nil to
// match classic Table.ReadRow's row-not-found signal — callers check
// `row == nil`. Same-named families that appear twice in Families are
// appended, not deduped, matching the server's wire format. Test suite
// (TestProtoRowToRow) pins every branch of this contract.
func protoRowToRow(pr *btpb.Row) Row {
	if pr == nil {
		return nil
	}
	rowMap := make(Row)
	rowKey := string(pr.Key)
	for _, fam := range pr.Families {
		familyName := fam.Name
		for _, col := range fam.Columns {
			columnName := familyName + ":" + string(col.Qualifier)
			var items []ReadItem
			for _, cell := range col.Cells {
				items = append(items, ReadItem{
					Row:       rowKey,
					Column:    columnName,
					Timestamp: Timestamp(cell.TimestampMicros),
					Value:     cell.Value,
					Labels:    cell.Labels,
				})
			}
			if len(items) > 0 {
				rowMap[familyName] = append(rowMap[familyName], items...)
			}
		}
	}
	// Match classic Table.ReadRow: a row with no cells is a not-found
	// signal; callers check `row == nil`. Server should send pr=nil for
	// not-found, but defensively collapse the empty-but-non-nil case too.
	if len(rowMap) == 0 {
		return nil
	}
	return rowMap
}
