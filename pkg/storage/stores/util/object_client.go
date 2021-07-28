package util

import (
	"context"
	"io"
	"strings"

	"github.com/grafana/loki/pkg/storage/chunk"
)

type PrefixedObjectClient struct {
	downstreamClient chunk.ObjectClient
	prefix           string
}

func (p PrefixedObjectClient) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return p.downstreamClient.PutObject(ctx, p.prefix+objectKey, object)
}

func (p PrefixedObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	return p.downstreamClient.GetObject(ctx, p.prefix+objectKey)
}

func (p PrefixedObjectClient) List(ctx context.Context, prefix, delimeter string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	objects, commonPrefixes, err := p.downstreamClient.List(ctx, p.prefix+prefix, delimeter)
	if err != nil {
		return nil, nil, err
	}

	for i := range objects {
		objects[i].Key = strings.TrimPrefix(objects[i].Key, p.prefix)
	}

	for i := range commonPrefixes {
		commonPrefixes[i] = chunk.StorageCommonPrefix(strings.TrimPrefix(string(commonPrefixes[i]), p.prefix))
	}

	return objects, commonPrefixes, nil
}

func (p PrefixedObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return p.downstreamClient.DeleteObject(ctx, p.prefix+objectKey)
}

func (p PrefixedObjectClient) Stop() {
	p.downstreamClient.Stop()
}

func NewPrefixedObjectClient(downstreamClient chunk.ObjectClient, prefix string) chunk.ObjectClient {
	return PrefixedObjectClient{downstreamClient: downstreamClient, prefix: prefix}
}
