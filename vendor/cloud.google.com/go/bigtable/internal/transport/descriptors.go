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

package internal

import (
	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"google.golang.org/protobuf/proto"
)

// VRpcDescriptor defines the interface for virtual RPC encoding and decoding.
type VRpcDescriptor interface {
	Method() string
	Encode(req interface{}) ([]byte, error)
	Decode(buf []byte) (interface{}, error)
}

// VRpcDescriptorImpl implements VRpcDescriptor using custom encoder/decoder closures.
type VRpcDescriptorImpl struct {
	MethodName string
	EncodeFn   func(req interface{}) ([]byte, error)
	DecodeFn   func(buf []byte) (interface{}, error)
}

// Method returns the virtual RPC method identifier name.
func (d *VRpcDescriptorImpl) Method() string {
	return d.MethodName
}

// Encode serializes standard Bigtable request payload into virtual RPC bytes.
func (d *VRpcDescriptorImpl) Encode(req interface{}) ([]byte, error) {
	return d.EncodeFn(req)
}

// Decode de-serializes virtual RPC bytes back into standard Bigtable response message.
func (d *VRpcDescriptorImpl) Decode(buf []byte) (interface{}, error) {
	return d.DecodeFn(buf)
}

// vRPC Encoder/Decoder factories

func createTableEncoder(subEncoder func(req interface{}, envelope *btpb.TableRequest)) func(interface{}) ([]byte, error) {
	return func(req interface{}) ([]byte, error) {
		envelope := &btpb.TableRequest{}
		subEncoder(req, envelope)
		return proto.Marshal(envelope)
	}
}

func createTableDecoder(subDecoder func(envelope *btpb.TableResponse) interface{}) func([]byte) (interface{}, error) {
	return func(buf []byte) (interface{}, error) {
		envelope := &btpb.TableResponse{}
		if err := proto.Unmarshal(buf, envelope); err != nil {
			return nil, err
		}
		return subDecoder(envelope), nil
	}
}

func createAuthViewEncoder(subEncoder func(req interface{}, envelope *btpb.AuthorizedViewRequest)) func(interface{}) ([]byte, error) {
	return func(req interface{}) ([]byte, error) {
		envelope := &btpb.AuthorizedViewRequest{}
		subEncoder(req, envelope)
		return proto.Marshal(envelope)
	}
}

func createAuthViewDecoder(subDecoder func(envelope *btpb.AuthorizedViewResponse) interface{}) func([]byte) (interface{}, error) {
	return func(buf []byte) (interface{}, error) {
		envelope := &btpb.AuthorizedViewResponse{}
		if err := proto.Unmarshal(buf, envelope); err != nil {
			return nil, err
		}
		return subDecoder(envelope), nil
	}
}

func createMatViewEncoder(subEncoder func(req interface{}, envelope *btpb.MaterializedViewRequest)) func(interface{}) ([]byte, error) {
	return func(req interface{}) ([]byte, error) {
		envelope := &btpb.MaterializedViewRequest{}
		subEncoder(req, envelope)
		return proto.Marshal(envelope)
	}
}

func createMatViewDecoder(subDecoder func(envelope *btpb.MaterializedViewResponse) interface{}) func([]byte) (interface{}, error) {
	return func(buf []byte) (interface{}, error) {
		envelope := &btpb.MaterializedViewResponse{}
		if err := proto.Unmarshal(buf, envelope); err != nil {
			return nil, err
		}
		return subDecoder(envelope), nil
	}
}

// ReadRowArgs contains arguments required for a virtual RPC ReadRow call.
type ReadRowArgs struct {
	RowKey string
	Filter *btpb.RowFilter
}

// ReadRowResult holds the result returned from a virtual RPC ReadRow call.
type ReadRowResult struct {
	Row *btpb.Row
}

// MutateRowArgs contains arguments required for a virtual RPC MutateRow call.
type MutateRowArgs struct {
	RowKey    string
	Mutations []*btpb.Mutation
}

// MutateRowResult holds the result of a MutateRow virtual RPC.
type MutateRowResult struct{}

