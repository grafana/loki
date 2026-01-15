package main

import (
	"context"
	"log"

	glog "github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/storage/bucket/gcs"
	"github.com/thanos-io/objstore"
)

// MustDataobjBucket creates a GCS bucket client for dataobj storage
func MustDataobjBucket() objstore.Bucket {
	bkt, err := gcs.NewBucketClient(context.Background(), gcs.Config{
		BucketName: storageBucket,
	}, "testing", glog.NewNopLogger(), nil)
	if err != nil {
		log.Fatal(err)
	}
	objBucket := objstore.NewPrefixedBucket(bkt, "dataobj")
	return objBucket
}

// MustRawBucket creates a GCS bucket client for raw storage
func MustRawBucket() objstore.Bucket {
	bkt, err := gcs.NewBucketClient(context.Background(), gcs.Config{
		BucketName: storageBucket,
	}, "testing", glog.NewNopLogger(), nil)
	if err != nil {
		log.Fatal(err)
	}
	return bkt
}
