package deletion

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

func TestUnreachable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		scanned   []string
		reachable []string
		want      []string
	}{
		{"returns no paths when nothing is scanned", nil, []string{"a"}, []string{}},
		{"returns every scanned path when nothing is reachable", []string{"a", "b"}, nil, []string{"a", "b"}},
		{"returns no paths when every scanned path is reachable", []string{"a", "b"}, []string{"a", "b"}, []string{}},
		{"returns the scanned paths that are not reachable", []string{"a", "b", "c", "d"}, []string{"b", "d"}, []string{"a", "c"}},
		{"ignores reachable paths that were not scanned", []string{"b", "d"}, []string{"a", "c", "d", "e"}, []string{"b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := unreachable(tc.scanned, tc.reachable)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("returns an error when the scanned paths are not sorted", func(t *testing.T) {
		_, err := unreachable([]string{"b", "a"}, nil)
		require.ErrorContains(t, err, "scanned paths are not sorted")
	})

	t.Run("returns an error when the reachable paths are not sorted", func(t *testing.T) {
		_, err := unreachable([]string{"a"}, []string{"b", "a"})
		require.ErrorContains(t, err, "reachable paths are not sorted")
	})
}

func TestFindCandidates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stored  []string
		current []string
		want    []string
	}{
		{"returns no candidates when both lists are empty", nil, nil, nil},
		{"returns no candidates when the stored list is empty", nil, []string{"a", "b"}, nil},
		{"returns no candidates when the new list is empty", []string{"a", "b"}, nil, nil},
		{"returns no candidates when the lists do not overlap", []string{"a", "c"}, []string{"b", "d"}, nil},
		{"returns every path when the lists are equal", []string{"a", "b", "c"}, []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"returns the shared paths of interleaved lists", []string{"a", "b", "d", "f"}, []string{"b", "c", "d", "e"}, []string{"b", "d"}},
		{"returns the shared paths when the new list ends first", []string{"b", "x", "y", "z"}, []string{"a", "b"}, []string{"b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cands, err := findCandidates(seqOf(tc.stored, nil), tc.current)
			require.NoError(t, err)
			require.Equal(t, tc.want, cands.logObjects)
			require.Empty(t, cands.indexObjects)
			require.Equal(t, len(tc.stored), cands.previousPaths)
		})
	}

	t.Run("splits the candidates into log objects and index objects", func(t *testing.T) {
		paths := []string{"index/v0/indexes/00/x", "index/v0/indexes/tenants/a/bb/y", "objects/00/z"}

		cands, err := findCandidates(seqOf(paths, nil), paths)
		require.NoError(t, err)
		require.Equal(t, []string{"objects/00/z"}, cands.logObjects)
		require.Equal(t, []string{"index/v0/indexes/00/x", "index/v0/indexes/tenants/a/bb/y"}, cands.indexObjects)
		require.Equal(t, 3, cands.count())
	})

	t.Run("returns the error at the end of the stored list after the last match", func(t *testing.T) {
		errAtEnd := errors.New("checksum mismatch")

		cands, err := findCandidates(seqOf([]string{"a", "b", "c"}, errAtEnd), []string{"a"})
		require.ErrorIs(t, err, errAtEnd)
		require.Zero(t, cands.count())
	})

	t.Run("returns an error when the current paths are not sorted", func(t *testing.T) {
		_, err := findCandidates(seqOf([]string{"a"}, nil), []string{"b", "a"})
		require.ErrorContains(t, err, "current paths are not sorted")
	})

	t.Run("returns no candidates and an error when the stored paths are not in increasing order", func(t *testing.T) {
		cands, err := findCandidates(seqOf([]string{"a", "c", "b"}, nil), []string{"a", "b", "c"})
		require.ErrorContains(t, err, `previous paths are not in strictly increasing order: "b" is not after "c"`)
		require.Zero(t, cands.count())
	})

	t.Run("returns no candidates and an error when the stored paths repeat", func(t *testing.T) {
		cands, err := findCandidates(seqOf([]string{"a", "a"}, nil), []string{"a"})
		require.ErrorContains(t, err, "not in strictly increasing order")
		require.Zero(t, cands.count())
	})

	t.Run("returns no candidates and the error when the stored list fails partway", func(t *testing.T) {
		errPartway := errors.New("read failed")

		cands, err := findCandidates(seqOf([]string{"a"}, errPartway), []string{"a", "b"})
		require.ErrorIs(t, err, errPartway)
		require.Zero(t, cands.count())
	})
}

