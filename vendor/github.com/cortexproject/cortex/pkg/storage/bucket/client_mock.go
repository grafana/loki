package bucket

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/ioutil"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/thanos-io/thanos/pkg/objstore"
)

var errObjectDoesNotExist = errors.New("object does not exist")

// ClientMock mocks objstore.Bucket
type ClientMock struct {
	mock.Mock
}

// Upload mocks objstore.Bucket.Upload()
func (m *ClientMock) Upload(ctx context.Context, name string, r io.Reader) error {
	args := m.Called(ctx, name, r)
	return args.Error(0)
}

func (m *ClientMock) MockUpload(name string, err error) {
	m.On("Upload", mock.Anything, name, mock.Anything).Return(err)
}

// Delete mocks objstore.Bucket.Delete()
func (m *ClientMock) Delete(ctx context.Context, name string) error {
	args := m.Called(ctx, name)
	return args.Error(0)
}

// Name mocks objstore.Bucket.Name()
func (m *ClientMock) Name() string {
	return "mock"
}

// Iter mocks objstore.Bucket.Iter()
func (m *ClientMock) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	args := m.Called(ctx, dir, f, options)
	return args.Error(0)
}

// MockIter is a convenient method to mock Iter()
func (m *ClientMock) MockIter(prefix string, objects []string, err error) {
	m.MockIterWithCallback(prefix, objects, err, nil)
}

// MockIterWithCallback is a convenient method to mock Iter() and get a callback called when the Iter
// API is called.
func (m *ClientMock) MockIterWithCallback(prefix string, objects []string, err error, cb func()) {
	m.On("Iter", mock.Anything, prefix, mock.Anything, mock.Anything).Return(err).Run(func(args mock.Arguments) {
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
func (m *ClientMock) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	args := m.Called(ctx, name)
	val, err := args.Get(0), args.Error(1)
	if val == nil {
		return nil, err
	}
	return val.(io.ReadCloser), err
}

// MockGet is a convenient method to mock Get() and Exists()
func (m *ClientMock) MockGet(name, content string, err error) {
	if content != "" {
		m.On("Exists", mock.Anything, name).Return(true, err)
		m.On("Attributes", mock.Anything, name).Return(objstore.ObjectAttributes{
			Size:         int64(len(content)),
			LastModified: time.Now(),
		}, nil)

		// Since we return an ReadCloser and it can be consumed only once,
		// each time the mocked Get() is called we do create a new one, so
		// that getting the same mocked object twice works as expected.
		mockedGet := m.On("Get", mock.Anything, name)
		mockedGet.Run(func(args mock.Arguments) {
			mockedGet.Return(ioutil.NopCloser(bytes.NewReader([]byte(content))), err)
		})
	} else {
		m.On("Exists", mock.Anything, name).Return(false, err)
		m.On("Get", mock.Anything, name).Return(nil, errObjectDoesNotExist)
		m.On("Attributes", mock.Anything, name).Return(nil, errObjectDoesNotExist)
	}
}

func (m *ClientMock) MockDelete(name string, err error) {
	m.On("Delete", mock.Anything, name).Return(err)
}

func (m *ClientMock) MockExists(name string, exists bool, err error) {
	m.On("Exists", mock.Anything, name).Return(exists, err)
}

// GetRange mocks objstore.Bucket.GetRange()
func (m *ClientMock) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	args := m.Called(ctx, name, off, length)
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

// Exists mocks objstore.Bucket.Exists()
func (m *ClientMock) Exists(ctx context.Context, name string) (bool, error) {
	args := m.Called(ctx, name)
	return args.Bool(0), args.Error(1)
}

// IsObjNotFoundErr mocks objstore.Bucket.IsObjNotFoundErr()
func (m *ClientMock) IsObjNotFoundErr(err error) bool {
	return err == errObjectDoesNotExist
}

// ObjectSize mocks objstore.Bucket.Attributes()
func (m *ClientMock) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	args := m.Called(ctx, name)
	return args.Get(0).(objstore.ObjectAttributes), args.Error(1)
}

// Close mocks objstore.Bucket.Close()
func (m *ClientMock) Close() error {
	return nil
}
