package deletion

import (
	"crypto/rand"
	"fmt"
	"io"
	"iter"
	"math/big"
	"slices"
	"strings"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// examplesPerKind is the number of log objects, and the number of index
// objects, that a dry run logs as example candidates. An operator checks the
// examples by hand, so the dry run shows both kinds.
const examplesPerKind = 5

// unreachable returns the paths of scanned that reachable does not hold.
// Both must be sorted. The result is sorted, and shares no memory with
// scanned.
func unreachable(scanned, reachable []string) []string {
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
	return result
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

	logObjects   []string
	indexObjects []string
}

// count returns the number of candidates.
func (c candidates) count() int {
	return len(c.logObjects) + len(c.indexObjects)
}

// findCandidates returns the paths that previous yields and that current
// holds, split into log objects and index objects. previous must yield paths
// in strictly increasing order, and current must be sorted.
//
// findCandidates reads previous to the end, even after the last match. gzip
// checks the checksum only at the end of the list. So findCandidates returns
// an error, and no candidates, for a corrupt or truncated list.
func findCandidates(previous iter.Seq2[string, error], current []string) (candidates, error) {
	var (
		result candidates
		i      int
	)
	for p, err := range previous {
		if err != nil {
			return candidates{}, err
		}
		result.previousPaths++
		for i < len(current) && current[i] < p {
			i++
		}
		if i < len(current) && current[i] == p {
			if isIndexObject(p) {
				result.indexObjects = append(result.indexObjects, p)
			} else {
				result.logObjects = append(result.logObjects, p)
			}
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
