package client

import (
	"context"
	"io"
	"strings"
)

type PrefixedObjectClient struct {
	downstreamClient ObjectClient
	prefix           string
}

func NewPrefixedObjectClient(downstreamClient ObjectClient, prefix string) ObjectClient {
	return PrefixedObjectClient{downstreamClient: downstreamClient, prefix: prefix}
}

func (p PrefixedObjectClient) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	return p.downstreamClient.PutObject(ctx, p.prefix+objectKey, object)
}

func (p PrefixedObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	return p.downstreamClient.ObjectExists(ctx, p.prefix+objectKey)
}

func (p PrefixedObjectClient) GetAttributes(ctx context.Context, objectKey string) (ObjectAttributes, error) {
	return p.downstreamClient.GetAttributes(ctx, p.prefix+objectKey)
}

func (p PrefixedObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	return p.downstreamClient.GetObject(ctx, p.prefix+objectKey)
}

func (p PrefixedObjectClient) GetObjectRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	return p.downstreamClient.GetObjectRange(ctx, p.prefix+objectKey, offset, length)
}

func (p PrefixedObjectClient) List(ctx context.Context, prefix, delimiter string) ([]StorageObject, []StorageCommonPrefix, error) {
	objects, commonPrefixes, err := p.downstreamClient.List(ctx, p.prefix+prefix, delimiter)
	if err != nil {
		return nil, nil, err
	}

	for i := range objects {
		objects[i].Key = strings.TrimPrefix(objects[i].Key, p.prefix)
	}

	for i := range commonPrefixes {
		commonPrefixes[i] = StorageCommonPrefix(strings.TrimPrefix(string(commonPrefixes[i]), p.prefix))
	}

	return objects, commonPrefixes, nil
}

func (p PrefixedObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return p.downstreamClient.DeleteObject(ctx, p.prefix+objectKey)
}

func (p PrefixedObjectClient) IsObjectNotFoundErr(err error) bool {
	return p.downstreamClient.IsObjectNotFoundErr(err)
}

func (p PrefixedObjectClient) IsRetryableErr(err error) bool {
	return p.downstreamClient.IsRetryableErr(err)
}

func (p PrefixedObjectClient) Stop() {
	p.downstreamClient.Stop()
}

func (p PrefixedObjectClient) GetDownstream() ObjectClient {
	return p.downstreamClient
}

func (p PrefixedObjectClient) GetPrefix() string {
	return p.prefix
}
