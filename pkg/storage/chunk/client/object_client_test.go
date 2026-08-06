package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

func MustParseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}

func TestFSEncoder(t *testing.T) {
	schema := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:   MustParseDayTime("2020-01-01"),
				Schema: "v11",
			},
			{
				From:   MustParseDayTime("2022-01-01"),
				Schema: "v12",
			},
		},
	}

	// chunk that resolves to v11
	oldChunk := chunk.Chunk{
		ChunkRef: logproto.ChunkRef{
			UserID:      "fake",
			From:        MustParseDayTime("2020-01-02").Time,
			Through:     MustParseDayTime("2020-01-03").Time,
			Fingerprint: uint64(456),
			Checksum:    123,
		},
	}

	// chunk that resolves to v12
	newChunk := chunk.Chunk{
		ChunkRef: logproto.ChunkRef{
			UserID:      "fake",
			From:        MustParseDayTime("2022-01-02").Time,
			Through:     MustParseDayTime("2022-01-03").Time,
			Fingerprint: uint64(456),
			Checksum:    123,
		},
	}

	for _, tc := range []struct {
		desc string
		from string
		exp  string
	}{
		{
			desc: "before v12 encodes entire chunk",
			from: schema.ExternalKey(oldChunk.ChunkRef),
			exp:  "ZmFrZS8xYzg6MTZmNjM4ZDQ0MDA6MTZmNjhiM2EwMDA6N2I=",
		},
		{
			desc: "v12+ encodes encodes the non-directory trail",
			from: schema.ExternalKey(newChunk.ChunkRef),
			exp:  "fake/1c8/MTdlMTgxNWY4MDA6MTdlMWQzYzU0MDA6N2I=",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			chk, err := chunk.ParseExternalKey("fake", tc.from)
			require.Nil(t, err)
			require.Equal(t, tc.exp, FSEncoder(schema, chk))
		})
	}
}

func TestGetChunkDecodeError(t *testing.T) {
	schema := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:   MustParseDayTime("2020-01-01"),
				Schema: "v11",
			},
		},
	}

	chk := chunk.Chunk{
		ChunkRef: logproto.ChunkRef{
			UserID:      "fake",
			From:        MustParseDayTime("2020-01-02").Time,
			Through:     MustParseDayTime("2020-01-03").Time,
			Fingerprint: uint64(456),
			Checksum:    123,
		},
	}
	key := schema.ExternalKey(chk.ChunkRef)

	store := newStubObjectClient()
	require.NoError(t, store.PutObject(context.Background(), key, bytes.NewReader([]byte("not a chunk"))))

	c := NewClientWithMaxParallel(store, nil, 1, schema)
	_, err := c.GetChunks(context.Background(), []chunk.Chunk{chk})
	require.Error(t, err)

	require.ErrorIs(t, err, ErrChunkDecodeFailed)
	require.ErrorIs(t, err, chunk.ErrInvalidChecksum)

	// Spelled out rather than built from the same format string, so that a later
	// rewrite of getChunk cannot change the message and the test at once.
	require.Equal(t,
		fmt.Sprintf("failed to decode chunk '%s' for tenant `fake`: invalid chunk checksum", key),
		err.Error(),
	)
}

// stubObjectClient is a minimal in-memory object store. The testutils package
// imports this one, so its client cannot be used here.
type stubObjectClient struct {
	objects map[string][]byte
}

func newStubObjectClient() *stubObjectClient {
	return &stubObjectClient{objects: map[string][]byte{}}
}

func (s *stubObjectClient) PutObject(_ context.Context, key string, object io.Reader) error {
	buf, err := io.ReadAll(object)
	if err != nil {
		return err
	}
	s.objects[key] = buf
	return nil
}

func (s *stubObjectClient) GetObject(_ context.Context, key string) (io.ReadCloser, int64, error) {
	buf, ok := s.objects[key]
	if !ok {
		return nil, 0, ErrStorageObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(buf)), int64(len(buf)), nil
}

func (s *stubObjectClient) ObjectExists(_ context.Context, key string) (bool, error) {
	_, ok := s.objects[key]
	return ok, nil
}

func (s *stubObjectClient) GetAttributes(_ context.Context, key string) (ObjectAttributes, error) {
	buf, ok := s.objects[key]
	if !ok {
		return ObjectAttributes{}, ErrStorageObjectNotFound
	}
	return ObjectAttributes{Size: int64(len(buf))}, nil
}

func (s *stubObjectClient) GetObjectRange(_ context.Context, _ string, _, _ int64) (io.ReadCloser, error) {
	return nil, ErrMethodNotImplemented
}

func (s *stubObjectClient) List(_ context.Context, _ string, _ string) ([]StorageObject, []StorageCommonPrefix, error) {
	return nil, nil, ErrMethodNotImplemented
}

func (s *stubObjectClient) DeleteObject(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

func (s *stubObjectClient) IsObjectNotFoundErr(err error) bool {
	return errors.Is(err, ErrStorageObjectNotFound)
}

func (s *stubObjectClient) IsRetryableErr(error) bool { return false }

func (s *stubObjectClient) Stop() {}
