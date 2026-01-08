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
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/bigtable/internal"
	"cloud.google.com/go/internal/version"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const (
	// LoadBalancingStrategyEnvVar is the environment variable to control the gRPC load balancing strategy.
	LoadBalancingStrategyEnvVar = "CBT_LOAD_BALANCING_STRATEGY"
	// RoundRobinLBPolicy is the policy name for round-robin.
	RoundRobinLBPolicy = "round_robin"
	// LeastInFlightLBPolicy is the policy name for least in flight (custom).
	LeastInFlightLBPolicy = "least_in_flight"
	// PowerOfTwoLeastInFlightLBPolicy is the policy name for power of two least in flight (custom).
	PowerOfTwoLeastInFlightLBPolicy = "power_of_two_least_in_flight"
	// BigtableConnectionPoolEnvVar is the env var for enabling Bigtable Connection Pool.
	BigtableConnectionPoolEnvVar = "CBT_BIGTABLE_CONN_POOL"
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

// withClientAttemptEpochUsec sets the client epoch in usec.
func withClientAttemptEpochUsec() metadata.MD {
	return metadata.Pairs("bigtable-client-attempt-epoch-usec", strconv.FormatInt(time.Now().UnixMicro(), 10))
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
		"gccl",
		internal.Version,
	}
	return metadata.Pairs("x-goog-api-client", gax.XGoogHeader(kv...))
}

// streamInterceptor intercepts the creation of ClientStream within the bigtable
// client to inject Google client information into the context metadata for
// streaming RPCs.
func streamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ctx = mergeOutgoingMetadata(ctx, withGoogleClientInfo(), withClientAttemptEpochUsec())
	return streamer(ctx, desc, cc, method, opts...)
}

// unaryInterceptor intercepts the creation of UnaryInvoker within the bigtable
// client to inject Google client information into the context metadata for
// unary RPCs.
func unaryInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ctx = mergeOutgoingMetadata(ctx, withGoogleClientInfo(), withClientAttemptEpochUsec())
	return invoker(ctx, method, req, reply, cc, opts...)
}

// DefaultClientOptions returns the default client options to use for the
// client's gRPC connection.
func DefaultClientOptions(endpoint, mtlsEndpoint, scope, userAgent string) ([]option.ClientOption, error) {
	var o []option.ClientOption
	// Check the environment variables for the bigtable emulator.
	// Dial it directly and don't pass any credentials.
	if addr := os.Getenv("BIGTABLE_EMULATOR_HOST"); addr != "" {
		conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, fmt.Errorf("emulator grpc.Dial: %w", err)
		}
		o = []option.ClientOption{option.WithGRPCConn(conn)}
	} else {
		o = []option.ClientOption{
			internaloption.WithDefaultEndpointTemplate(endpoint),
			internaloption.WithDefaultMTLSEndpoint(mtlsEndpoint),
			internaloption.WithDefaultUniverseDomain("googleapis.com"),
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

// LoadBalancingStrategy for connection pool.
type LoadBalancingStrategy int

const (
	// RoundRobin is the round_robin gRPC load balancing policy.
	RoundRobin LoadBalancingStrategy = iota
	// LeastInFlight is the least_in_flight gRPC load balancing policy (custom).
	LeastInFlight
	// PowerOfTwoLeastInFlight is the power_of_two_least_in_flight gRPC load balancing policy (custom).
	PowerOfTwoLeastInFlight
)

// String returns the string representation of the LoadBalancingStrategy.
func (s LoadBalancingStrategy) String() string {
	switch s {
	case LeastInFlight:
		return "least_in_flight"
	case PowerOfTwoLeastInFlight:
		return "power_of_two_least_in_flight"
	case RoundRobin:
		return "round_robin"
	default:
		return "round_robin" // Default
	}
}

// parseLoadBalancingStrategy parses the string from the environment variable
// into a LoadBalancingStrategy enum value.
func parseLoadBalancingStrategy(strategyStr string) LoadBalancingStrategy {
	switch strings.ToUpper(strategyStr) {
	case "LEAST_IN_FLIGHT":
		return LeastInFlight
	case "POWER_OF_TWO_LEAST_IN_FLIGHT":
		return PowerOfTwoLeastInFlight
	case "ROUND_ROBIN":
		return RoundRobin
	case "":
		return RoundRobin // Default if env var is not set
	default:
		return RoundRobin // Default for unknown values
	}
}

// BigtableLoadBalancingStrategy returns the gRPC service config JSON string for the chosen policy.
func BigtableLoadBalancingStrategy() LoadBalancingStrategy {
	strategyStr := os.Getenv(LoadBalancingStrategyEnvVar)
	return parseLoadBalancingStrategy(strategyStr)
}

// EnableBigtableConnectionPool uses new conn pool if envVar is set.
func EnableBigtableConnectionPool() bool {
	bigtableConnPoolEnvVal := os.Getenv(BigtableConnectionPoolEnvVar)
	if bigtableConnPoolEnvVal == "" {
		return false
	}
	enableBigtableConnPool, err := strconv.ParseBool(bigtableConnPoolEnvVal)
	if err != nil {
		// just fail and use default conn pool
		return false
	}
	return enableBigtableConnPool
}
