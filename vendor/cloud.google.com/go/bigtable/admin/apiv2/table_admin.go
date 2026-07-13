// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package admin

import (
	"context"
	"time"

	adminpb "cloud.google.com/go/bigtable/admin/apiv2/adminpb"
	gax "github.com/googleapis/gax-go/v2"
)

// RestoreTable creates a new table by restoring from a backup.
func (c *BigtableTableAdminClient) RestoreTable(ctx context.Context, req *adminpb.RestoreTableRequest, opts ...gax.CallOption) error {
	op, err := c.restoreTable(ctx, req, opts...)
	if err != nil {
		return err
	}

	// Poll the LRO until the table is usable
	if _, err := op.Wait(ctx, opts...); err != nil {
		return err
	}

	return nil
}

// WaitForConsistency waits until all the writes committed before the call started have been propagated to all the clusters in the instance via replication.
func (c *BigtableTableAdminClient) WaitForConsistency(ctx context.Context, tableName string, opts ...gax.CallOption) error {
	// Get the token.
	tokenResp, err := c.GenerateConsistencyToken(ctx, &adminpb.GenerateConsistencyTokenRequest{
		Name: tableName,
	}, opts...)
	if err != nil {
		return err
	}
	token := tokenResp.GetConsistencyToken()

	// Periodically check if the token is consistent.
	timer := time.NewTicker(time.Second * 10)
	defer timer.Stop()
	for {
		consistentResp, err := c.CheckConsistency(ctx, &adminpb.CheckConsistencyRequest{
			Name:             tableName,
			ConsistencyToken: token,
		}, opts...)
		if err != nil {
			return err
		}
		if consistentResp.GetConsistent() {
			return nil
		}
		// Sleep for a bit or until the ctx is cancelled.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}
