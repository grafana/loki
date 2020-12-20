package util

import (
	"context"

	"google.golang.org/grpc/metadata"
)

// ipAddressesKey is key for the GRPC metadata where the IP addresses are stored
const ipAddressesKey = "extract-forwarded-x-forwarded-for"

// GetSourceIPsFromOutgoingCtx extracts the source field from the GRPC context
func GetSourceIPsFromOutgoingCtx(ctx context.Context) string {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return ""
	}
	ipAddresses, ok := md[ipAddressesKey]
	if !ok {
		return ""
	}
	return ipAddresses[0]
}

// GetSourceIPsFromIncomingCtx extracts the source field from the GRPC context
func GetSourceIPsFromIncomingCtx(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	ipAddresses, ok := md[ipAddressesKey]
	if !ok {
		return ""
	}
	return ipAddresses[0]
}

// AddSourceIPsToOutgoingContext adds the given source to the GRPC context
func AddSourceIPsToOutgoingContext(ctx context.Context, source string) context.Context {
	if source != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, ipAddressesKey, source)
	}
	return ctx
}

// AddSourceIPsToIncomingContext adds the given source to the GRPC context
func AddSourceIPsToIncomingContext(ctx context.Context, source string) context.Context {
	if source != "" {
		md := metadata.Pairs(ipAddressesKey, source)
		ctx = metadata.NewIncomingContext(ctx, md)
	}
	return ctx
}
