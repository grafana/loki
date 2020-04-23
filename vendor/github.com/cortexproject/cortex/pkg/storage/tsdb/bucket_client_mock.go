package tsdb

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/ioutil"

	"github.com/stretchr/testify/mock"
)

var errObjectDoesNotExist = errors.New("object does not exist")

// BucketClientMock mocks objstore.Bucket
type BucketClientMock struct {
	mock.Mock
}

// Upload mocks objstore.Bucket.Upload()
func (m *BucketClientMock) Upload(ctx context.Context, name string, r io.Reader) error {
	args := m.Called(ctx, name, r)
	return args.Error(0)
}

// Delete mocks objstore.Bucket.Delete()
func (m *BucketClientMock) Delete(ctx context.Context, name string) error {
	args := m.Called(ctx, name)
	return args.Error(0)
}

// Name mocks objstore.Bucket.Name()
func (m *BucketClientMock) Name() string {
	return "mock"
}

// Iter mocks objstore.Bucket.Iter()
func (m *BucketClientMock) Iter(ctx context.Context, dir string, f func(string) error) error {
	args := m.Called(ctx, dir, f)
	return args.Error(0)
}

// MockIter is a convenient method to mock Iter()
func (m *BucketClientMock) MockIter(prefix string, objects []string, err error) {
	m.MockIterWithCallback(prefix, objects, err, nil)
}

// MockIterWithCallback is a convenient method to mock Iter() and get a callback called when the Iter
// API is called.
func (m *BucketClientMock) MockIterWithCallback(prefix string, objects []string, err error, cb func()) {
	m.On("Iter", mock.Anything, prefix, mock.Anything).Return(err).Run(func(args mock.Arguments) {
		if cb != nil {
			cb()
		}

		f := args.Get(2).(func(string) error)

		for _, o := range objects {
			if f(o) != nil {
				break
			}
		}
	})
}

// Get mocks objstore.Bucket.Get()
func (m *BucketClientMock) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	args := m.Called(ctx, name)
	val, err := args.Get(0), args.Error(1)
	if val == nil {
		return nil, err
	}
	return val.(io.ReadCloser), err
}

// MockGet is a convenient method to mock Get() and Exists()
func (m *BucketClientMock) MockGet(name, content string, err error) {
	if content != "" {
		m.On("Exists", mock.Anything, name).Return(true, err)
		m.On("Get", mock.Anything, name).Return(ioutil.NopCloser(bytes.NewReader([]byte(content))), err)
	} else {
		m.On("Exists", mock.Anything, name).Return(false, err)
		m.On("Get", mock.Anything, name).Return(nil, errObjectDoesNotExist)
	}
}

func (m *BucketClientMock) MockDelete(name string, err error) {
	m.On("Delete", mock.Anything, name).Return(err)
}

// GetRange mocks objstore.Bucket.GetRange()
func (m *BucketClientMock) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	args := m.Called(ctx, name, off, length)
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

// Exists mocks objstore.Bucket.Exists()
func (m *BucketClientMock) Exists(ctx context.Context, name string) (bool, error) {
	args := m.Called(ctx, name)
	return args.Bool(0), args.Error(1)
}

// IsObjNotFoundErr mocks objstore.Bucket.IsObjNotFoundErr()
func (m *BucketClientMock) IsObjNotFoundErr(err error) bool {
	return err == errObjectDoesNotExist
}

// ObjectSize mocks objstore.Bucket.ObjectSize()
func (m *BucketClientMock) ObjectSize(ctx context.Context, name string) (uint64, error) {
	args := m.Called(ctx, name)
	return args.Get(0).(uint64), args.Error(1)
}

// Close mocks objstore.Bucket.Close()
func (m *BucketClientMock) Close() error {
	return nil
}
