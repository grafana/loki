package v1

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/iter"
	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type failingChunkInput struct {
	v2.Iterator[ChunkRefWithIter]
	err error
}

func TestMergeBuilderRejectsIncompleteBloomInput(t *testing.T) {
	sentinel := errors.New("chunk loading interrupted")
	metrics := NewMetrics(nil)
	tokenizer := NewBloomTokenizer(128<<20, metrics, log.NewNopLogger())
	populate := func(_ *Series, blooms v2.SizedIterator[*Bloom], _ ChunkRefs, out chan *BloomCreation) {
		tokenizer.Populate(blooms, failingChunkInput{v2.NewEmptyIter[ChunkRefWithIter](), sentinel}, out)
	}
	series := &Series{Fingerprint: 1, Chunks: ChunkRefs{{From: 1, Through: 2, Checksum: 3}}}
	merge := NewMergeBuilder(v2.NewEmptyIter[*SeriesWithBlooms](), v2.NewPeekIter(v2.NewSliceIter([]*Series{series})), populate, metrics, log.NewNopLogger())
	writer := NewMemoryBlockWriter(bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	builder, err := NewBlockBuilder(BlockOptions{Schema: NewSchema(CurrentSchemaVersion, compression.None), SeriesPageSize: 100, BloomPageSize: 10 << 10}, writer)
	require.NoError(t, err)
	_, _, err = merge.Build(builder)
	require.ErrorIs(t, err, sentinel, "Incomplete inputs must fail block generation instead of committing chunk coverage")
}

func (i failingChunkInput) Err() error { return i.err }

type failingEntryInput struct {
	iter.EntryIterator
	err error
}

func (i failingEntryInput) Next() bool { return false }
func (i failingEntryInput) Err() error { return i.err }

type failingBloomInput struct {
	v2.SizedIterator[*Bloom]
	err error
}

func (i failingBloomInput) Err() error { return i.err }

func TestBloomTokenizerPropagatesInputErrors(t *testing.T) {
	sentinel := errors.New("input interrupted")
	for _, name := range []string{"chunk loader", "chunk entries", "existing blooms"} {
		t.Run(name, func(t *testing.T) {
			// A nonempty prior Bloom must not hide failure to index new chunks.
			prior := NewBloom()
			prior.Add([]byte("request_id=older-request"))
			var blooms v2.SizedIterator[*Bloom] = v2.NewSliceIter([]*Bloom{prior})
			var chunks v2.Iterator[ChunkRefWithIter] = v2.NewEmptyIter[ChunkRefWithIter]()
			switch name {
			case "chunk loader":
				chunks = failingChunkInput{chunks, sentinel}
			case "chunk entries":
				chunks = v2.NewSliceIter([]ChunkRefWithIter{{Itr: failingEntryInput{err: sentinel}}})
			case "existing blooms":
				blooms = failingBloomInput{blooms, sentinel}
			}
			out := make(chan *BloomCreation)
			go NewBloomTokenizer(128<<20, NewMetrics(nil), log.NewNopLogger()).Populate(blooms, chunks, out)
			var results []*BloomCreation
			for result := range out {
				results = append(results, result)
			}
			require.Len(t, results, 1)
			require.ErrorIs(t, results[0].Err, sentinel)
			require.Nil(t, results[0].Bloom)
		})
	}
}

type errorAfterEntries struct {
	iter.EntryIterator
	err error
}

func (i errorAfterEntries) Err() error { return i.err }

func TestBloomTokenizerRetryExtendsExistingBloom(t *testing.T) {
	prior := NewBloom()
	prior.Add([]byte("request_id=older-request"))
	tokenizer := NewBloomTokenizer(128<<20, NewMetrics(nil), log.NewNopLogger())
	sentinel := errors.New("interrupted after reading an entry")
	for _, failure := range []error{sentinel, nil} {
		entries := iter.NewStreamIterator(logproto.Stream{Entries: []logproto.Entry{{
			Timestamp: time.Unix(1, 0), Line: "synthetic",
			StructuredMetadata: []logproto.LabelAdapter{{Name: "request_id", Value: "new-request"}},
		}}})
		chunks := v2.NewSliceIter([]ChunkRefWithIter{{Ref: ChunkRef{From: 1, Through: 2, Checksum: 3}, Itr: errorAfterEntries{entries, failure}}})
		out := make(chan *BloomCreation)
		go tokenizer.Populate(v2.NewSliceIter([]*Bloom{prior}), chunks, out)
		var results []*BloomCreation
		for result := range out {
			results = append(results, result)
		}
		require.Len(t, results, 1)
		if failure != nil {
			require.ErrorIs(t, results[0].Err, sentinel)
			require.Nil(t, results[0].Bloom)
			continue
		}
		require.NoError(t, results[0].Err)
		require.True(t, results[0].Bloom.Test([]byte("request_id=older-request")))
		require.True(t, results[0].Bloom.Test([]byte("request_id=new-request")))
	}
}

func TestBloomTokenizerReportsFailureAfterEarlierOutput(t *testing.T) {
	prior := NewBloom()
	prior.Add([]byte("request_id=older-request"))
	sentinel := errors.New("later chunk loading failed")
	out := make(chan *BloomCreation)
	go NewBloomTokenizer(128<<20, NewMetrics(nil), log.NewNopLogger()).Populate(
		v2.NewSliceIter([]*Bloom{prior, NewBloom()}),
		failingChunkInput{v2.NewEmptyIter[ChunkRefWithIter](), sentinel}, out)
	var results []*BloomCreation
	for result := range out {
		results = append(results, result)
	}
	require.Len(t, results, 2)
	require.NoError(t, results[0].Err)
	require.True(t, results[0].Bloom.Test([]byte("request_id=older-request")))
	require.ErrorIs(t, results[1].Err, sentinel)
	require.Nil(t, results[1].Bloom)
}