func encodeReadRow(args ReadRowArgs) *btpb.SessionReadRowRequest {
	return &btpb.SessionReadRowRequest{
		Key:    []byte(args.RowKey),
		Filter: args.Filter,
	}
}

func decodeReadRow(resp *btpb.SessionReadRowResponse) ReadRowResult {
	if resp == nil {
		return ReadRowResult{}
	}
	return ReadRowResult{
		Row: resp.Row,
	}
}

func encodeMutateRow(args MutateRowArgs) *btpb.SessionMutateRowRequest {
	return &btpb.SessionMutateRowRequest{
		Key:       []byte(args.RowKey),
		Mutations: args.Mutations,
	}
}

func decodeMutateRow(resp *btpb.SessionMutateRowResponse) MutateRowResult {
	return MutateRowResult{}
}

var (
	// READ_ROW executes Point Reads on standard tables.
	READ_ROW = &VRpcDescriptorImpl{
		MethodName: "ReadRow",
		EncodeFn: createTableEncoder(func(req interface{}, env *btpb.TableRequest) {
			args := req.(ReadRowArgs)
			env.Payload = &btpb.TableRequest_ReadRow{
				ReadRow: encodeReadRow(args),
			}
		}),
		DecodeFn: createTableDecoder(func(env *btpb.TableResponse) interface{} {
			return decodeReadRow(env.GetReadRow())
		}),
	}

	// MUTATE_ROW executes Point Mutations on standard tables.
	MUTATE_ROW = &VRpcDescriptorImpl{
		MethodName: "MutateRow",
		EncodeFn: createTableEncoder(func(req interface{}, env *btpb.TableRequest) {
			args := req.(MutateRowArgs)
			env.Payload = &btpb.TableRequest_MutateRow{
				MutateRow: encodeMutateRow(args),
			}
		}),
		DecodeFn: createTableDecoder(func(env *btpb.TableResponse) interface{} {
			return decodeMutateRow(env.GetMutateRow())
		}),
	}

	// READ_ROW_AUTH_VIEW executes Point Reads on Authorized Views.
	READ_ROW_AUTH_VIEW = &VRpcDescriptorImpl{
		MethodName: "ReadRow",
		EncodeFn: createAuthViewEncoder(func(req interface{}, env *btpb.AuthorizedViewRequest) {
			args := req.(ReadRowArgs)
			env.Payload = &btpb.AuthorizedViewRequest_ReadRow{
				ReadRow: encodeReadRow(args),
			}
		}),
		DecodeFn: createAuthViewDecoder(func(env *btpb.AuthorizedViewResponse) interface{} {
			return decodeReadRow(env.GetReadRow())
		}),
	}

	// MUTATE_ROW_AUTH_VIEW executes Point Mutations on Authorized Views.
	MUTATE_ROW_AUTH_VIEW = &VRpcDescriptorImpl{
		MethodName: "MutateRow",
		EncodeFn: createAuthViewEncoder(func(req interface{}, env *btpb.AuthorizedViewRequest) {
			args := req.(MutateRowArgs)
			env.Payload = &btpb.AuthorizedViewRequest_MutateRow{
				MutateRow: encodeMutateRow(args),
			}
		}),
		DecodeFn: createAuthViewDecoder(func(env *btpb.AuthorizedViewResponse) interface{} {
			return decodeMutateRow(env.GetMutateRow())
		}),
	}

	// READ_ROW_MAT_VIEW executes Point Reads on Materialized Views.
	READ_ROW_MAT_VIEW = &VRpcDescriptorImpl{
		MethodName: "ReadRow",
		EncodeFn: createMatViewEncoder(func(req interface{}, env *btpb.MaterializedViewRequest) {
			args := req.(ReadRowArgs)
			env.Payload = &btpb.MaterializedViewRequest_ReadRow{
				ReadRow: encodeReadRow(args),
			}
		}),
		DecodeFn: createMatViewDecoder(func(env *btpb.MaterializedViewResponse) interface{} {
			return decodeReadRow(env.GetReadRow())
		}),
	}
)
