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
	"errors"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ApplyReadModifyWrite applies a ReadModifyWrite to a specific row.
// It returns the newly written cells.
func (t *Table) ApplyReadModifyWrite(ctx context.Context, row string, m *ReadModifyWrite) (Row, error) {
	ctx = mergeOutgoingMetadata(ctx, t.md)

	mt := t.newBuiltinMetricsTracer(ctx, false)
	defer mt.recordOperationCompletion()

	updatedRow, err := t.applyReadModifyWrite(ctx, mt, row, m)
	statusCode, statusErr := convertToGrpcStatusErr(err)
	mt.setCurrOpStatus(statusCode)
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
