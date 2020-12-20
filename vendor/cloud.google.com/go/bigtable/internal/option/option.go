/*
Copyright 2015 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package option contains common code for dealing with client options.
package option

import (
	"context"
	"fmt"
	"os"

	"cloud.google.com/go/internal/version"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// mergeOutgoingMetadata returns a context populated by the existing outgoing
// metadata merged with the provided mds.
func mergeOutgoingMetadata(ctx context.Context, mds ...metadata.MD) context.Context {
	// There may not be metadata in the context, only insert the existing
	// metadata if it exists (ok).
	ctxMD, ok := metadata.FromOutgoingContext(ctx)
	if ok {
		// The ordering matters, hence why ctxMD is added to the front.
		mds = append([]metadata.MD{ctxMD}, mds...)
	}

	return metadata.NewOutgoingContext(ctx, metadata.Join(mds...))
}

// withGoogleClientInfo sets the name and version of the application in
// the `x-goog-api-client` header passed on each request. Intended for
// use by Google-written clients.
func withGoogleClientInfo() metadata.MD {
	kv := []string{
		"gl-go",
		version.Go(),
		"gax",
		gax.Version,
		"grpc",
		grpc.Version,
	}
	return metadata.Pairs("x-goog-api-client", gax.XGoogHeader(kv...))
}

// streamInterceptor intercepts the creation of ClientStream within the bigtable
// client to inject Google client information into the context metadata for
// streaming RPCs.
func streamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ctx = mergeOutgoingMetadata(ctx, withGoogleClientInfo())
	return streamer(ctx, desc, cc, method, opts...)
}

// unaryInterceptor intercepts the creation of UnaryInvoker within the bigtable
// client to inject Google client information into the context metadata for
// unary RPCs.
func unaryInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ctx = mergeOutgoingMetadata(ctx, withGoogleClientInfo())
	return invoker(ctx, method, req, reply, cc, opts...)
}

// DefaultClientOptions returns the default client options to use for the
// client's gRPC connection.
func DefaultClientOptions(endpoint, scope, userAgent string) ([]option.ClientOption, error) {
	var o []option.ClientOption
	// Check the environment variables for the bigtable emulator.
	// Dial it directly and don't pass any credentials.
	if addr := os.Getenv("BIGTABLE_EMULATOR_HOST"); addr != "" {
		conn, err := grpc.Dial(addr, grpc.WithInsecure())
		if err != nil {
			return nil, fmt.Errorf("emulator grpc.Dial: %v", err)
		}
		o = []option.ClientOption{option.WithGRPCConn(conn)}
	} else {
		o = []option.ClientOption{
			option.WithEndpoint(endpoint),
			option.WithScopes(scope),
			option.WithUserAgent(userAgent),
		}
	}
	return o, nil
}

// ClientInterceptorOptions returns client options to use for the client's gRPC
// connection, using the given streaming and unary RPC interceptors.
//
// The passed interceptors are applied after internal interceptors which inject
// Google client information into the gRPC context.
func ClientInterceptorOptions(stream []grpc.StreamClientInterceptor, unary []grpc.UnaryClientInterceptor) []option.ClientOption {
	// By prepending the interceptors defined here, they will be applied first.
	stream = append([]grpc.StreamClientInterceptor{streamInterceptor}, stream...)
	unary = append([]grpc.UnaryClientInterceptor{unaryInterceptor}, unary...)
	return []option.ClientOption{
		option.WithGRPCDialOption(grpc.WithChainStreamInterceptor(stream...)),
		option.WithGRPCDialOption(grpc.WithChainUnaryInterceptor(unary...)),
	}
}