func TestCandidates_Examples(t *testing.T) {
	t.Run("returns every candidate of each kind sorted when each kind has five or fewer", func(t *testing.T) {
		cands := candidates{logObjects: []string{"objects/00/a", "objects/00/b"}, indexObjects: []string{"index/v0/indexes/00/x"}}

		logObjects, indexObjects, err := cands.examples(rand.Reader)
		require.NoError(t, err)
		require.Equal(t, []string{"objects/00/a", "objects/00/b"}, logObjects)
		require.Equal(t, []string{"index/v0/indexes/00/x"}, indexObjects)
	})

	t.Run("returns five sorted candidates of each kind when each kind has more", func(t *testing.T) {
		var cands candidates
		for i := range 20 {
			cands.indexObjects = append(cands.indexObjects, fmt.Sprintf("index/v0/indexes/%02d/x", i))
			cands.logObjects = append(cands.logObjects, fmt.Sprintf("objects/%02d/x", i))
		}

		logObjects, indexObjects, err := cands.examples(rand.Reader)
		require.NoError(t, err)
		for kind, examples := range map[string][]string{"log": logObjects, "index": indexObjects} {
			require.Len(t, examples, 5, kind)
			require.True(t, slices.IsSorted(examples), kind)
			require.Len(t, slices.Compact(slices.Clone(examples)), 5, kind)
		}
		for _, p := range logObjects {
			require.Contains(t, cands.logObjects, p)
		}
		for _, p := range indexObjects {
			require.Contains(t, cands.indexObjects, p)
		}
	})

	t.Run("returns an error when the random source fails", func(t *testing.T) {
		cands := candidates{logObjects: []string{"objects/00/a", "objects/00/b", "objects/00/c", "objects/00/d", "objects/00/e", "objects/00/f"}}

		_, _, err := cands.examples(failingReader{})
		require.ErrorContains(t, err, "picking random examples")
	})
}

