package azure

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

type mockBucket struct {
	objstore.Bucket
	lastKey  string
	iterKeys []string
}

func (m *mockBucket) Get(_ context.Context, name string) (io.ReadCloser, error) {
	m.lastKey = name
	return nil, nil
}

func (m *mockBucket) GetRange(_ context.Context, name string, _, _ int64) (io.ReadCloser, error) {
	m.lastKey = name
	return nil, nil
}

func (m *mockBucket) Exists(_ context.Context, name string) (bool, error) {
	m.lastKey = name
	return false, nil
}

func (m *mockBucket) Attributes(_ context.Context, name string) (objstore.ObjectAttributes, error) {
	m.lastKey = name
	return objstore.ObjectAttributes{}, nil
}

func (m *mockBucket) Upload(_ context.Context, name string, _ io.Reader) error {
	m.lastKey = name
	return nil
}

func (m *mockBucket) Delete(_ context.Context, name string) error {
	m.lastKey = name
	return nil
}

func (m *mockBucket) Iter(_ context.Context, _ string, f func(string) error, _ ...objstore.IterOption) error {
	for _, key := range m.iterKeys {
		if err := f(key); err != nil {
			return err
		}
	}
	return nil
}

func TestKeyRewriteBucket(t *testing.T) {
	mock := &mockBucket{}
	bucket := &keyRewriteBucket{
		Bucket:    mock,
		delimiter: "-",
	}

	tests := []struct {
		name     string
		input    string
		expected string
		fn       func(string) error
	}{
		{
			name:     "Get replaces colons",
			input:    "foo:bar:baz",
			expected: "foo-bar-baz",
			fn: func(key string) error {
				_, err := bucket.Get(context.Background(), key)
				return err
			},
		},
		{
			name:     "GetRange replaces colons",
			input:    "foo:bar:baz",
			expected: "foo-bar-baz",
			fn: func(key string) error {
				_, err := bucket.GetRange(context.Background(), key, 0, 10)
				return err
			},
		},
		{
			name:     "Exists replaces colons",
			input:    "foo:bar:baz",
			expected: "foo-bar-baz",
			fn: func(key string) error {
				_, err := bucket.Exists(context.Background(), key)
				return err
			},
		},
		{
			name:     "Attributes replaces colons",
			input:    "foo:bar:baz",
			expected: "foo-bar-baz",
			fn: func(key string) error {
				_, err := bucket.Attributes(context.Background(), key)
				return err
			},
		},
		{
			name:     "Upload replaces colons",
			input:    "foo:bar:baz",
			expected: "foo-bar-baz",
			fn: func(key string) error {
				return bucket.Upload(context.Background(), key, strings.NewReader("test"))
			},
		},
		{
			name:     "Delete replaces colons",
			input:    "foo:bar:baz",
			expected: "foo-bar-baz",
			fn: func(key string) error {
				return bucket.Delete(context.Background(), key)
			},
		},
		{
			name:     "No colons remains unchanged",
			input:    "foo/bar/baz",
			expected: "foo/bar/baz",
			fn: func(key string) error {
				return bucket.Delete(context.Background(), key)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.expected, mock.lastKey)
		})
	}
}

func TestKeyRewriteBucket_Iter(t *testing.T) {
	iterKeys := []string{"foo:bar:baz", "foo-bar-qux", "foo-bar-quux"}
	mock := &mockBucket{
		iterKeys: iterKeys,
	}
	bucket := &keyRewriteBucket{
		Bucket:    mock,
		delimiter: "-",
	}

	var gotKeys []string
	// keyRewriteBucket transparently returns the keys in the storage
	err := bucket.Iter(context.Background(), "", func(name string) error {
		gotKeys = append(gotKeys, name)
		return nil
	})

	require.NoError(t, err)
	require.EqualValues(t, iterKeys, gotKeys)
}
