package series

import (
	"strings"
	"unicode/utf8"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk/encoding"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/util"
)

func filterChunksByTime(from, through model.Time, chunks []encoding.Chunk) []encoding.Chunk {
	filtered := make([]encoding.Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.Through < from || through < chunk.From {
			continue
		}
		filtered = append(filtered, chunk)
	}
	return filtered
}

func filterChunkRefsByTime(from, through model.Time, chunks []logproto.ChunkRef) []logproto.ChunkRef {
	filtered := make([]logproto.ChunkRef, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.Through < from || through < chunk.From {
			continue
		}
		filtered = append(filtered, chunk)
	}
	return filtered
}

func labelNamesFromChunks(chunks []encoding.Chunk) []string {
	var result util.UniqueStrings
	for _, c := range chunks {
		for _, l := range c.Metric {
			result.Add(l.Name)
		}
	}
	return result.Strings()
}

func filterChunksByUniqueFingerprint(s config.SchemaConfig, chunks []encoding.Chunk) ([]encoding.Chunk, []string) {
	filtered := make([]encoding.Chunk, 0, len(chunks))
	keys := make([]string, 0, len(chunks))
	uniqueFp := map[model.Fingerprint]struct{}{}

	for _, chunk := range chunks {
		if _, ok := uniqueFp[chunk.FingerprintModel()]; ok {
			continue
		}
		filtered = append(filtered, chunk)
		keys = append(keys, s.ExternalKey(chunk.ChunkRef))
		uniqueFp[chunk.FingerprintModel()] = struct{}{}
	}
	return filtered, keys
}

func uniqueStrings(cs []string) []string {
	if len(cs) == 0 {
		return []string{}
	}

	result := make([]string, 1, len(cs))
	result[0] = cs[0]
	i, j := 0, 1
	for j < len(cs) {
		if result[i] == cs[j] {
			j++
			continue
		}
		result = append(result, cs[j])
		i++
		j++
	}
	return result
}

func intersectStrings(left, right []string) []string {
	var (
		i, j   = 0, 0
		result = []string{}
	)
	for i < len(left) && j < len(right) {
		if left[i] == right[j] {
			result = append(result, left[i])
		}

		if left[i] < right[j] {
			i++
		} else {
			j++
		}
	}
	return result
}

//nolint:unused //Ignoring linting as this might be useful
func nWayIntersectStrings(sets [][]string) []string {
	l := len(sets)
	switch l {
	case 0:
		return []string{}
	case 1:
		return sets[0]
	case 2:
		return intersectStrings(sets[0], sets[1])
	default:
		var (
			split = l / 2
			left  = nWayIntersectStrings(sets[:split])
			right = nWayIntersectStrings(sets[split:])
		)
		return intersectStrings(left, right)
	}
}

// Bitmap used by func isRegexMetaCharacter to check whether a character needs to be escaped.
var regexMetaCharacterBytes [16]byte

// isRegexMetaCharacter reports whether byte b needs to be escaped.
func isRegexMetaCharacter(b byte) bool {
	return b < utf8.RuneSelf && regexMetaCharacterBytes[b%16]&(1<<(b/16)) != 0
}

func init() {
	for _, b := range []byte(`.+*?()|[]{}^$`) {
		regexMetaCharacterBytes[b%16] |= 1 << (b / 16)
	}
}

// FindSetMatches returns list of values that can be equality matched on.
// copied from Prometheus querier.go, removed check for Prometheus wrapper.
func FindSetMatches(pattern string) []string {
	escaped := false
	sets := []*strings.Builder{{}}
	for i := 0; i < len(pattern); i++ {
		if escaped {
			switch {
			case isRegexMetaCharacter(pattern[i]):
				sets[len(sets)-1].WriteByte(pattern[i])
			case pattern[i] == '\\':
				sets[len(sets)-1].WriteByte('\\')
			default:
				return nil
			}
			escaped = false
		} else {
			switch {
			case isRegexMetaCharacter(pattern[i]):
				if pattern[i] == '|' {
					sets = append(sets, &strings.Builder{})
				} else {
					return nil
				}
			case pattern[i] == '\\':
				escaped = true
			default:
				sets[len(sets)-1].WriteByte(pattern[i])
			}
		}
	}
	matches := make([]string, 0, len(sets))
	for _, s := range sets {
		if s.Len() > 0 {
			matches = append(matches, s.String())
		}
	}
	return matches
}

// Using this function avoids logging of nil matcher, which works, but indirectly via panic and recover.
// That confuses attached debugger, which wants to breakpoint on each panic.
// Using simple check is also faster.
func formatMatcher(matcher *labels.Matcher) string {
	if matcher == nil {
		return "nil"
	}
	return matcher.String()
}
