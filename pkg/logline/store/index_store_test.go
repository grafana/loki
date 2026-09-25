package store

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

// newTestStore creates a Store with short poll interval and standard retention for testing.
func newTestStore(t *testing.T, bucket objstore.Bucket) *Store {
	t.Helper()
	cfg := Config{
		PollInterval:          10 * time.Millisecond,
		RetentionDuration:     7 * 24 * time.Hour,
		CompactionGracePeriod: 24 * time.Hour,
		MinDate:               "0001-01-01",
	}
	s, err := NewStore(bucket, cfg, log.NewNopLogger(), prometheus.NewPedanticRegistry())
	require.NoError(t, err)
	return s
}

// writeTestIndex is a helper to write an index with test data to a Store.
// It defaults MinRecordTs/MaxRecordTs from the log timestamps if unset.
func writeTestIndex(t *testing.T, s *Store, meta Meta, data string) {
	t.Helper()
	if meta.MinRecordTs.IsZero() {
		meta.MinRecordTs = meta.MinLogTs
	}
	if meta.MaxRecordTs.IsZero() {
		meta.MaxRecordTs = meta.MaxLogTs
	}
	if meta.IndexHeader == nil {
		meta.IndexHeader = &format.HeaderInfo{}
	}
	if meta.SizeBytes == 0 {
		meta.SizeBytes = int64(len(data))
	}
	err := s.PutIndex(context.Background(), strings.NewReader(data), meta)
	require.NoError(t, err)
}

// ---- Meta path tests ----

func TestMeta_Path(t *testing.T) {
	m := Meta{Date: "2026-02-23", Hash: "abc123"}
	require.Equal(t, "2026-02-23/abc123/index", m.IndexPath())
	require.Equal(t, "2026-02-23/abc123/meta.json", m.MetaPath())
}

func TestMeta_Path_UsesStorageIDWhenSet(t *testing.T) {
	m := Meta{Date: "2026-02-23", StorageID: "builder-123", Hash: "abc123"}
	require.Equal(t, "2026-02-23/builder-123/index", m.IndexPath())
	require.Equal(t, "2026-02-23/builder-123/meta.json", m.MetaPath())
}

func TestMeta_ID(t *testing.T) {
	m := Meta{Date: "2026-02-23", Hash: "abc123"}
	require.Equal(t, "2026-02-23/abc123", m.ID())
}

func TestMeta_ID_UsesStorageIDWhenSet(t *testing.T) {
	m := Meta{Date: "2026-02-23", StorageID: "worker-456", Hash: "abc123"}
	require.Equal(t, "2026-02-23/worker-456", m.ID())
}

// ---- Meta JSON tests ----

func TestMeta_JSON_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	src := []string{"2026-02-22/aaa", "2026-02-22/bbb"}
	original := Meta{
		Date:          "2026-02-23",
		Hash:          "roundtrip",
		Version:       "v3",
		MinLogTs:      now.Add(-1 * time.Hour),
		MaxLogTs:      now,
		CompactedFrom: src,
		CreatedAt:     now,
		SizeBytes:     42 * 1024 * 1024,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Meta
	require.NoError(t, json.Unmarshal(data, &decoded))

	require.Equal(t, original.Date, decoded.Date)
	require.Equal(t, original.Hash, decoded.Hash)
	require.Equal(t, original.Version, decoded.Version)
	require.True(t, original.MinLogTs.Equal(decoded.MinLogTs), "MinLogTs mismatch")
	require.True(t, original.MaxLogTs.Equal(decoded.MaxLogTs), "MaxLogTs mismatch")
	require.True(t, original.CreatedAt.Equal(decoded.CreatedAt), "CreatedAt mismatch")
	require.Equal(t, original.CompactedFrom, decoded.CompactedFrom)
	require.Equal(t, original.SizeBytes, decoded.SizeBytes)
}

