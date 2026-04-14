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
	"io"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/grpc/metadata"
)

// SampleRowKeys returns a sample of row keys in the table. The returned row keys will delimit contiguous sections of
// the table of approximately equal size, which can be used to break up the data for distributed tasks like mapreduces.
func (t *Table) SampleRowKeys(ctx context.Context) ([]string, error) {
	ctx = mergeOutgoingMetadata(ctx, t.md)

	mt := t.newBuiltinMetricsTracer(ctx, true)
	defer mt.recordOperationCompletion()

	rowKeys, err := t.sampleRowKeys(ctx, mt)
	statusCode, statusErr := convertToGrpcStatusErr(err)
	mt.setCurrOpStatus(statusCode)
	return rowKeys, statusErr
}

func (t *Table) sampleRowKeys(ctx context.Context, mt *builtinMetricsTracer) ([]string, error) {
	var sampledRowKeys []string
	err := gaxInvokeWithRecorder(ctx, mt, "SampleRowKeys", func(ctx context.Context, headerMD, trailerMD *metadata.MD, _ gax.CallSettings) error {
		sampledRowKeys = nil
		req := &btpb.SampleRowKeysRequest{
			AppProfileId: t.c.appProfile,
		}
		if t.materializedView != "" {
			req.MaterializedViewName = t.c.fullMaterializedViewName(t.materializedView)
		} else if t.authorizedView == "" {
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
	}, t.c.retryOption)

	return sampledRowKeys, err
}
