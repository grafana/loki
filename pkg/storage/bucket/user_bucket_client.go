package bucket

import (
	"github.com/thanos-io/objstore"
)

// NewUserBucketClient returns a bucket client to use to access the storage on behalf of the provided user.
// The cfgProvider can be nil.
func NewUserBucketClient(userID string, bucket objstore.Bucket, cfgProvider SSEConfigProvider) objstore.InstrumentedBucket {
	// Inject the user/tenant prefix.
	bucket = NewPrefixedBucketClient(bucket, userID)

	// Inject the SSE config.
	return NewSSEBucketClient(userID, bucket, cfgProvider)
}