func TestLogCandidates(t *testing.T) {
	modified := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	newLogger := func() (log.Logger, *bytes.Buffer) {
		var logs bytes.Buffer
		return log.NewLogfmtLogger(log.NewSyncWriter(&logs)), &logs
	}

	t.Run("logs every candidate with kind, last modification time and size, index objects first, when each kind has five or fewer", func(t *testing.T) {
		bucket := newFileBucket(t)
		bucket.put(t, "index/v0/indexes/tenants/a/00/orphan-index", []byte("12345"), modified)
		bucket.put(t, "objects/tenants/a/01/orphan", []byte("123"), modified)
		logger, logs := newLogger()

		LogCandidates(t.Context(), bucket, []string{"index/v0/indexes/tenants/a/00/orphan-index", "objects/tenants/a/01/orphan"}, logger)
		require.Equal(t, `level=info msg="deletion sweep candidate" kind=index path=index/v0/indexes/tenants/a/00/orphan-index last_modified=2026-11-04T10:00:00Z size=5`+"\n"+
			`level=info msg="deletion sweep candidate" kind=log path=objects/tenants/a/01/orphan last_modified=2026-11-04T10:00:00Z size=3`+"\n", logs.String())
	})

	t.Run("logs five examples of each kind when each kind has more", func(t *testing.T) {
		bucket := newFileBucket(t)
		var paths []string
		for i := range 8 {
			for _, p := range []string{fmt.Sprintf("objects/tenants/a/aa/log-%d", i), fmt.Sprintf("index/v0/indexes/tenants/a/bb/index-%d", i)} {
				bucket.put(t, p, []byte("x"), modified)
				paths = append(paths, p)
			}
		}
		slices.Sort(paths)
		logger, logs := newLogger()

		LogCandidates(t.Context(), bucket, paths, logger)
		require.Equal(t, 5, strings.Count(logs.String(), `level=info msg="deletion sweep candidate" kind=log`))
		require.Equal(t, 5, strings.Count(logs.String(), `level=info msg="deletion sweep candidate" kind=index`))
		require.Equal(t, 10, strings.Count(logs.String(), "\n"))
	})

	t.Run("logs a warning with the error for each candidate whose attributes cannot be read", func(t *testing.T) {
		bucket := &failingAttributesBucket{fileBucket: newFileBucket(t)}
		logger, logs := newLogger()

		LogCandidates(t.Context(), bucket, []string{"objects/tenants/a/00/orphan", "objects/tenants/a/01/orphan"}, logger)
		require.Equal(t, `level=warn msg="deletion sweep candidate" kind=log path=objects/tenants/a/00/orphan err="attributes failed"`+"\n"+
			`level=warn msg="deletion sweep candidate" kind=log path=objects/tenants/a/01/orphan err="attributes failed"`+"\n", logs.String())
	})

	t.Run("logs nothing when there are no paths", func(t *testing.T) {
		logger, logs := newLogger()

		LogCandidates(t.Context(), newFileBucket(t), nil, logger)
		require.Empty(t, logs.String())
	})
}

// failingAttributesBucket fails every Attributes call.
type failingAttributesBucket struct {
	fileBucket
}

func (failingAttributesBucket) Attributes(context.Context, string) (objstore.ObjectAttributes, error) {
	return objstore.ObjectAttributes{}, errors.New("attributes failed")
}

func TestSample(t *testing.T) {
	paths := make([]string, 100)
	for i := range paths {
		paths[i] = fmt.Sprintf("objects/%03d/x", i)
	}

	t.Run("returns a copy of every path when there are n or fewer", func(t *testing.T) {
		in := []string{"a", "b"}
		out, err := sample(rand.Reader, in, 5)
		require.NoError(t, err)
		require.Equal(t, in, out)
		out[0] = "changed"
		require.Equal(t, "a", in[0])
	})

	t.Run("returns n distinct sorted paths of the input when there are more", func(t *testing.T) {
		out, err := sample(rand.Reader, paths, 5)
		require.NoError(t, err)
		require.Len(t, out, 5)
		require.True(t, slices.IsSorted(out))
		require.Len(t, slices.Compact(slices.Clone(out)), 5)
		for _, p := range out {
			require.Contains(t, paths, p)
		}
	})

	t.Run("picks each path about equally often", func(t *testing.T) {
		const runs = 10000
		hits := make(map[string]int, len(paths))
		for range runs {
			out, err := sample(rand.Reader, paths, 5)
			require.NoError(t, err)
			for _, p := range out {
				hits[p]++
			}
		}
		// Each path has a chance of 5 in 100 per run, so it is picked about
		// 500 times, with a standard deviation of about 22. The bounds are
		// about seven standard deviations wide, so only a biased sample fails.
		for _, p := range paths {
			require.InDelta(t, 500, hits[p], 150, p)
		}
	})

	t.Run("returns an error when the random source fails", func(t *testing.T) {
		_, err := sample(failingReader{}, paths, 5)
		require.ErrorContains(t, err, "picking random examples")
	})
}

// seqOf yields paths, then err if it is not nil.
func seqOf(paths []string, err error) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		for _, p := range paths {
			if !yield(p, nil) {
				return
			}
		}
		if err != nil {
			yield("", err)
		}
	}
}
