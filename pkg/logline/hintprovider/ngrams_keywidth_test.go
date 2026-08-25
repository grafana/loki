package hintprovider

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	v3 "github.com/grafana/loki/v3/pkg/logline/internal/v3"
)

// builderDictKey mirrors what the writer stores for an extracted key:
// copy(key[:], term[:NgramLength]) in streaming_index_writer.go.
func builderDictKey(extracted [8]byte) [v3.NgramLength]byte {
	var key [v3.NgramLength]byte
	copy(key[:], extracted[:v3.NgramLength])
	return key
}

// readerDictKey mirrors IndexReader.FindTerm: the query term is copied into a
// zero-valued [NgramLength]byte, so a short term is zero-padded.
func readerDictKey(term string) [v3.NgramLength]byte {
	var key [v3.NgramLength]byte
	copy(key[:], term)
	return key
}

// readerKey8 mirrors filterNgramsForShard, which copies the term into a
// zero-valued [8]byte before hashing it.
func readerKey8(term string) [8]byte {
	var key [8]byte
	copy(key[:], term)
	return key
}

// TestExtractQueryNgrams_ResolvesToBuilderDictKeys is the invariant that makes
// a lookup work at all: every term the query path derives must resolve, through
// FindTerm's zero-padding, to a dictionary key the builder actually wrote, and
// every key the builder wrote must be reachable from some query term.
//
// It runs across every ngram length because the term key width is a property of
// the format, not of ngram_length. The two are equal for v3 at the production
// setting but not for an extractor that uses the full key, as v4 does for packed
// numeric grams. Text grams keep their ngram_length slice, so this also proves
// widening packed keys left the text path untouched.
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
		keyLength, err := logline.TermKeyLengthForVersion(version)
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
						// A text gram keeps its ngram_length bytes. Only a packed
						// key is widened, because only it carries value bytes past
						// ngram_length.
						wantWidth := n
						if logline.IsPackedTermKey(readerKey8(term)) {
							wantWidth = keyLength
						}
						require.Len(t, term, wantWidth,
							"a packed term must be sliced to the term key width and a text term to ngram_length")
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
// separates the two widths. A v4 numeric key occupies all 6 bytes, so slicing a
// term by an ngram_length below 6 would drop the low value bytes and the lookup
// would resolve to a different key.
func TestExtractQueryNgrams_V4NumericSurvivesShortNgramLength(t *testing.T) {
	// Deliberately not a multiple of 256, so a dropped low byte changes the key.
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

// TestExtractQueryNgrams_V3UnchangedByKeyWidthSlicing shows the two widths are
// interchangeable for v3: its unused tail bytes are already zero, so slicing to
// n and slicing to the key width resolve to the same dictionary key.
func TestExtractQueryNgrams_V3UnchangedByKeyWidthSlicing(t *testing.T) {
	for n := 1; n <= 6; n++ {
		terms, err := ExtractQueryNgrams("level=info msg=hello world", n, "v3")
		require.NoError(t, err)
		for _, term := range terms {
			// Slicing to n and to the key width must resolve identically.
			require.Equal(t, readerDictKey(term[:n]), readerDictKey(term),
				"v3 term %q must resolve the same whether sliced to n or to the key width", term)
		}
	}
}

func TestTermKeyLengthForVersion(t *testing.T) {
	for _, v := range logline.AllVersions() {
		got, err := logline.TermKeyLengthForVersion(v)
		require.NoError(t, err)
		require.Equal(t, v3.NgramLength, got)
	}
	_, err := logline.TermKeyLengthForVersion("v99")
	require.Error(t, err)
}
