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
	"fmt"
	"io"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"cloud.google.com/go/internal/trace"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

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
	defer mt.recordOperationCompletion()

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
	}, t.c.retryOption)

	statusCode, statusErr := convertToGrpcStatusErr(err)
	mt.setCurrOpStatus(statusCode)
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
				entryErrs[entry.Index].Err = status.Error(codes.Code(s.Code), s.Message)
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
