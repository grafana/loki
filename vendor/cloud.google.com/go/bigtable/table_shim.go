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

	internal "cloud.google.com/go/bigtable/internal/transport"
)

// TableShim wraps a classic and a session-based TableAPI and diverts traffic between them.
type TableShim struct {
	classic  TableAPI
	session  TableAPI
	diverter *internal.Diverter
}

// NewTableShim creates a new TableShim.
func NewTableShim(classic, session TableAPI, diverter *internal.Diverter) TableAPI {
	return &TableShim{
		classic:  classic,
		session:  session,
		diverter: diverter,
	}
}

// ReadRow implements TableAPI.
func (t *TableShim) ReadRow(ctx context.Context, row string, opts ...ReadOption) (Row, error) {
	if t.diverter.UseSession() {
		return t.session.ReadRow(ctx, row, opts...)
	}
	return t.classic.ReadRow(ctx, row, opts...)
}

// Apply implements TableAPI.
func (t *TableShim) Apply(ctx context.Context, row string, m *Mutation, opts ...ApplyOption) error {
	if t.diverter.UseSession() {
		return t.session.Apply(ctx, row, m, opts...)
	}
	return t.classic.Apply(ctx, row, m, opts...)
}

// ReadRows implements TableAPI. It delegates to classic as session support is not yet implemented.
func (t *TableShim) ReadRows(ctx context.Context, arg RowSet, f func(Row) bool, opts ...ReadOption) error {
	return t.classic.ReadRows(ctx, arg, f, opts...)
}

// SampleRowKeys implements TableAPI. It delegates to classic.
func (t *TableShim) SampleRowKeys(ctx context.Context) ([]string, error) {
	return t.classic.SampleRowKeys(ctx)
}

// ApplyBulk implements TableAPI. It delegates to classic.
func (t *TableShim) ApplyBulk(ctx context.Context, rowKeys []string, muts []*Mutation, opts ...ApplyOption) ([]error, error) {
	return t.classic.ApplyBulk(ctx, rowKeys, muts, opts...)
}

// ApplyReadModifyWrite implements TableAPI. It delegates to classic.
func (t *TableShim) ApplyReadModifyWrite(ctx context.Context, row string, m *ReadModifyWrite) (Row, error) {
	return t.classic.ApplyReadModifyWrite(ctx, row, m)
}
