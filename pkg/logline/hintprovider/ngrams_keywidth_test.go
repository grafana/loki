package hintprovider

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	v3 "github.com/grafana/loki/v3/pkg/logline/internal/v3"
)

// builderDictKey mirrors the writer: copy(key[:], term[:NgramLength]).
func builderDictKey(extracted [8]byte) [v3.NgramLength]byte {
	var key [v3.NgramLength]byte
	copy(key[:], extracted[:v3.NgramLength])
	return key
}

// readerDictKey mirrors IndexReader.FindTerm, which zero-pads or truncates the
// term into a fixed-width key.
func readerDictKey(term string) [v3.NgramLength]byte {
	var key [v3.NgramLength]byte
	copy(key[:], term)
	return key
}

// readerKey8 mirrors filterNgramsForShard, which hashes the term as an [8]byte.
func readerKey8(term string) [8]byte {
	var key [8]byte
	copy(key[:], term)
	return key
}

// TestExtractQueryNgrams_ResolvesToBuilderDictKeys is the invariant a lookup
// depends on: every query term must resolve to a dictionary key the builder
// wrote, and every key the builder wrote must be reachable from a query term.
func TestExtractQueryNgrams_ResolvesToBuilderDictKeys(t *testing.T) {
	inputs := []string{
		"level=info msg=hello",
		"tenant=836243 status=500",
		"id=1779629282473030489",
		"ts=2026-08-12T12:39:34.933471853Z caller=foo.go:12",
		"user=abc123456789 path=/api/v1/query",
	}

	for _, version := range logline.AllVersions() {
		extract, err := logline.ExtractorForVersion(version)
		require.NoError(t, err)

		for n := 1; n <= 6; n++ {
			for _, in := range inputs {
				t.Run(fmt.Sprintf("%s/n=%d/%.20s", version, n, in), func(t *testing.T) {
					built := map[[v3.NgramLength]byte]struct{}{}
					for _, k := range extract(n, in, nil, nil, nil) {
						built[builderDictKey(k)] = struct{}{}
					}

					terms, err := ExtractQueryNgrams(in, n, version)
					require.NoError(t, err)

					queried := map[[v3.NgramLength]byte]struct{}{}
					for _, term := range terms {
						wantWidth := n
						if logline.IsPackedTermKey(readerKey8(term)) {
							wantWidth = 8
						}
						require.Len(t, term, wantWidth,
							"a packed term is passed whole and a text term sliced to ngram_length")

						k := readerDictKey(term)
						_, ok := built[k]
						require.True(t, ok,
							"query term %q resolves to key %v, which the builder never wrote", term, k)
						queried[k] = struct{}{}
					}
					require.Equal(t, len(built), len(queried),
						"every dictionary key the builder wrote must be reachable from a query term")
				})
			}
		}
	}
}

// TestExtractQueryNgrams_V4NumericSurvivesShortNgramLength pins the case that
// motivates passing packed keys whole: a v4 numeric key fills all six dictionary
// bytes, so slicing it to a smaller ngram_length drops low value bytes.
func TestExtractQueryNgrams_V4NumericSurvivesShortNgramLength(t *testing.T) {
	// Not a multiple of 256, so a dropped low byte changes the key.
	const needle = "933471853"

	extract, err := logline.ExtractorForVersion("v4")
	require.NoError(t, err)
	want := builderDictKey(extract(6, needle, nil, nil, nil)[0])

	for n := 1; n <= 6; n++ {
		terms, err := ExtractQueryNgrams(needle, n, "v4")
		require.NoError(t, err)
		require.Len(t, terms, 1, "n=%d", n)
		require.Equal(t, want, readerDictKey(terms[0]),
			"numeric term must resolve to the same dictionary key at ngram_length %d", n)
	}
}

// TestExtractQueryNgrams_TextTermsAreNotWidened keeps the text path byte-exact:
// a text term stays ngram_length bytes with no padding.
func TestExtractQueryNgrams_TextTermsAreNotWidened(t *testing.T) {
	for _, version := range logline.AllVersions() {
		for n := 1; n <= 6; n++ {
			terms, err := ExtractQueryNgrams("level=info msg=hello world", n, version)
			require.NoError(t, err)
			require.NotEmpty(t, terms)
			for _, term := range terms {
				require.Len(t, term, n)
				require.NotContains(t, term, "\x00")
			}
		}
	}
}
