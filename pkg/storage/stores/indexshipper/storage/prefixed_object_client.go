package storage

import (
	"context"
	"io"
	"strings"

	"github.com/grafana/loki/pkg/storage/chunk/client"
)

type prefixedObjectClient struct {
	downstreamClient client.ObjectClient
	prefix           string
}

func newPrefixedObjectClient(downstreamClient client.ObjectClient, prefix string) client.ObjectClient {
	return prefixedObjectClient{downstreamClient: downstreamClient, prefix: prefix}
}

func (p prefixedObjectClient) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return p.downstreamClient.PutObject(ctx, p.prefix+objectKey, object)
}

func (p prefixedObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	return p.downstreamClient.GetObject(ctx, p.prefix+objectKey)
}

func (p prefixedObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	objects, commonPrefixes, err := p.downstreamClient.List(ctx, p.prefix+prefix, delimiter)
	if err != nil {
		return nil, nil, err
	}

	for i := range objects {
		objects[i].Key = strings.TrimPrefix(objects[i].Key, p.prefix)
	}

	for i := range commonPrefixes {
		commonPrefixes[i] = client.StorageCommonPrefix(strings.TrimPrefix(string(commonPrefixes[i]), p.prefix))
	}

	return objects, commonPrefixes, nil
}

func (p prefixedObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return p.downstreamClient.DeleteObject(ctx, p.prefix+objectKey)
}

func (p prefixedObjectClient) IsObjectNotFoundErr(err error) bool {
	return p.downstreamClient.IsObjectNotFoundErr(err)
}

func (p prefixedObjectClient) IsRetryableErr(err error) bool {
	return p.downstreamClient.IsRetryableErr(err)
}

func (p prefixedObjectClient) Stop() {
	p.downstreamClient.Stop()
}
