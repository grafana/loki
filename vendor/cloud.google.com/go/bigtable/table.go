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

	metrics "cloud.google.com/go/bigtable/internal/metrics"
	"google.golang.org/grpc/metadata"
)

// TableAPI interface allows existing data APIs to be applied to either an authorized view, a materialized view or a table.
// A materialized view is a read-only entity.
type TableAPI interface {
	ReadRows(ctx context.Context, arg RowSet, f func(Row) bool, opts ...ReadOption) error
	ReadRow(ctx context.Context, row string, opts ...ReadOption) (Row, error)
	SampleRowKeys(ctx context.Context) ([]string, error)
	Apply(ctx context.Context, row string, m *Mutation, opts ...ApplyOption) error
	ApplyBulk(ctx context.Context, rowKeys []string, muts []*Mutation, opts ...ApplyOption) ([]error, error)
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
	md               metadata.MD
	authorizedView   string
	materializedView string

	// divertible, when non-nil, layers session routing on top of the
	// classic body for the methods that have session equivalents:
	// Apply, ReadRow, and single-row shapes of ReadRows. Populated by
	// Open when c.diverter != nil so callers that hold a bare *Table
	// transparently participate in the session/classic split. Nil for
	// classic-only clients — the gate in Table.Apply / Table.ReadRow /
	// Table.ReadRows short-circuits to the *Classic helper, preserving
	// the old fast path.
	//
	// The value is a *TableShim whose classic side wraps a *tableImpl
	// built from a snapshot of this Table with divertible EXPLICITLY
	// nil-ed. That break in the loop is what prevents infinite
	// recursion: the shim's classic branch dispatches into tableImpl,
	// whose Apply/ReadRow/ReadRows overrides land on applyClassic /
	// readRowClassic / readRowsClassic directly — not back through the
	// gated Table.Apply / Table.ReadRow / Table.ReadRows.
	divertible TableAPI
}

// ReadRows bypasses the Table.ReadRows divertible gate — a tableImpl
// used as the classic side of a TableShim would otherwise recurse
// back through the gate into the shim itself when the shim's classic
// branch dispatches a single-row ReadRows call. See Table.divertible.
func (ti *tableImpl) ReadRows(ctx context.Context, arg RowSet, f func(Row) bool, opts ...ReadOption) error {
	return ti.Table.readRowsClassic(ctx, arg, f, opts...)
}

// ReadRow bypasses the Table.ReadRow divertible gate — a tableImpl used
// as the classic side of a TableShim would otherwise recurse back through
// the gate into the shim itself. See Table.divertible.
func (ti *tableImpl) ReadRow(ctx context.Context, row string, opts ...ReadOption) (Row, error) {
	return ti.Table.readRowClassic(ctx, row, opts...)
}

// Apply bypasses the Table.Apply divertible gate — same reason as ReadRow.
func (ti *tableImpl) Apply(ctx context.Context, row string, m *Mutation, opts ...ApplyOption) error {
	return ti.Table.applyClassic(ctx, row, m, opts...)
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

func (ti *tableImpl) newBuiltinMetricsTracer(ctx context.Context, isStreaming bool) *metrics.Tracer {
	return ti.Table.newBuiltinMetricsTracer(ctx, isStreaming)
}

func (t *Table) newBuiltinMetricsTracer(ctx context.Context, isStreaming bool) *metrics.Tracer {
	return t.c.newBuiltinMetricsTracer(ctx, t.table, isStreaming)
}
