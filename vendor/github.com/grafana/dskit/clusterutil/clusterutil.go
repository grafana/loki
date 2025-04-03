package clusterutil

import (
	"context"
	"fmt"

	"google.golang.org/grpc/metadata"
)

const (
	// MetadataClusterValidationLabelKey is the key of the cluster validation label gRPC metadata.
	MetadataClusterValidationLabelKey = "x-cluster"
)

var (
	ErrNoClusterValidationLabel         = fmt.Errorf("no cluster validation label in context")
	errDifferentClusterValidationLabels = func(clusterIDs []string) error {
		return fmt.Errorf("gRPC metadata should contain exactly 1 value for key %q, but it contains %v", MetadataClusterValidationLabelKey, clusterIDs)
	}
)

// PutClusterIntoOutgoingContext returns a new context with the provided value for
// MetadataClusterValidationLabelKey, merged with any existing metadata in the context.
// Empty values are ignored.
func PutClusterIntoOutgoingContext(ctx context.Context, cluster string) context.Context {
	if cluster == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, MetadataClusterValidationLabelKey, cluster)
}

// GetClusterFromIncomingContext returns a single metadata value corresponding to the
// MetadataClusterValidationLabelKey key from the incoming context, if it exists.
// In all other cases an error is returned.
func GetClusterFromIncomingContext(ctx context.Context) (string, error) {
	clusterIDs := metadata.ValueFromIncomingContext(ctx, MetadataClusterValidationLabelKey)
	if len(clusterIDs) > 1 {
		return "", errDifferentClusterValidationLabels(clusterIDs)
	}
	if len(clusterIDs) == 0 || clusterIDs[0] == "" {
		return "", ErrNoClusterValidationLabel
	}
	return clusterIDs[0], nil
}
