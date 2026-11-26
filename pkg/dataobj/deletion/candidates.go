package deletion

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"iter"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// examplesPerKind is the number of log objects, and the number of index
// objects, that a dry run logs as example candidates. An operator checks the
// examples by hand, so the dry run shows both kinds.
const examplesPerKind = 5

// unreachable returns the paths of scanned that reachable does not hold.
// The result is sorted, and shares no memory with scanned.
//
// It returns an error if scanned or reachable is not sorted. The merge would
// then miss reachable paths and return them as unreachable.
func unreachable(scanned, reachable []string) ([]string, error) {
	if !slices.IsSorted(scanned) {
		return nil, errors.New("scanned paths are not sorted")
	}
	if !slices.IsSorted(reachable) {
		return nil, errors.New("reachable paths are not sorted")
	}
	result := make([]string, 0, len(scanned))
	i := 0
	for _, p := range scanned {
		for i < len(reachable) && reachable[i] < p {
			i++
		}
		if i < len(reachable) && reachable[i] == p {
			continue
		}
		result = append(result, p)
	}
	return result, nil
}

// candidates are the deletion candidates of a sweep: the objects in both the
// previous file list and the new file list. Both lists hold the objects that
// no ToC could reach. So each candidate was unreachable in two sweeps, at
// least MinSweepInterval apart.
//
// The paths are sorted and relative to the root of the data object storage.
type candidates struct {
	// previousPaths is the number of paths in the previous file list.
	previousPaths int
	// previousCreatedAt is the creation time of the previous file list.
	previousCreatedAt time.Time

	logObjects   []string
	indexObjects []string
}

// count returns the number of candidates.
func (c candidates) count() int {
	return len(c.logObjects) + len(c.indexObjects)
}

// add adds p to the log objects or the index objects. The caller adds the
// paths in sorted order, so that each kind stays sorted.
func (c *candidates) add(p string) {
	if isIndexObject(p) {
		c.indexObjects = append(c.indexObjects, p)
	} else {
		c.logObjects = append(c.logObjects, p)
	}
}

// findCandidates returns the paths that previous yields and that current
// holds, split into log objects and index objects.
//
// It returns an error, and no candidates, if previous does not yield paths in
// strictly increasing order or current is not sorted. The merge would then
// miss candidates. previous streams from the stored list, so findCandidates
// checks its order while it reads.
//
// findCandidates reads previous to the end, even after the last match. gzip
// checks the checksum only at the end of the list. So findCandidates returns
// an error, and no candidates, for a corrupt or truncated list.
func findCandidates(previous iter.Seq2[string, error], current []string) (candidates, error) {
	if !slices.IsSorted(current) {
		return candidates{}, errors.New("current paths are not sorted")
	}
	var (
		result candidates
		i      int
		last   string
	)
	for p, err := range previous {
		if err != nil {
			return candidates{}, err
		}
		if result.previousPaths > 0 && p <= last {
			return candidates{}, fmt.Errorf("previous paths are not in strictly increasing order: %q is not after %q", p, last)
		}
		last = p
		result.previousPaths++
		for i < len(current) && current[i] < p {
			i++
		}
		if i < len(current) && current[i] == p {
			result.add(p)
			i++
		}
	}
	return result, nil
}

// examples returns up to examplesPerKind log objects and up to
// examplesPerKind index objects of the candidates, each sorted. The
// candidates are sorted, so the first ones come from a few hash prefixes
// only. examples reads random to pick them at random instead, so that
// repeated sweeps show different candidates.
func (c candidates) examples(random io.Reader) (logObjects, indexObjects []string, err error) {
	logObjects, err = sample(random, c.logObjects, examplesPerKind)
	if err != nil {
		return nil, nil, err
	}
	indexObjects, err = sample(random, c.indexObjects, examplesPerKind)
	if err != nil {
		return nil, nil, err
	}
	return logObjects, indexObjects, nil
}

// LogCandidates logs up to examplesPerKind random log objects and up to
// examplesPerKind random index objects of paths. Each line holds the last
// modification time and size of the object. An operator uses the lines to
// check the deletion candidates of a dry run by hand.
//
// paths are sorted and relative to the root of bucket, for example the
// SweepResult.Candidates of a sweep. LogCandidates logs nothing for empty
// paths.
//
// LogCandidates logs an error instead of returning it. The sweep already
// stored its file list, and the lines are only for a manual check. So an
// error here must not fail the sweep.
func LogCandidates(ctx context.Context, bucket objstore.BucketReader, paths []string, logger log.Logger) {
	var cands candidates
	for _, p := range paths {
		cands.add(p)
	}
	logObjects, indexObjects, err := cands.examples(rand.Reader)
	if err != nil {
		level.Warn(logger).Log("msg", "deletion sweep cannot pick example candidates", "err", err)
		return
	}
	logExamples(ctx, bucket, logger, kindIndex, indexObjects)
	logExamples(ctx, bucket, logger, kindLog, logObjects)
}

// logExamples logs one line for each path, with its last modification time
// and size.
func logExamples(ctx context.Context, bucket objstore.BucketReader, logger log.Logger, kind string, paths []string) {
	for _, p := range paths {
		attrs, err := bucket.Attributes(ctx, p)
		if err != nil {
			level.Warn(logger).Log("msg", "deletion sweep candidate", "kind", kind, "path", p, "err", err)
			continue
		}
		level.Info(logger).Log(
			"msg", "deletion sweep candidate",
			"kind", kind,
			"path", p,
			"last_modified", attrs.LastModified.UTC().Format(time.RFC3339),
			"size", attrs.Size,
		)
	}
}

// sample returns n distinct random elements of the sorted paths, or a copy
// of paths if it holds n or fewer. The result is sorted.
//
// sample uses Floyd's algorithm, so it reads n random numbers from random,
// whatever the length of paths.
func sample(random io.Reader, paths []string, n int) ([]string, error) {
	if len(paths) <= n {
		return slices.Clone(paths), nil
	}
	picked := make(map[int]struct{}, n)
	for j := len(paths) - n; j < len(paths); j++ {
		r, err := rand.Int(random, big.NewInt(int64(j+1)))
		if err != nil {
			return nil, fmt.Errorf("picking random examples: %w", err)
		}
		i := int(r.Int64())
		if _, ok := picked[i]; ok {
			i = j
		}
		picked[i] = struct{}{}
	}
	indexes := make([]int, 0, n)
	for i := range picked {
		indexes = append(indexes, i)
	}
	slices.Sort(indexes)
	result := make([]string, 0, n)
	for _, i := range indexes {
		result = append(result, paths[i])
	}
	return result, nil
}

func isIndexObject(p string) bool {
	return strings.HasPrefix(p, metastore.IndexStoragePrefix+"/")
}