func TestMeta_JSON_NilCompactedFrom(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	original := Meta{
		Date:          "2026-02-23",
		Hash:          "nilcf",
		Version:       "v3",
		MinLogTs:      now.Add(-1 * time.Hour),
		MaxLogTs:      now,
		CompactedFrom: nil,
		CreatedAt:     now,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	// compacted_from should be absent due to omitempty
	require.NotContains(t, string(data), "compacted_from")

	var decoded Meta
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Nil(t, decoded.CompactedFrom)
}

func TestMeta_JSON_SerializesStorageIDAsID(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	meta := Meta{
		Date:        "2026-02-23",
		StorageID:   "builder-123",
		Hash:        "deadbeef",
		Version:     "v3",
		MinLogTs:    now.Add(-1 * time.Hour),
		MaxLogTs:    now,
		MinRecordTs: now.Add(-1 * time.Hour),
		MaxRecordTs: now,
		CreatedAt:   now,
	}

	data, err := json.Marshal(meta)
	require.NoError(t, err)
	require.Contains(t, string(data), `"id":"builder-123"`)
	require.NotContains(t, string(data), "storage_id")
}

func TestMeta_JSON_Tags(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	meta := Meta{
		Date:      "2026-02-23",
		Hash:      "tagstest",
		Version:   "v3",
		MinLogTs:  now.Add(-1 * time.Hour),
		MaxLogTs:  now,
		CreatedAt: now,
	}

	data, err := json.Marshal(meta)
	require.NoError(t, err)

	jsonStr := string(data)
	require.Contains(t, jsonStr, `"version"`)
	require.Contains(t, jsonStr, `"min_log_ts"`)
	require.Contains(t, jsonStr, `"max_log_ts"`)
	require.Contains(t, jsonStr, `"created_at"`)
	require.Contains(t, jsonStr, `"date"`)
	require.Contains(t, jsonStr, `"hash"`)

	// Verify camelCase is NOT used
	require.NotContains(t, jsonStr, `"minTime"`)
	require.NotContains(t, jsonStr, `"maxTime"`)
	require.NotContains(t, jsonStr, `"createdAt"`)

	// Verify compacted_from uses snake_case when present
	metaWithCompacted := Meta{
		Date:          "2026-02-23",
		Hash:          "tagstest2",
		Version:       "v3",
		MinLogTs:      now.Add(-1 * time.Hour),
		MaxLogTs:      now,
		CreatedAt:     now,
		CompactedFrom: []string{"2026-02-22/src1"},
	}
	dataWithCompacted, err := json.Marshal(metaWithCompacted)
	require.NoError(t, err)
	jsonWithCompacted := string(dataWithCompacted)
	require.Contains(t, jsonWithCompacted, `"compacted_from"`)
	require.NotContains(t, jsonWithCompacted, `"compactedFrom"`)
}

func TestMeta_JSON_IndexHeader_RoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	original := Meta{
		Date:        "2026-02-23",
		Hash:        "headtest",
		Version:     "v3",
		MinLogTs:    now.Add(-1 * time.Hour),
		MaxLogTs:    now,
		MinRecordTs: now.Add(-1 * time.Hour),
		MaxRecordTs: now,
		CreatedAt:   now,
		IndexHeader: &format.HeaderInfo{
			Version:              2,
			Flags:                3,
			DocumentCount:        100,
			TermBlockCount:       10,
			PostingsBlockCount:   5,
			PostingsCompression:  1,
			TermCount:            5000,
			PostingsDataSize:     1024000,
			TermDataSize:         256000,
			DocMetadataSize:      2000,
			TermBlockDirSize:     260,
			PostingsBlockDirSize: 100,
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	jsonStr := string(data)
	require.Contains(t, jsonStr, `"index_header"`)
	require.Contains(t, jsonStr, `"document_count"`)
	require.Contains(t, jsonStr, `"term_count"`)
	require.Contains(t, jsonStr, `"postings_data_size"`)

	var decoded Meta
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.NotNil(t, decoded.IndexHeader)
	require.Equal(t, original.IndexHeader, decoded.IndexHeader)
}

func TestMeta_JSON_NilIndexHeader(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	original := Meta{
		Date:        "2026-02-23",
		Hash:        "noheader",
		Version:     "v3",
		MinLogTs:    now.Add(-1 * time.Hour),
		MaxLogTs:    now,
		MinRecordTs: now.Add(-1 * time.Hour),
		MaxRecordTs: now,
		CreatedAt:   now,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Contains(t, string(data), `"index_header":null`)

	var decoded Meta
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Nil(t, decoded.IndexHeader)
}

// ---- Config tests ----

func TestConfig_Validate_Defaults(t *testing.T) {
	cfg := Config{MinDate: "2025-01-01"}
	require.NoError(t, cfg.Validate())
	require.Equal(t, DefaultPollInterval, cfg.PollInterval)
	require.Equal(t, DefaultPollConcurrency, cfg.PollConcurrency)
	require.Equal(t, DefaultRetentionDuration, cfg.RetentionDuration)
	require.Equal(t, DefaultCompactionGracePeriod, cfg.CompactionGracePeriod)
}

func TestConfig_Validate_NegativePollInterval(t *testing.T) {
	cfg := Config{PollInterval: -1 * time.Second}
	require.Error(t, cfg.Validate())
}

func TestConfig_Validate_NegativePollConcurrency(t *testing.T) {
	cfg := Config{PollConcurrency: -1}
	require.Error(t, cfg.Validate())
}

func TestConfig_Validate_NegativeRetention(t *testing.T) {
	cfg := Config{RetentionDuration: -1 * time.Hour}
	require.Error(t, cfg.Validate())
}

func TestConfig_Validate_NegativeGracePeriod(t *testing.T) {
	cfg := Config{CompactionGracePeriod: -1 * time.Hour}
	require.Error(t, cfg.Validate())
}

func TestConfig_Validate_MinDate_Valid(t *testing.T) {
	cfg := Config{MinDate: "2026-04-08"}
	require.NoError(t, cfg.Validate())
	require.Equal(t, "2026-04-08", cfg.MinDate)
}

func TestConfig_Validate_MinDate_EmptyRejected(t *testing.T) {
	cfg := Config{MinDate: ""}
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "min_date is required")
}

func TestConfig_Validate_MinDate_InvalidFormat(t *testing.T) {
	cfg := Config{MinDate: "2026-4-8"}
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "min_date must be YYYY-MM-DD")
}

func TestConfig_RegisterFlags_Smoke(_ *testing.T) {
	cfg := Config{}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	// should not panic
	cfg.RegisterFlags(fs)
}

func TestConfig_RegisterFlags_NilFlagSet(t *testing.T) {
	cfg := Config{}
	// CommandLine is process-wide; a fresh FlagSet keeps -count>1 from
	// panicking with "flag redefined".
	require.NotPanics(t, func() {
		cfg.RegisterFlags(flag.NewFlagSet(t.Name(), flag.ContinueOnError))
	})
}

// ---- NewStore tests ----

func TestNewStore_NilBucket(t *testing.T) {
	_, err := NewStore(nil, Config{}, log.NewNopLogger(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bucket cannot be nil")
}

func TestNewStore_InvalidConfig(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	_, err := NewStore(bucket, Config{PollInterval: -1 * time.Second}, log.NewNopLogger(), nil)
	require.Error(t, err)
}

func TestNewStore_NilLogger(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	// nil logger should be accepted and replaced with a NopLogger internally
	s, err := NewStore(bucket, Config{MinDate: "2025-01-01"}, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, s)

	// Verify the store is functional — Poll must not panic on a nil logger
	require.NotPanics(t, func() {
		_ = s.poll(context.Background())
	})
}

// ---- Store.Write tests ----

func TestStore_Write_CreatesIndexAndMeta(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	meta := Meta{
		Date:        "2026-02-23",
		Hash:        "deadbeef",
		Version:     "v3",
		MinLogTs:    now.Add(-1 * time.Hour),
		MaxLogTs:    now,
		MinRecordTs: now.Add(-1 * time.Hour),
		MaxRecordTs: now,
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   int64(len("index data")),
	}

	err := s.PutIndex(context.Background(), strings.NewReader("index data"), meta)
	require.NoError(t, err)

	exists, err := bucket.Exists(context.Background(), meta.IndexPath())
	require.NoError(t, err)
	require.True(t, exists, "index file should exist")

	exists, err = bucket.Exists(context.Background(), meta.MetaPath())
	require.NoError(t, err)
	require.True(t, exists, "meta.json should exist")
}

func TestStore_Write_MetaContents(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC().Truncate(time.Second)
	compactedFrom := []string{"2026-02-22/src1"}
	meta := Meta{
		Date:          "2026-02-23",
		Hash:          "cafebabe",
		Version:       "v3",
		MinLogTs:      now.Add(-2 * time.Hour),
		MaxLogTs:      now,
		CompactedFrom: compactedFrom,
	}

	writeTestIndex(t, s, meta, "some index content")

	// Download and decode meta.json
	rc, err := bucket.Get(context.Background(), meta.MetaPath())
	require.NoError(t, err)
	defer rc.Close()

	var decoded Meta
	require.NoError(t, json.NewDecoder(rc).Decode(&decoded))

	require.True(t, meta.MinLogTs.Equal(decoded.MinLogTs), "MinLogTs mismatch")
	require.True(t, meta.MaxLogTs.Equal(decoded.MaxLogTs), "MaxLogTs mismatch")
	require.Equal(t, compactedFrom, decoded.CompactedFrom)
	require.Equal(t, "2026-02-23", decoded.Date)
	require.Equal(t, "cafebabe", decoded.Hash)
	// CreatedAt is set by Write — it should be non-zero and recent
	require.False(t, decoded.CreatedAt.IsZero(), "CreatedAt should be set by Write")
	require.WithinDuration(t, time.Now().UTC(), decoded.CreatedAt, 5*time.Second)
}

func TestStore_Write_IndexContents(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	indexData := "this is the index content bytes"
	meta := Meta{
		Date:        "2026-02-23",
		Hash:        "feedface",
		Version:     "v3",
		MinLogTs:    now.Add(-1 * time.Hour),
		MaxLogTs:    now,
		MinRecordTs: now.Add(-1 * time.Hour),
		MaxRecordTs: now,
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   int64(len(indexData)),
	}

	err := s.PutIndex(context.Background(), strings.NewReader(indexData), meta)
	require.NoError(t, err)

	rc, err := bucket.Get(context.Background(), meta.IndexPath())
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, indexData, string(data))
}

func TestStore_Write_ContextCancel(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	now := time.Now().UTC()
	err := s.PutIndex(ctx, strings.NewReader("data"), Meta{
		Date:        "2026-02-23",
		Hash:        "cancelled",
		Version:     "v3",
		MinLogTs:    now.Add(-1 * time.Hour),
		MaxLogTs:    now,
		MinRecordTs: now.Add(-1 * time.Hour),
		MaxRecordTs: now,
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   1,
	})
	require.Error(t, err)
}

func TestStore_Write_EmptyDateHash(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)
	now := time.Now().UTC()

	// Empty Date
	err := s.PutIndex(context.Background(), strings.NewReader("data"), Meta{
		Date: "", Hash: "abc123", Version: "v3",
		MinLogTs: now.Add(-time.Hour), MaxLogTs: now,
		MinRecordTs: now.Add(-time.Hour), MaxRecordTs: now,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "date")

	// Empty Hash
	err = s.PutIndex(context.Background(), strings.NewReader("data"), Meta{
		Date: "2026-02-23", Hash: "", Version: "v3",
		MinLogTs: now.Add(-time.Hour), MaxLogTs: now,
		MinRecordTs: now.Add(-time.Hour), MaxRecordTs: now,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "hash")

	// Both empty
	err = s.PutIndex(context.Background(), strings.NewReader("data"), Meta{
		Version: "v3", MinLogTs: now.Add(-time.Hour), MaxLogTs: now,
		MinRecordTs: now.Add(-time.Hour), MaxRecordTs: now,
	})
	require.Error(t, err)
}

func TestStore_Write_ValidatesRequiredFields(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)
	now := time.Now().UTC()

	// Positive case: all required fields set — must succeed.
	err := s.PutIndex(context.Background(), strings.NewReader("data"), Meta{
		Date: "2026-02-23", Hash: "abc123", Version: "v3",
		MinLogTs: now.Add(-time.Hour), MaxLogTs: now,
		MinRecordTs: now.Add(-time.Hour), MaxRecordTs: now,
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   int64(len("data")),
	})
	require.NoError(t, err)

	// Nil IndexHeader must be rejected.
	err = s.PutIndex(context.Background(), strings.NewReader("data"), Meta{
		Date: "2026-02-23", Hash: "abc124", Version: "v3",
		MinLogTs: now.Add(-time.Hour), MaxLogTs: now,
		MinRecordTs: now.Add(-time.Hour), MaxRecordTs: now,
		IndexHeader: nil,
		SizeBytes:   int64(len("data")),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "index_header")

	// Zero SizeBytes must be rejected.
	err = s.PutIndex(context.Background(), strings.NewReader("data"), Meta{
		Date: "2026-02-23", Hash: "abc125", Version: "v3",
		MinLogTs: now.Add(-time.Hour), MaxLogTs: now,
		MinRecordTs: now.Add(-time.Hour), MaxRecordTs: now,
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   0,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "size_bytes")
}

// ---- PutIndexStreaming tests ----

func TestStore_PutIndexStreaming_HappyPath(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC().Truncate(time.Second)
	meta := Meta{
		Date:        "2026-02-23",
		StorageID:   "streaming-happy",
		Version:     "v3",
		MinLogTs:    now.Add(-1 * time.Hour),
		MaxLogTs:    now,
		MinRecordTs: now.Add(-1 * time.Hour),
		MaxRecordTs: now,
	}

	payload := []byte("hello streaming upload world")
	err := s.PutIndexStreaming(context.Background(), &meta, func(w io.Writer) (format.HeaderInfo, error) {
		// Split across two writes to exercise the tee path.
		if _, err := w.Write(payload[:10]); err != nil {
			return format.HeaderInfo{}, err
		}
		if _, err := w.Write(payload[10:]); err != nil {
			return format.HeaderInfo{}, err
		}
		// Post-write HeaderInfo (typically from a writer's Info() or a merger's
		// return value) is flowed into meta.IndexHeader by the store.
		return format.HeaderInfo{DocumentCount: 42}, nil
	})
	require.NoError(t, err)

	// Hash and SizeBytes must be populated from the stream.
	require.Equal(t, int64(len(payload)), meta.SizeBytes)
	expectedHash, err := computeIndexHash(bytes.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, expectedHash, meta.Hash)

	// IndexHeader must be populated from the returned HeaderInfo.
	require.NotNil(t, meta.IndexHeader)
	require.Equal(t, uint32(42), meta.IndexHeader.DocumentCount)

	// CreatedAt must be populated.
	require.False(t, meta.CreatedAt.IsZero())

	// Both objects must be present with the expected bytes.
	indexReader, err := bucket.Get(context.Background(), meta.IndexPath())
	require.NoError(t, err)
	defer indexReader.Close()
	gotPayload, err := io.ReadAll(indexReader)
	require.NoError(t, err)
	require.Equal(t, payload, gotPayload)

	metaReader, err := bucket.Get(context.Background(), meta.MetaPath())
	require.NoError(t, err)
	defer metaReader.Close()
	var roundTripped Meta
	require.NoError(t, json.NewDecoder(metaReader).Decode(&roundTripped))
	require.Equal(t, meta.Hash, roundTripped.Hash)
	require.Equal(t, meta.SizeBytes, roundTripped.SizeBytes)
	require.Equal(t, uint32(42), roundTripped.IndexHeader.DocumentCount)
}

func TestStore_PutIndexStreaming_WriteErrorSkipsMeta(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC().Truncate(time.Second)
	meta := Meta{
		Date:        "2026-02-23",
		StorageID:   "streaming-fail",
		Version:     "v3",
		MinLogTs:    now.Add(-1 * time.Hour),
		MaxLogTs:    now,
		MinRecordTs: now.Add(-1 * time.Hour),
		MaxRecordTs: now,
	}

	writeErr := fmt.Errorf("producer blew up")
	err := s.PutIndexStreaming(context.Background(), &meta, func(w io.Writer) (format.HeaderInfo, error) {
		_, _ = w.Write([]byte("partial"))
		return format.HeaderInfo{}, writeErr
	})
	require.ErrorIs(t, err, writeErr)

	// meta.json must not have been uploaded — it's the commit marker.
	exists, err := bucket.Exists(context.Background(), meta.MetaPath())
	require.NoError(t, err)
	require.False(t, exists, "meta.json must not be written when the producer fails")
}

// TestStore_PutIndexStreaming_UploadEarlyReturnDoesNotHang verifies that when
// the bucket's Upload reads a few bytes and then returns an error without
// draining the reader, PutIndexStreaming returns promptly instead of parking
// the producer goroutine on pw.Write forever.
//
// Regression: see ISSUE.md (worker io.Pipe deadlock). The upload goroutine
// must close the read end of the pipe on any return path so the writer side
// is unblocked.
func TestStore_PutIndexStreaming_UploadEarlyReturnDoesNotHang(t *testing.T) {
	t.Parallel()
	deadline := time.Now().Add(5 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	uploadErr := fmt.Errorf("injected upload short-read")
	bucket := &shortReadUploadBucket{
		Bucket:   objstore.NewInMemBucket(),
		readN:    8,
		returnOn: "/index",
		err:      uploadErr,
	}
	s := newTestStore(t, bucket)

	now := time.Now().UTC().Truncate(time.Second)
	meta := Meta{
		Date:        "2026-04-30",
		StorageID:   "streaming-early-return",
		Version:     "v3",
		MinLogTs:    now.Add(-1 * time.Hour),
		MaxLogTs:    now,
		MinRecordTs: now.Add(-1 * time.Hour),
		MaxRecordTs: now,
	}

	// 1 MiB payload: large enough that the producer must block on a pipe
	// write after the bucket has stopped reading.
	payload := make([]byte, 1<<20)
	for i := range payload {
		payload[i] = byte(i)
	}

	done := make(chan error, 1)
	go func() {
		done <- s.PutIndexStreaming(ctx, &meta, func(w io.Writer) (format.HeaderInfo, error) {
			_, err := w.Write(payload)
			return format.HeaderInfo{}, err
		})
	}()

	select {
	case err := <-done:
		require.Error(t, err, "PutIndexStreaming must propagate the upload error")
		require.ErrorIs(t, err, uploadErr)
	case <-time.After(time.Until(deadline)):
		t.Fatal("PutIndexStreaming hung after Upload returned early — pipe writer was not unblocked")
	}

	// meta.json must not be written when the index upload failed.
	exists, err := bucket.Exists(context.Background(), meta.MetaPath())
	require.NoError(t, err)
	require.False(t, exists, "meta.json must not be written when the upload fails")
}

// TestStore_PutIndexStreaming_ContextCancelDoesNotHang verifies that when the
// caller cancels the context mid-upload, PutIndexStreaming returns promptly
// rather than wedging the producer.
func TestStore_PutIndexStreaming_ContextCancelDoesNotHang(t *testing.T) {
	t.Parallel()
	deadline := time.Now().Add(5 * time.Second)
	testCtx, testCancel := context.WithDeadline(context.Background(), deadline)
	defer testCancel()

	bucket := &shortReadUploadBucket{
		Bucket:   objstore.NewInMemBucket(),
		readN:    8,
		returnOn: "/index",
		err:      context.Canceled,
	}
	s := newTestStore(t, bucket)

	now := time.Now().UTC().Truncate(time.Second)
	meta := Meta{
		Date:        "2026-04-30",
		StorageID:   "streaming-ctx-cancel",
		Version:     "v3",
		MinLogTs:    now.Add(-1 * time.Hour),
		MaxLogTs:    now,
		MinRecordTs: now.Add(-1 * time.Hour),
		MaxRecordTs: now,
	}

	payload := make([]byte, 1<<20)

	ctx, cancel := context.WithCancel(testCtx)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- s.PutIndexStreaming(ctx, &meta, func(w io.Writer) (format.HeaderInfo, error) {
			_, err := w.Write(payload)
			return format.HeaderInfo{}, err
		})
	}()

	// Give the producer a moment to push some bytes into the pipe so the
	// bucket has actually started consuming, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.Error(t, err, "PutIndexStreaming must return after context cancel")
	case <-time.After(time.Until(deadline)):
		t.Fatal("PutIndexStreaming hung after context cancel — pipe writer was not unblocked")
	}
}

// shortReadUploadBucket wraps an objstore.Bucket. For uploads whose name has
// the configured suffix it reads readN bytes from the reader and returns err
// without draining the rest — simulating an Upload that exits early (mid-flight
// retry, network error, context cancel) and abandons its io.Reader.
type shortReadUploadBucket struct {
	objstore.Bucket
	readN    int
	returnOn string
	err      error
}

func (b *shortReadUploadBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	if strings.HasSuffix(name, b.returnOn) {
		buf := make([]byte, b.readN)
		_, _ = io.ReadFull(r, buf)
		return b.err
	}
	return b.Bucket.Upload(ctx, name, r)
}

// ---- Store.Delete tests ----

func TestStore_Delete_RemovesBothFiles(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	meta := Meta{
		Date: "2026-02-23", Hash: "todelete", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}
	writeTestIndex(t, s, meta, "index content")

	// Confirm both files exist before delete
	exists, err := bucket.Exists(context.Background(), meta.IndexPath())
	require.NoError(t, err)
	require.True(t, exists)
	exists, err = bucket.Exists(context.Background(), meta.MetaPath())
	require.NoError(t, err)
	require.True(t, exists)

	// Delete (no CompactedFrom, so no protection check)
	require.NoError(t, s.DeleteIndex(context.Background(), meta))

	// Both files should be gone
	exists, err = bucket.Exists(context.Background(), meta.IndexPath())
	require.NoError(t, err)
	require.False(t, exists, "index file should be deleted")
	exists, err = bucket.Exists(context.Background(), meta.MetaPath())
	require.NoError(t, err)
	require.False(t, exists, "meta.json should be deleted")
}

func TestStore_Delete_NotFoundIsIdempotent(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	// Delete a ref that never existed — should not error
	meta := Meta{Date: "2026-02-23", Hash: "nonexistent"}
	require.NoError(t, s.DeleteIndex(context.Background(), meta))
}

func TestStore_Delete_MetaDeletedFirst(t *testing.T) {
	// Verify that after Delete, Poll no longer sees the index.
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	meta := Meta{
		Date: "2026-02-23", Hash: "pollcheck", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}
	writeTestIndex(t, s, meta, "content")

	require.NoError(t, s.poll(context.Background()))
	require.Len(t, s.IndexesForRange(now.Add(-2*time.Hour), now.Add(time.Hour)), 1)

	require.NoError(t, s.DeleteIndex(context.Background(), meta))
	require.NoError(t, s.poll(context.Background()))
	require.Empty(t, s.IndexesForRange(now.Add(-2*time.Hour), now.Add(time.Hour)),
		"deleted index should not appear after re-poll")
}

func TestStore_Delete_RefusesToDeleteMergedWithLiveSources(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	source := Meta{
		Date: "2026-02-23", Hash: "source", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now.Add(-time.Hour),
	}
	merged := Meta{
		Date: "2026-02-23", Hash: "merged", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now,
		CompactedFrom: []string{source.ID()},
	}

	writeTestIndex(t, s, source, "source data")
	writeTestIndex(t, s, merged, "merged data")
	require.NoError(t, s.poll(context.Background()))

	// Attempting to delete merged should fail — source still exists
	err := s.DeleteIndex(context.Background(), merged)
	require.Error(t, err)
	require.Contains(t, err.Error(), "compacted source")
	require.Contains(t, err.Error(), source.ID())
}

func TestStore_Delete_AllowsMergedAfterSourcesDeleted(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	source := Meta{
		Date: "2026-02-23", Hash: "source", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now.Add(-time.Hour),
	}
	merged := Meta{
		Date: "2026-02-23", Hash: "merged", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now,
		CompactedFrom: []string{source.ID()},
	}

	writeTestIndex(t, s, source, "source data")
	writeTestIndex(t, s, merged, "merged data")
	require.NoError(t, s.poll(context.Background()))

	// Delete source first (leaf index, no protection check)
	require.NoError(t, s.DeleteIndex(context.Background(), source))
	require.NoError(t, s.poll(context.Background()))

	// Now deleting merged should succeed — source no longer in snapshot
	require.NoError(t, s.DeleteIndex(context.Background(), merged))
}

// ---- Store.Poll tests ----

func TestStore_Poll_EmptyBucket(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	require.NoError(t, s.poll(context.Background()))

	result := s.IndexesForRange(time.Now().Add(-24*time.Hour), time.Now())
	require.Empty(t, result)
}

func TestStore_Poll_DiscoversSingleIndex(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	meta := Meta{
		Date: "2026-02-23", Hash: "singleidx", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}
	writeTestIndex(t, s, meta, "index content")

	require.NoError(t, s.poll(context.Background()))

	result := s.IndexesForRange(now.Add(-2*time.Hour), now.Add(time.Hour))
	require.Len(t, result, 1)
	require.Equal(t, meta.ID(), result[0].ID())
}

func TestStore_Poll_DiscoversMultipleIndexes(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	metas := []Meta{
		{Date: "2026-02-23", Hash: "idx1", Version: "v3", MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now},
		{Date: "2026-02-23", Hash: "idx2", Version: "v3", MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now},
		{Date: "2026-02-22", Hash: "idx3", Version: "v3", MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now},
	}
	for _, m := range metas {
		writeTestIndex(t, s, m, "content")
	}

	require.NoError(t, s.poll(context.Background()))

	result := s.IndexesForRange(now.Add(-2*time.Hour), now.Add(time.Hour))
	require.Len(t, result, 3)
}

func TestStore_Poll_ExcludesCompacted(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	metaA := Meta{
		Date: "2026-02-23", Hash: "source", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now.Add(-1 * time.Hour),
	}
	metaB := Meta{
		Date: "2026-02-23", Hash: "merged", Version: "v3",
		MinLogTs:      now.Add(-2 * time.Hour),
		MaxLogTs:      now,
		CompactedFrom: []string{metaA.ID()},
	}

	writeTestIndex(t, s, metaA, "source index")
	writeTestIndex(t, s, metaB, "merged index")

	require.NoError(t, s.poll(context.Background()))

	result := s.IndexesForRange(now.Add(-3*time.Hour), now.Add(time.Hour))

	// Only the merged index (B) should be in the active list
	require.Len(t, result, 1)
	require.Equal(t, metaB.ID(), result[0].ID())
}

func TestStore_Poll_SnapshotIsAtomic(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	meta1 := Meta{
		Date: "2026-02-23", Hash: "first", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}
	writeTestIndex(t, s, meta1, "first index")

	require.NoError(t, s.poll(context.Background()))
	result1 := s.IndexesForRange(now.Add(-2*time.Hour), now.Add(time.Hour))
	require.Len(t, result1, 1)

	// Add a second index
	meta2 := Meta{
		Date: "2026-02-23", Hash: "second", Version: "v3",
		MinLogTs: now.Add(-30 * time.Minute), MaxLogTs: now,
	}
	writeTestIndex(t, s, meta2, "second index")

	require.NoError(t, s.poll(context.Background()))
	result2 := s.IndexesForRange(now.Add(-2*time.Hour), now.Add(time.Hour))
	require.Len(t, result2, 2)
}

func TestStore_Poll_FailsOnMalformedMeta(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	// Manually write an invalid meta.json
	err := bucket.Upload(context.Background(), "2026-02-23/badhash/meta.json",
		bytes.NewReader([]byte("this is not valid JSON {")))
	require.NoError(t, err)

	// Poll should fail on malformed meta
	err = s.poll(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid meta")
}

func TestStore_Poll_SkipsIndexWithoutMeta(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	// Upload only the index file, no meta.json
	err := bucket.Upload(context.Background(), "2026-02-23/nometa/index",
		strings.NewReader("index data without meta"))
	require.NoError(t, err)

	// Poll should succeed and skip this entry
	require.NoError(t, s.poll(context.Background()))

	result := s.IndexesForRange(time.Now().Add(-24*time.Hour), time.Now())
	require.Empty(t, result)
}

// ---- Store.Indexes tests ----

func TestStore_Indexes_EmptyRange(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	// Write an index covering yesterday
	meta := Meta{
		Date: "2026-02-22", Hash: "yesterday", Version: "v3",
		MinLogTs: now.Add(-48 * time.Hour), MaxLogTs: now.Add(-24 * time.Hour),
	}
	writeTestIndex(t, s, meta, "yesterday index")

	require.NoError(t, s.poll(context.Background()))

	// Query only today
	result := s.IndexesForRange(now.Add(-1*time.Hour), now)
	require.Empty(t, result)
}

func TestStore_Indexes_ReturnsOverlapping(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()

	// Index 1: covers [now-4h, now-2h]
	meta1 := Meta{
		Date: "2026-02-23", Hash: "early", Version: "v3",
		MinLogTs: now.Add(-4 * time.Hour), MaxLogTs: now.Add(-2 * time.Hour),
	}
	writeTestIndex(t, s, meta1, "early")

	// Index 2: covers [now-2h, now]
	meta2 := Meta{
		Date: "2026-02-23", Hash: "recent", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now,
	}
	writeTestIndex(t, s, meta2, "recent")

	// Index 3: covers [now-10h, now-8h]
	meta3 := Meta{
		Date: "2026-02-23", Hash: "old", Version: "v3",
		MinLogTs: now.Add(-10 * time.Hour), MaxLogTs: now.Add(-8 * time.Hour),
	}
	writeTestIndex(t, s, meta3, "old")

	require.NoError(t, s.poll(context.Background()))

	// Query [now-3h, now-1h] — should match index1 and index2 (both overlap)
	result := s.IndexesForRange(now.Add(-3*time.Hour), now.Add(-1*time.Hour))
	require.Len(t, result, 2)

	idSet := make(map[string]bool)
	for _, m := range result {
		idSet[m.ID()] = true
	}
	require.True(t, idSet[meta1.ID()])
	require.True(t, idSet[meta2.ID()])
	require.False(t, idSet[meta3.ID()])
}

func TestStore_Indexes_UsesSnapshot(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()

	// Write an index, but don't poll yet
	meta := Meta{
		Date: "2026-02-23", Hash: "snapshot", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}
	writeTestIndex(t, s, meta, "content")

	// Indexes() before Poll should return nothing (uses empty snapshot)
	result := s.IndexesForRange(now.Add(-2*time.Hour), now.Add(time.Hour))
	require.Empty(t, result)

	// After Poll, the index appears
	require.NoError(t, s.poll(context.Background()))
	result = s.IndexesForRange(now.Add(-2*time.Hour), now.Add(time.Hour))
	require.Len(t, result, 1)
}

func TestStore_IndexesExcludedByIngesterWindow(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)
	s.cfg.QueryIngestersWithin = 3 * time.Hour

	now := time.Now().UTC()
	oldRec := Meta{
		Date: "2026-02-23", Hash: "oldrec", Version: "v3",
		MinLogTs: now.Add(-24 * time.Hour), MaxLogTs: now.Add(-23 * time.Hour),
		MinRecordTs: now.Add(-24 * time.Hour), MaxRecordTs: now.Add(-23 * time.Hour),
	}
	freshRec := Meta{
		Date: "2026-02-23", Hash: "freshrec", Version: "v3",
		MinLogTs: now.Add(-24 * time.Hour), MaxLogTs: now.Add(-23 * time.Hour),
		MinRecordTs: now.Add(-30 * time.Minute), MaxRecordTs: now.Add(-15 * time.Minute),
	}
	writeTestIndex(t, s, oldRec, "old")
	writeTestIndex(t, s, freshRec, "fresh")
	require.NoError(t, s.poll(context.Background()))

	queryStart := now.Add(-25 * time.Hour)
	queryEnd := now.Add(-22 * time.Hour)

	included := s.IndexesForRange(queryStart, queryEnd)
	require.Len(t, included, 1)
	require.Equal(t, oldRec.ID(), included[0].ID())

	excluded := s.IndexesExcludedByIngesterWindow(queryStart, queryEnd)
	require.Len(t, excluded, 1)
	require.Equal(t, freshRec.ID(), excluded[0].ID())
}

func TestStore_Indexes_NoBlockBeforePoll(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	// Should return empty, not panic or block
	result := s.IndexesForRange(time.Now().Add(-24*time.Hour), time.Now())
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestStore_Indexes_MinDateSet_IsInclusiveAndFiltersEarlierDates(t *testing.T) {
	inner := objstore.NewInMemBucket()
	bucket := &trackingBucket{Bucket: inner}
	cfg := Config{
		PollInterval:          10 * time.Millisecond,
		RetentionDuration:     7 * 24 * time.Hour,
		CompactionGracePeriod: 24 * time.Hour,
		MinDate:               "2026-02-23",
	}
	s, err := NewStore(bucket, cfg, log.NewNopLogger(), prometheus.NewPedanticRegistry())
	require.NoError(t, err)

	now := time.Now().UTC()
	before := Meta{
		Date: "2026-02-22", Hash: "before-min-date", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}
	boundary := Meta{
		Date: "2026-02-23", Hash: "on-min-date", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}
	after := Meta{
		Date: "2026-02-24", Hash: "after-min-date", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}
	writeTestIndex(t, s, before, "before")
	writeTestIndex(t, s, boundary, "boundary")
	writeTestIndex(t, s, after, "after")
	require.NoError(t, s.poll(context.Background()))

	snap := s.Snapshot()
	require.Len(t, snap.All(), 2, "pre-MinDate indexes must not be present in the snapshot")
	require.False(t, snap.Contains(before.ID()))
	require.True(t, snap.Contains(boundary.ID()))
	require.True(t, snap.Contains(after.ID()))

	require.NotContains(t, bucket.GetCalls(), before.MetaPath(), "poll must not fetch meta for pre-MinDate indexes")
	require.NotContains(t, bucket.GetIterCalls(), "2026-02-22/", "poll must not list inside pre-MinDate date prefixes")
	require.Contains(t, bucket.GetIterCalls(), "2026-02-23/")
	require.Contains(t, bucket.GetIterCalls(), "2026-02-24/")

	result := s.IndexesForRange(now.Add(-2*time.Hour), now.Add(time.Hour))
	require.Len(t, result, 2)

	ids := make(map[string]struct{}, len(result))
	for _, m := range result {
		ids[m.ID()] = struct{}{}
	}
	require.NotContains(t, ids, before.ID())
	require.Contains(t, ids, boundary.ID())
	require.Contains(t, ids, after.ID())
}

// ---- Store.EligibleForDeletion tests ----

func TestStore_EligibleForDeletion_ActiveRecentFile(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()

	// Recent active index — MaxLogTs is only 1 day old (retention is 7 days)
	meta := Meta{
		Date: "2026-02-23", Hash: "recent", Version: "v3",
		MinLogTs: now.Add(-25 * time.Hour), MaxLogTs: now.Add(-24 * time.Hour),
	}
	writeTestIndex(t, s, meta, "content")

	require.NoError(t, s.poll(context.Background()))

	result := s.EligibleForDeletion(now)
	require.Empty(t, result)
}

func TestStore_EligibleForDeletion_ActiveOldFile(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()

	// Old active index — MaxLogTs is 8 days old (retention is 7 days)
	meta := Meta{
		Date: "2026-02-15", Hash: "old", Version: "v3",
		MinLogTs: now.Add(-9 * 24 * time.Hour), MaxLogTs: now.Add(-8 * 24 * time.Hour),
	}
	writeTestIndex(t, s, meta, "content")

	require.NoError(t, s.poll(context.Background()))

	result := s.EligibleForDeletion(now)
	require.Len(t, result, 1)
	require.Equal(t, meta.ID(), result[0].ID())
}

func TestStore_EligibleForDeletion_CompactedWithinGrace(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	source := Meta{
		Date: "2026-02-23", Hash: "source", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now.Add(-1 * time.Hour),
	}
	merged := Meta{
		Date: "2026-02-23", Hash: "merged", Version: "v3",
		MinLogTs:      now.Add(-2 * time.Hour),
		MaxLogTs:      now,
		CompactedFrom: []string{source.ID()},
	}

	writeTestIndex(t, s, source, "source")
	writeTestIndex(t, s, merged, "merged")

	require.NoError(t, s.poll(context.Background()))

	result := s.EligibleForDeletion(now)
	require.Empty(t, result, "source within grace period should not be eligible")
}

func TestStore_EligibleForDeletion_CompactedExpiredGrace(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	source := Meta{
		Date: "2026-02-21", Hash: "oldsource", Version: "v3",
		MinLogTs: now.Add(-4 * 24 * time.Hour), MaxLogTs: now.Add(-3 * 24 * time.Hour),
	}
	merged := Meta{
		Date: "2026-02-23", Hash: "newmerged", Version: "v3",
		MinLogTs:      now.Add(-4 * 24 * time.Hour),
		MaxLogTs:      now.Add(-1 * time.Hour),
		CompactedFrom: []string{source.ID()},
	}

	writeTestIndex(t, s, source, "old source")
	writeTestIndex(t, s, merged, "merged")

	require.NoError(t, s.poll(context.Background()))

	// Pass a future "now" so that the merged index's CreatedAt (set to real time.Now()
	// by Write) is older than the grace cutoff. Advance by 48h to exceed the 24h grace period.
	result := s.EligibleForDeletion(now.Add(48 * time.Hour))
	require.Len(t, result, 1)
	require.Equal(t, source.ID(), result[0].ID())
}

func TestStore_EligibleForDeletion_EmptySnapshot(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	result := s.EligibleForDeletion(time.Now())
	require.NotNil(t, result, "should return empty slice, not nil")
	require.Empty(t, result)
}

// ---- StartPolling integration test ----

func TestStore_StartPolling_UpdatesSnapshot(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	ctx := t.Context()

	require.NoError(t, s.StartPolling(ctx))

	now := time.Now().UTC()
	meta := Meta{
		Date: "2026-02-23", Hash: "polled", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}

	writeTestIndex(t, s, meta, "polled index")

	// Wait for the polling goroutine to pick up the new index.
	// PollInterval is 10ms in test store, so we wait up to 200ms.
	deadline := time.Now().Add(200 * time.Millisecond)

	var result []Meta
	for time.Now().Before(deadline) {
		result = s.IndexesForRange(now.Add(-2*time.Hour), now.Add(time.Hour))
		if len(result) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	require.Len(t, result, 1, "polling should have discovered the new index within 200ms")
	require.Equal(t, meta.ID(), result[0].ID())
}

func TestStore_Poll_RemovesDeletedIndex(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	meta := Meta{
		Date: "2026-02-23", Hash: "disappears", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}
	writeTestIndex(t, s, meta, "content")

	require.NoError(t, s.poll(context.Background()))
	require.Len(t, s.IndexesForRange(now.Add(-2*time.Hour), now.Add(time.Hour)), 1)

	// Delete both files directly from the bucket (bypass Store)
	require.NoError(t, bucket.Delete(context.Background(), meta.MetaPath()))
	require.NoError(t, bucket.Delete(context.Background(), meta.IndexPath()))

	// Next poll should rebuild snapshot without this entry
	require.NoError(t, s.poll(context.Background()))
	result := s.IndexesForRange(now.Add(-2*time.Hour), now.Add(time.Hour))
	require.Empty(t, result, "snapshot should not contain deleted index after re-poll")
}

// ---- Concurrent listing ----

func TestStore_Poll_ConcurrentListAcrossDates(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()

	// Spread indexes across 7 dates with 3 indexes per date → 21 total.
	// This exercises the concurrent inner-Iter fan-out in Phase 1.
	var want []string
	for day := 1; day <= 7; day++ {
		date := fmt.Sprintf("2026-03-%02d", day)
		for idx := 1; idx <= 3; idx++ {
			hash := fmt.Sprintf("%04d%012d", day, idx)
			m := Meta{
				Date:     date,
				Hash:     hash,
				Version:  "v3",
				MinLogTs: now.Add(-time.Duration(day) * time.Hour),
				MaxLogTs: now,
			}
			writeTestIndex(t, s, m, "data")
			want = append(want, m.ID())
		}
	}

	require.NoError(t, s.poll(context.Background()))

	snap := s.Snapshot()
	require.Len(t, snap.All(), 21, "all 21 indexes must survive concurrent listing")

	gotIDs := make(map[string]struct{}, len(snap.All()))
	for _, m := range snap.All() {
		gotIDs[m.ID()] = struct{}{}
	}
	for _, id := range want {
		require.Contains(t, gotIDs, id, "missing index %s", id)
	}
}

func TestStore_Poll_PropagatesInnerIterError(t *testing.T) {
	inner := objstore.NewInMemBucket()

	now := time.Now().UTC()
	// Write indexes under two dates so outer Iter succeeds.
	for _, date := range []string{"2026-03-01", "2026-03-02"} {
		meta := Meta{
			Date: date, Hash: "aaaa000000000001", Version: "v3",
			MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
			MinRecordTs: now.Add(-1 * time.Hour), MaxRecordTs: now,
		}
		metaBytes, err := json.Marshal(meta)
		require.NoError(t, err)
		require.NoError(t, inner.Upload(context.Background(), meta.MetaPath(), bytes.NewReader(metaBytes)))
		require.NoError(t, inner.Upload(context.Background(), meta.IndexPath(), strings.NewReader("data")))
	}

	bucket := &innerIterErrorBucket{Bucket: inner, failDate: "2026-03-02/"}
	s := newTestStore(t, bucket)

	err := s.poll(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "poll list failed")
}

// innerIterErrorBucket fails Iter only for a specific date prefix (inner Iter),
// allowing the outer date-level Iter to succeed.
type innerIterErrorBucket struct {
	objstore.Bucket
	failDate string
}

func (b *innerIterErrorBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	if dir == b.failDate {
		return fmt.Errorf("injected inner iter error for %s", dir)
	}
	return b.Bucket.Iter(ctx, dir, f, options...)
}

func (b *innerIterErrorBucket) IsObjNotFoundErr(err error) bool {
	return b.Bucket.IsObjNotFoundErr(err)
}

// ---- Poll per-date metrics ----

func TestStore_Poll_PerDateMetrics(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	reg := prometheus.NewPedanticRegistry()
	cfg := Config{
		PollInterval:          10 * time.Millisecond,
		RetentionDuration:     7 * 24 * time.Hour,
		CompactionGracePeriod: 24 * time.Hour,
		MinDate:               "0001-01-01",
	}
	s, err := NewStore(bucket, cfg, log.NewNopLogger(), reg)
	require.NoError(t, err)

	now := time.Now().UTC()
	date1 := now.AddDate(0, 0, -3).Format("2006-01-02")
	date2 := now.AddDate(0, 0, -2).Format("2006-01-02")
	date3 := now.AddDate(0, 0, -1).Format("2006-01-02")

	// Date 1: two leaf indexes.
	writeTestIndex(t, s, Meta{
		Date: date1, Hash: "aaaa000000000001", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now.Add(-1 * time.Hour),
	}, "d1-a")
	writeTestIndex(t, s, Meta{
		Date: date1, Hash: "aaaa000000000002", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}, "d1-b")

	// Date 2: one leaf + one merged (compacts the leaf).
	src := Meta{
		Date: date2, Hash: "bbbb000000000001", Version: "v3",
		MinLogTs: now.Add(-3 * time.Hour), MaxLogTs: now.Add(-2 * time.Hour),
	}
	writeTestIndex(t, s, src, "d2-src")
	writeTestIndex(t, s, Meta{
		Date: date2, Hash: "bbbb000000000002", Version: "v3",
		MinLogTs: now.Add(-3 * time.Hour), MaxLogTs: now,
		CompactedFrom: []string{src.ID()},
	}, "d2-merged")

	// Date 3: single leaf.
	writeTestIndex(t, s, Meta{
		Date: date3, Hash: "cccc000000000001", Version: "v3",
		MinLogTs: now.Add(-30 * time.Minute), MaxLogTs: now,
	}, "d3-a")

	require.NoError(t, s.Poll(context.Background()))

	// active: date1=2, date2=1 (merged only), date3=1
	require.Equal(t, 2.0, testutil.ToFloat64(s.metrics.indexes.WithLabelValues("active", date1)))
	require.Equal(t, 1.0, testutil.ToFloat64(s.metrics.indexes.WithLabelValues("active", date2)))
	require.Equal(t, 1.0, testutil.ToFloat64(s.metrics.indexes.WithLabelValues("active", date3)))

	// compacted: date2=1 (source), others absent (zero).
	require.Equal(t, 1.0, testutil.ToFloat64(s.metrics.indexes.WithLabelValues("compacted", date2)))
	require.Equal(t, 0.0, testutil.ToFloat64(s.metrics.indexes.WithLabelValues("compacted", date1)))
	require.Equal(t, 0.0, testutil.ToFloat64(s.metrics.indexes.WithLabelValues("compacted", date3)))
}

func TestStore_Poll_PerDateMetrics_SkipsPreMinDate(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	reg := prometheus.NewPedanticRegistry()
	now := time.Now().UTC()
	dateBefore := now.AddDate(0, 0, -2).Format("2006-01-02")
	dateOnOrAfter := now.AddDate(0, 0, -1).Format("2006-01-02")
	cfg := Config{
		PollInterval:          10 * time.Millisecond,
		RetentionDuration:     7 * 24 * time.Hour,
		CompactionGracePeriod: 24 * time.Hour,
		MinDate:               dateOnOrAfter,
	}
	s, err := NewStore(bucket, cfg, log.NewNopLogger(), reg)
	require.NoError(t, err)

	writeTestIndex(t, s, Meta{
		Date: dateBefore, Hash: "aaaa000000000010", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now.Add(-1 * time.Hour),
	}, "before")
	writeTestIndex(t, s, Meta{
		Date: dateOnOrAfter, Hash: "bbbb000000000010", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}, "on-or-after")

	require.NoError(t, s.Poll(context.Background()))

	require.Equal(t, 0.0, testutil.ToFloat64(s.metrics.indexes.WithLabelValues("pre_min_date", dateBefore)))
	require.Equal(t, 1.0, testutil.ToFloat64(s.metrics.indexes.WithLabelValues("active", dateOnOrAfter)))
	require.Equal(t, 0.0, testutil.ToFloat64(s.metrics.indexes.WithLabelValues("active", dateBefore)))
}

func TestStore_Poll_PerDateMetrics_ResetOnEmpty(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	reg := prometheus.NewPedanticRegistry()
	cfg := Config{
		PollInterval:          10 * time.Millisecond,
		RetentionDuration:     7 * 24 * time.Hour,
		CompactionGracePeriod: 24 * time.Hour,
		MinDate:               "0001-01-01",
	}
	s, err := NewStore(bucket, cfg, log.NewNopLogger(), reg)
	require.NoError(t, err)

	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	meta := Meta{
		Date: today, Hash: "aaaa000000000001", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}
	writeTestIndex(t, s, meta, "data")
	require.NoError(t, s.Poll(context.Background()))
	require.Equal(t, 1.0, testutil.ToFloat64(s.metrics.indexes.WithLabelValues("active", today)))

	// Remove the index and re-poll — gauge should reset to zero.
	require.NoError(t, bucket.Delete(context.Background(), meta.MetaPath()))
	require.NoError(t, bucket.Delete(context.Background(), meta.IndexPath()))
	require.NoError(t, s.Poll(context.Background()))

	// After reset, the metric family should have no children.
	count, err := testutil.GatherAndCount(reg, "logline_index_store_indexes")
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestStore_Poll_PerDateBytesMetric(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	reg := prometheus.NewPedanticRegistry()
	cfg := Config{
		PollInterval:          10 * time.Millisecond,
		RetentionDuration:     7 * 24 * time.Hour,
		CompactionGracePeriod: 24 * time.Hour,
		MinDate:               "0001-01-01",
	}
	s, err := NewStore(bucket, cfg, log.NewNopLogger(), reg)
	require.NoError(t, err)

	now := time.Now().UTC()
	date1 := now.AddDate(0, 0, -3).Format("2006-01-02")
	date2 := now.AddDate(0, 0, -2).Format("2006-01-02")
	date3 := now.AddDate(0, 0, -1).Format("2006-01-02")

	// Date 1: two leaf indexes — "d1-a" (4 bytes) + "d1-b" (4 bytes) = 8
	writeTestIndex(t, s, Meta{
		Date: date1, Hash: "aaaa000000000001", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now.Add(-1 * time.Hour),
	}, "d1-a")
	writeTestIndex(t, s, Meta{
		Date: date1, Hash: "aaaa000000000002", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}, "d1-b")

	// Date 2: one source "d2-src" (6 bytes) + one merged "d2-merged" (9 bytes) = 15.
	// Compacted source still occupies bytes in object storage.
	src := Meta{
		Date: date2, Hash: "bbbb000000000001", Version: "v3",
		MinLogTs: now.Add(-3 * time.Hour), MaxLogTs: now.Add(-2 * time.Hour),
	}
	writeTestIndex(t, s, src, "d2-src")
	writeTestIndex(t, s, Meta{
		Date: date2, Hash: "bbbb000000000002", Version: "v3",
		MinLogTs: now.Add(-3 * time.Hour), MaxLogTs: now,
		CompactedFrom: []string{src.ID()},
	}, "d2-merged")

	// Date 3: single leaf — "d3-ab" (5 bytes) = 5
	writeTestIndex(t, s, Meta{
		Date: date3, Hash: "cccc000000000001", Version: "v3",
		MinLogTs: now.Add(-30 * time.Minute), MaxLogTs: now,
	}, "d3-ab")

	require.NoError(t, s.Poll(context.Background()))

	require.Equal(t, float64(len("d1-a")+len("d1-b")), testutil.ToFloat64(s.metrics.indexBytes.WithLabelValues("active", date1)))
	// Compacted source and merged index tracked separately by state.
	require.Equal(t, float64(len("d2-src")), testutil.ToFloat64(s.metrics.indexBytes.WithLabelValues("compacted", date2)))
	require.Equal(t, float64(len("d2-merged")), testutil.ToFloat64(s.metrics.indexBytes.WithLabelValues("active", date2)))
	require.Equal(t, float64(len("d3-ab")), testutil.ToFloat64(s.metrics.indexBytes.WithLabelValues("active", date3)))
}

func TestStore_Poll_PerDateBytesMetric_ResetOnEmpty(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	reg := prometheus.NewPedanticRegistry()
	cfg := Config{
		PollInterval:          10 * time.Millisecond,
		RetentionDuration:     7 * 24 * time.Hour,
		CompactionGracePeriod: 24 * time.Hour,
		MinDate:               "0001-01-01",
	}
	s, err := NewStore(bucket, cfg, log.NewNopLogger(), reg)
	require.NoError(t, err)

	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	meta := Meta{
		Date: today, Hash: "aaaa000000000001", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
	}
	writeTestIndex(t, s, meta, "data")
	require.NoError(t, s.Poll(context.Background()))
	// "data" = 4 bytes
	require.Equal(t, 4.0, testutil.ToFloat64(s.metrics.indexBytes.WithLabelValues("active", today)))

	// Remove the index and re-poll — gauge should reset.
	require.NoError(t, bucket.Delete(context.Background(), meta.MetaPath()))
	require.NoError(t, bucket.Delete(context.Background(), meta.IndexPath()))
	require.NoError(t, s.Poll(context.Background()))

	// After reset, the metric family should have no children.
	count, err := testutil.GatherAndCount(reg, "logline_index_store_index_bytes")
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

// ---- buildSnapshot helper tests ----

func TestBuildSnapshot_CompactedSetCorrect(t *testing.T) {
	now := time.Now().UTC()
	metaA := Meta{Date: "2026-02-23", Hash: "a", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now.Add(-1 * time.Hour)}
	metaB := Meta{Date: "2026-02-23", Hash: "b", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now,
		CompactedFrom: []string{metaA.ID()}}
	metaC := Meta{Date: "2026-02-23", Hash: "c", Version: "v3",
		MinLogTs: now.Add(-30 * time.Minute), MaxLogTs: now}

	snap := buildSnapshot([]Meta{metaA, metaB, metaC})

	// metaA should be in compactedAt
	_, inCompacted := snap.compactedAt[metaA.ID()]
	require.True(t, inCompacted)

	// metaB and metaC should NOT be in compactedAt
	_, inCompacted = snap.compactedAt[metaB.ID()]
	require.False(t, inCompacted)
	_, inCompacted = snap.compactedAt[metaC.ID()]
	require.False(t, inCompacted)

	// active should contain B and C but not A
	require.Len(t, snap.active, 2)
	activeIDs := make(map[string]bool)
	for _, m := range snap.active {
		activeIDs[m.ID()] = true
	}
	require.False(t, activeIDs[metaA.ID()])
	require.True(t, activeIDs[metaB.ID()])
	require.True(t, activeIDs[metaC.ID()])
}

func TestStore_Write_SetsVersion(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	meta := Meta{
		Date: "2026-02-23", Hash: "versioned", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
		MinRecordTs: now.Add(-1 * time.Hour), MaxRecordTs: now,
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   int64(len("data")),
	}

	err := s.PutIndex(context.Background(), strings.NewReader("data"), meta)
	require.NoError(t, err)

	rc, err := bucket.Get(context.Background(), meta.MetaPath())
	require.NoError(t, err)
	defer rc.Close()

	var decoded Meta
	require.NoError(t, json.NewDecoder(rc).Decode(&decoded))
	require.Equal(t, "v3", decoded.Version)
}

// ---- RetentionDuration uses MaxLogTs ----

func TestStore_EligibleForDeletion_UsesMaxLogTsNotCreatedAt(t *testing.T) {
	// Verify that retention is based on the data's MaxLogTs, not CreatedAt.
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()

	// Write index — data is 8 days old (MaxLogTs), but we simulate it was "just written"
	meta := Meta{
		Date: "2026-02-15", Hash: "olddata", Version: "v3",
		MinLogTs:    now.Add(-9 * 24 * time.Hour),
		MaxLogTs:    now.Add(-8 * 24 * time.Hour), // data is 8 days old
		MinRecordTs: now.Add(-9 * 24 * time.Hour),
		MaxRecordTs: now.Add(-8 * 24 * time.Hour),
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   int64(len("data")),
	}
	err := s.PutIndex(context.Background(), strings.NewReader("data"), meta)
	require.NoError(t, err)

	require.NoError(t, s.poll(context.Background()))

	// EligibleForDeletion should use MaxLogTs — 8 days > 7 day retention
	result := s.EligibleForDeletion(now)
	require.Len(t, result, 1, "index with old MaxLogTs should be eligible for deletion")
	require.Equal(t, meta.ID(), result[0].ID())
}

// ---- Multiple CompactedFrom sources ----

func TestStore_Poll_MultipleCompactedFrom(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	metaA := Meta{
		Date: "2026-02-23", Hash: "src-a", Version: "v3",
		MinLogTs: now.Add(-3 * time.Hour), MaxLogTs: now.Add(-2 * time.Hour),
	}
	metaB := Meta{
		Date: "2026-02-23", Hash: "src-b", Version: "v3",
		MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now.Add(-1 * time.Hour),
	}
	metaMerged := Meta{
		Date: "2026-02-23", Hash: "merged", Version: "v3",
		MinLogTs:      now.Add(-3 * time.Hour),
		MaxLogTs:      now.Add(-1 * time.Hour),
		CompactedFrom: []string{metaA.ID(), metaB.ID()},
	}

	writeTestIndex(t, s, metaA, "a")
	writeTestIndex(t, s, metaB, "b")
	writeTestIndex(t, s, metaMerged, "merged")

	require.NoError(t, s.poll(context.Background()))

	result := s.IndexesForRange(now.Add(-4*time.Hour), now)
	require.Len(t, result, 1)
	require.Equal(t, metaMerged.ID(), result[0].ID())
}

// ---- Poll with unexpected path structure ----

func TestStore_Poll_FailsOnUnexpectedPathStructure(t *testing.T) {
	inner := objstore.NewInMemBucket()

	// Upload an index at a valid path so the outer Iter has something to visit
	err := inner.Upload(context.Background(), "2026-02-23/abc/meta.json",
		bytes.NewReader([]byte(`{"version":"v3","min_log_ts":"2026-02-23T10:00:00Z","max_log_ts":"2026-02-23T11:00:00Z","created_at":"2026-02-23T12:00:00Z"}`)))
	require.NoError(t, err)

	bucket := &deepPathBucket{Bucket: inner}
	s := newTestStore(t, bucket)

	// Poll should fail — unexpected path structure is an error
	err = s.poll(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid storage id")
}

func TestStore_Poll_FailsOnMismatchedJSONID(t *testing.T) {
	inner := objstore.NewInMemBucket()
	s := newTestStore(t, inner)

	now := time.Now().UTC().Truncate(time.Second)
	meta := Meta{
		Date:        "2026-02-23",
		StorageID:   "path-id",
		Hash:        "deadbeef00000000",
		Version:     "v3",
		MinLogTs:    now.Add(-1 * time.Hour),
		MaxLogTs:    now,
		MinRecordTs: now.Add(-1 * time.Hour),
		MaxRecordTs: now,
	}
	writeTestIndex(t, s, meta, "index-data")

	// Overwrite meta.json with a different StorageID than the path.
	meta.StorageID = "json-id"
	metaBytes, err := json.Marshal(meta)
	require.NoError(t, err)
	require.NoError(t, inner.Upload(context.Background(), "2026-02-23/path-id/meta.json", bytes.NewReader(metaBytes)))

	err = s.poll(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "mismatched storage id")
}

// deepPathBucket returns an extra-deep path prefix and serves a valid meta for it.
type deepPathBucket struct {
	objstore.Bucket
}

func (d *deepPathBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	if dir == "" {
		return f("2026-02-23/")
	}
	if dir == "2026-02-23/" {
		// Return a path with 3 components — unexpected, should be 2 (date/id)
		return f("2026-02-23/extra/deep/")
	}
	return d.Bucket.Iter(ctx, dir, f, options...)
}

func (d *deepPathBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	// Return valid meta JSON so Poll reaches the path-structure check
	if name == "2026-02-23/extra/deep/meta.json" {
		meta := `{"version":"v3","min_log_ts":"2026-02-23T10:00:00Z","max_log_ts":"2026-02-23T11:00:00Z","created_at":"2026-02-23T12:00:00Z"}`
		return io.NopCloser(strings.NewReader(meta)), nil
	}
	return d.Bucket.Get(ctx, name)
}

func (d *deepPathBucket) IsObjNotFoundErr(err error) bool {
	return d.Bucket.IsObjNotFoundErr(err)
}

// ---- Poll returns error on I/O failure ----

func TestStore_Poll_PropagatesIterError(t *testing.T) {
	bucket := &errorBucket{Bucket: objstore.NewInMemBucket(), failIter: true}
	s := newTestStore(t, bucket)

	err := s.poll(context.Background())
	require.Error(t, err)
}

func TestStore_Poll_PropagatesGetError(t *testing.T) {
	inner := objstore.NewInMemBucket()

	err := inner.Upload(context.Background(), "2026-02-23/geterr/meta.json",
		bytes.NewReader([]byte(`{}`)))
	require.NoError(t, err)

	bucket := &errorBucket{Bucket: inner, failGet: true}
	s := newTestStore(t, bucket)

	pollErr := s.poll(context.Background())
	require.Error(t, pollErr)
}

func TestStore_Write_UploadIndexError(t *testing.T) {
	bucket := &errorBucket{Bucket: objstore.NewInMemBucket(), failUpload: true}
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	err := s.PutIndex(context.Background(), strings.NewReader("data"), Meta{
		Date: "2026-02-23", Hash: "uploadfail", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
		MinRecordTs: now.Add(-1 * time.Hour), MaxRecordTs: now,
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   int64(len("data")),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to upload index data")
}

func TestStore_Write_UploadMetaError(t *testing.T) {
	inner := objstore.NewInMemBucket()
	bucket := &errorBucket{Bucket: inner, failUploadMeta: true}
	s := newTestStore(t, bucket)

	now := time.Now().UTC()
	err := s.PutIndex(context.Background(), strings.NewReader("data"), Meta{
		Date: "2026-02-23", Hash: "metafail", Version: "v3",
		MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now,
		MinRecordTs: now.Add(-1 * time.Hour), MaxRecordTs: now,
		IndexHeader: &format.HeaderInfo{},
		SizeBytes:   int64(len("data")),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to upload meta.json")
}

func TestStore_StartPolling_LogsInitialPollError(t *testing.T) {
	bucket := &errorBucket{Bucket: objstore.NewInMemBucket(), failIter: true}
	s := newTestStore(t, bucket)

	ctx := t.Context()

	// The initial poll fails and the error is surfaced to the caller rather than
	// panicking or blocking.
	require.Error(t, s.StartPolling(ctx))
	time.Sleep(20 * time.Millisecond)
}

func TestStore_StartPolling_CancelStopsGoroutine(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	s := newTestStore(t, bucket)

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, s.StartPolling(ctx))

	// Cancel and give the goroutine time to stop
	cancel()
	time.Sleep(20 * time.Millisecond)
	// No assertion needed — just verify it doesn't block/panic
}

// ---- Delta poll tests ----

func TestStore_DeltaPoll_FirstPollFetchesAll(t *testing.T) {
	inner := objstore.NewInMemBucket()
	tb := &trackingBucket{Bucket: inner}
	s := newTestStore(t, tb)

	now := time.Now().UTC()
	metas := []Meta{
		{Date: "2026-02-23", Hash: "delta1", Version: "v3", MinLogTs: now.Add(-3 * time.Hour), MaxLogTs: now},
		{Date: "2026-02-23", Hash: "delta2", Version: "v3", MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now},
		{Date: "2026-02-24", Hash: "delta3", Version: "v3", MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now},
	}
	for _, m := range metas {
		writeTestIndex(t, s, m, "content")
	}

	require.NoError(t, s.poll(context.Background()))

	calls := tb.GetCalls()
	for _, m := range metas {
		require.Contains(t, calls, m.MetaPath(), "first poll must fetch meta for %s", m.ID())
	}
}

func TestStore_DeltaPoll_SecondPollSkipsKnownMetas(t *testing.T) {
	inner := objstore.NewInMemBucket()
	tb := &trackingBucket{Bucket: inner}
	s := newTestStore(t, tb)

	now := time.Now().UTC()
	metas := []Meta{
		{Date: "2026-02-23", Hash: "skip1", Version: "v3", MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now},
		{Date: "2026-02-23", Hash: "skip2", Version: "v3", MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now},
	}
	for _, m := range metas {
		writeTestIndex(t, s, m, "content")
	}

	// First poll: full fetch.
	require.NoError(t, s.poll(context.Background()))
	callsAfterFirst := len(tb.GetCalls())

	// Second poll: no changes — known metas must not be re-fetched.
	require.NoError(t, s.poll(context.Background()))
	newCalls := tb.GetCalls()[callsAfterFirst:]

	for _, m := range metas {
		require.NotContains(t, newCalls, m.MetaPath(), "second poll must not re-fetch known meta %s", m.ID())
	}

	// Snapshot must still contain both indexes.
	require.Len(t, s.Snapshot().All(), 2)
}

func TestStore_DeltaPoll_NewIndexFetchedOnSecondPoll(t *testing.T) {
	inner := objstore.NewInMemBucket()
	tb := &trackingBucket{Bucket: inner}
	s := newTestStore(t, tb)

	now := time.Now().UTC()
	meta1 := Meta{Date: "2026-02-23", Hash: "new1", Version: "v3", MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now}
	writeTestIndex(t, s, meta1, "content1")

	// First poll: discovers meta1.
	require.NoError(t, s.poll(context.Background()))
	callsAfterFirst := len(tb.GetCalls())

	// Write a second index.
	meta2 := Meta{Date: "2026-02-23", Hash: "new2", Version: "v3", MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now}
	writeTestIndex(t, s, meta2, "content2")

	// Second poll: only meta2 should be fetched.
	require.NoError(t, s.poll(context.Background()))
	newCalls := tb.GetCalls()[callsAfterFirst:]

	require.Contains(t, newCalls, meta2.MetaPath(), "second poll must fetch newly discovered meta")
	require.NotContains(t, newCalls, meta1.MetaPath(), "second poll must not re-fetch known meta")

	// Both indexes are in the snapshot.
	snap := s.Snapshot()
	require.Len(t, snap.All(), 2)
}

func TestStore_DeltaPoll_DeletedIndexDisappearsAfterPoll(t *testing.T) {
	inner := objstore.NewInMemBucket()
	tb := &trackingBucket{Bucket: inner}
	s := newTestStore(t, tb)

	now := time.Now().UTC()
	meta1 := Meta{Date: "2026-02-23", Hash: "del1", Version: "v3", MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now}
	meta2 := Meta{Date: "2026-02-23", Hash: "del2", Version: "v3", MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now}
	writeTestIndex(t, s, meta1, "content1")
	writeTestIndex(t, s, meta2, "content2")

	require.NoError(t, s.poll(context.Background()))
	require.Len(t, s.Snapshot().All(), 2)

	// Delete meta2 directly from the underlying bucket.
	require.NoError(t, inner.Delete(context.Background(), meta2.MetaPath()))
	require.NoError(t, inner.Delete(context.Background(), meta2.IndexPath()))

	require.NoError(t, s.poll(context.Background()))
	snap := s.Snapshot()
	require.Len(t, snap.All(), 1, "deleted index must disappear after next poll")
	require.Equal(t, meta1.ID(), snap.All()[0].ID())
}

func TestStore_DeltaPoll_DeletedThenReaddedIndex(t *testing.T) {
	inner := objstore.NewInMemBucket()
	tb := &trackingBucket{Bucket: inner}
	s := newTestStore(t, tb)

	now := time.Now().UTC()
	meta1 := Meta{Date: "2026-02-23", Hash: "readd1", Version: "v3", MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now}
	writeTestIndex(t, s, meta1, "content")

	// Poll 1: discover meta1, add to knownMetas.
	require.NoError(t, s.poll(context.Background()))
	require.Len(t, s.Snapshot().All(), 1)

	// Delete meta1 from bucket.
	require.NoError(t, inner.Delete(context.Background(), meta1.MetaPath()))
	require.NoError(t, inner.Delete(context.Background(), meta1.IndexPath()))

	// Poll 2: meta1 is gone from listing; knownMetas is pruned.
	require.NoError(t, s.poll(context.Background()))
	require.Empty(t, s.Snapshot().All(), "deleted index must disappear")

	// Re-upload meta1 with the same path.
	callsBeforeReadd := len(tb.GetCalls())
	writeTestIndex(t, s, meta1, "content")

	// Poll 3: meta1 re-appears; because it was pruned from knownMetas it is treated as new.
	require.NoError(t, s.poll(context.Background()))
	require.Len(t, s.Snapshot().All(), 1, "re-added index must reappear")

	newCalls := tb.GetCalls()[callsBeforeReadd:]
	require.Contains(t, newCalls, meta1.MetaPath(), "re-added index must be fetched again after cache pruning")
}

func TestStore_DeltaPoll_ErrorPreservesKnownMetas(t *testing.T) {
	inner := objstore.NewInMemBucket()
	eb := &errorBucket{Bucket: inner}
	tb := &trackingBucket{Bucket: eb}
	s := newTestStore(t, tb)

	now := time.Now().UTC()
	meta1 := Meta{Date: "2026-02-23", Hash: "err1", Version: "v3", MinLogTs: now.Add(-3 * time.Hour), MaxLogTs: now}
	meta2 := Meta{Date: "2026-02-23", Hash: "err2", Version: "v3", MinLogTs: now.Add(-2 * time.Hour), MaxLogTs: now}
	writeTestIndex(t, s, meta1, "content1")
	writeTestIndex(t, s, meta2, "content2")

	// Poll 1: both metas fetched and cached.
	require.NoError(t, s.poll(context.Background()))
	require.Len(t, s.knownMetas, 2)

	// Write a third index, but inject a Get error for its meta.json.
	meta3 := Meta{Date: "2026-02-23", Hash: "err3", Version: "v3", MinLogTs: now.Add(-1 * time.Hour), MaxLogTs: now}
	writeTestIndex(t, s, meta3, "content3")
	eb.failGetPath = meta3.MetaPath()

	// Poll 2: must fail due to the injected error.
	require.Error(t, s.poll(context.Background()))

	// knownMetas must be unchanged — the failed poll must not corrupt the cache.
	require.Len(t, s.knownMetas, 2)
	require.Contains(t, s.knownMetas, meta1.ID())
	require.Contains(t, s.knownMetas, meta2.ID())

	// Heal the error.
	eb.failGetPath = ""
	callsBeforeHeal := len(tb.GetCalls())

	// Poll 3: must succeed, fetching only meta3 (meta1 and meta2 are still cached).
	require.NoError(t, s.poll(context.Background()))
	snap := s.Snapshot()
	require.Len(t, snap.All(), 3)

	newCalls := tb.GetCalls()[callsBeforeHeal:]
	require.Contains(t, newCalls, meta3.MetaPath(), "healed poll must fetch meta3")
	require.NotContains(t, newCalls, meta1.MetaPath(), "meta1 must remain cached after healed poll")
	require.NotContains(t, newCalls, meta2.MetaPath(), "meta2 must remain cached after healed poll")
}

// errorBucket wraps an InMemBucket and injects errors for testing.
type errorBucket struct {
	objstore.Bucket
	failIter       bool
	failGet        bool
	failGetPath    string // fail Get only for this specific path
	failUpload     bool
	failUploadMeta bool
}

func (e *errorBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	if e.failIter {
		return fmt.Errorf("injected iter error")
	}
	return e.Bucket.Iter(ctx, dir, f, options...)
}

func (e *errorBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	if e.failGet || (e.failGetPath != "" && name == e.failGetPath) {
		return nil, fmt.Errorf("injected get error")
	}
	return e.Bucket.Get(ctx, name)
}

func (e *errorBucket) IsObjNotFoundErr(err error) bool {
	return e.Bucket.IsObjNotFoundErr(err)
}

func (e *errorBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	if e.failUpload {
		return fmt.Errorf("injected upload error")
	}
	if e.failUploadMeta && strings.HasSuffix(name, "/meta.json") {
		return fmt.Errorf("injected meta upload error")
	}
	return e.Bucket.Upload(ctx, name, r)
}

// trackingBucket wraps an objstore.Bucket and records Get and Iter calls for test assertions.
type trackingBucket struct {
	objstore.Bucket
	mu        sync.Mutex
	getCalls  []string
	iterCalls []string
}

func (t *trackingBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	t.mu.Lock()
	t.iterCalls = append(t.iterCalls, dir)
	t.mu.Unlock()
	return t.Bucket.Iter(ctx, dir, f, options...)
}

func (t *trackingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	t.mu.Lock()
	t.getCalls = append(t.getCalls, name)
	t.mu.Unlock()
	return t.Bucket.Get(ctx, name)
}

// GetCalls returns a copy of all names passed to Get.
func (t *trackingBucket) GetCalls() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.getCalls))
	copy(out, t.getCalls)
	return out
}

// GetIterCalls returns a copy of all dirs passed to Iter.
func (t *trackingBucket) GetIterCalls() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.iterCalls))
	copy(out, t.iterCalls)
	return out
}
