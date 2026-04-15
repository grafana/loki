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

func (t *Table) newBuiltinMetricsTracer(ctx context.Context, isStreaming bool) *builtinMetricsTracer {
	return t.c.newBuiltinMetricsTracer(ctx, t.table, isStreaming)
}
