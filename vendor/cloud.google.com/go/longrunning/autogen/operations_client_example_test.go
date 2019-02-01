// Copyright 2018 Google LLC
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

// AUTO-GENERATED CODE. DO NOT EDIT.

package longrunning_test

import (
	"context"

	longrunning "cloud.google.com/go/longrunning/autogen"
	"google.golang.org/api/iterator"
	longrunningpb "google.golang.org/genproto/googleapis/longrunning"
)

func ExampleNewOperationsClient() {
	ctx := context.Background()
	c, err := longrunning.NewOperationsClient(ctx)
	if err != nil {
		// TODO: Handle error.
	}
	// TODO: Use client.
	_ = c
}

func ExampleOperationsClient_GetOperation() {
	ctx := context.Background()
	c, err := longrunning.NewOperationsClient(ctx)
	if err != nil {
		// TODO: Handle error.
	}

	req := &longrunningpb.GetOperationRequest{
		// TODO: Fill request struct fields.
	}
	resp, err := c.GetOperation(ctx, req)
	if err != nil {
		// TODO: Handle error.
	}
	// TODO: Use resp.
	_ = resp
}

func ExampleOperationsClient_ListOperations() {
	ctx := context.Background()
	c, err := longrunning.NewOperationsClient(ctx)
	if err != nil {
		// TODO: Handle error.
	}

	req := &longrunningpb.ListOperationsRequest{
		// TODO: Fill request struct fields.
	}
	it := c.ListOperations(ctx, req)
	for {
		resp, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			// TODO: Handle error.
		}
		// TODO: Use resp.
		_ = resp
	}
}

func ExampleOperationsClient_CancelOperation() {
	ctx := context.Background()
	c, err := longrunning.NewOperationsClient(ctx)
	if err != nil {
		// TODO: Handle error.
	}

	req := &longrunningpb.CancelOperationRequest{
		// TODO: Fill request struct fields.
	}
	err = c.CancelOperation(ctx, req)
	if err != nil {
		// TODO: Handle error.
	}
}

func ExampleOperationsClient_DeleteOperation() {
	ctx := context.Background()
	c, err := longrunning.NewOperationsClient(ctx)
	if err != nil {
		// TODO: Handle error.
	}

	req := &longrunningpb.DeleteOperationRequest{
		// TODO: Fill request struct fields.
	}
	err = c.DeleteOperation(ctx, req)
	if err != nil {
		// TODO: Handle error.
	}
}
