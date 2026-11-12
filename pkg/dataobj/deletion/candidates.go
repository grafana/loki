package deletion

import (
	"iter"
	"math/rand/v2"
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
// only. examples picks them at random with rng instead, so that repeated
// sweeps show different candidates.
func (c candidates) examples(rng *rand.Rand) (logObjects, indexObjects []string) {
	logObjects = sample(rng, c.logObjects, examplesPerKind)
	indexObjects = sample(rng, c.indexObjects, examplesPerKind)
	slices.Sort(logObjects)
	slices.Sort(indexObjects)
	return logObjects, indexObjects
}

// sample returns n random elements of paths, or a copy of paths if it holds
// n or fewer. It uses memory for n elements only, whatever the length of
// paths.
func sample(rng *rand.Rand, paths []string, n int) []string {
	result := make([]string, 0, min(n, len(paths)))
	for i, p := range paths {
		switch {
		case i < n:
			result = append(result, p)
		default:
			if j := rng.IntN(i + 1); j < n {
				result[j] = p
			}
		}
	}
	return result
}

func isIndexObject(p string) bool {
	return strings.HasPrefix(p, metastore.IndexStoragePrefix+"/")
}
