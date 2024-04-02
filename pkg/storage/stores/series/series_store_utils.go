package series

import (
	"strings"
	"unicode/utf8"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util"
)

func filterChunksByTime(from, through model.Time, chunks []chunk.Chunk) []chunk.Chunk {
	filtered := make([]chunk.Chunk, 0, len(chunks))
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

func labelNamesFromChunks(chunks []chunk.Chunk) []string {
	var result util.UniqueStrings
	for _, c := range chunks {
		for _, l := range c.Metric {
			if l.Name == model.MetricNameLabel {
				continue
			}

			result.Add(l.Name)
		}
	}
	return result.Strings()
}

func filterChunksByUniqueFingerprint(chunks []chunk.Chunk) []chunk.Chunk {
	filtered := make([]chunk.Chunk, 0, len(chunks))
	uniqueFp := map[model.Fingerprint]struct{}{}

	for _, chunk := range chunks {
		if _, ok := uniqueFp[chunk.FingerprintModel()]; ok {
			continue
		}
		filtered = append(filtered, chunk)
		uniqueFp[chunk.FingerprintModel()] = struct{}{}
	}
	return filtered
}

func filterChunkRefsByUniqueFingerprint(chunks []logproto.ChunkRef) []chunk.Chunk {
	filtered := make([]chunk.Chunk, 0, len(chunks))
	uniqueFp := map[model.Fingerprint]struct{}{}

	for _, c := range chunks {
		if _, ok := uniqueFp[c.FingerprintModel()]; ok {
			continue
		}
		filtered = append(filtered, chunk.Chunk{
			ChunkRef: c,
		})
		uniqueFp[c.FingerprintModel()] = struct{}{}
	}
	return filtered
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
