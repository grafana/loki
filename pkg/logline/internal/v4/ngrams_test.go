package v4

import (
	"testing"

	"github.com/grafana/loki/pkg/push"
	"github.com/stretchr/testify/require"

	v3 "github.com/grafana/loki/v3/pkg/logline/internal/v3"
)

func textGrams(t *testing.T, keys [][8]byte, n int) []string {
	t.Helper()
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if k[0] == numericTag {
			continue
		}
		out = append(out, string(k[:n]))
	}
	return out
}

func numericKeys(keys [][8]byte) [][8]byte {
	out := make([][8]byte, 0, len(keys))
	for _, k := range keys {
		if k[0] == numericTag {
			out = append(out, k)
		}
	}
	return out
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// TestExtractFeatures_TextParityWithV3 locks the text side of v4 to v3: for
// input with no all-digit window, the emitted grams must be byte-identical.
//
// DO NOT update these expected values. A failure means the text algorithm has
// changed, which would invalidate every v4 index already written.
func TestExtractFeatures_TextParityWithV3(t *testing.T) {
	tests := []struct {
		name     string
		n        int
		text     string
		expected []string
	}{
		{"simple word n=6", 6, "abcdefgh", []string{"ABCDEF", "BCDEFG", "CDEFGH"}},
		{"punctuation normalization n=6", 6, "a_b-c/d:e@f%g?h.i", []string{
			"A.B.C.", ".B.C.D", "B.C.D.", ".C.D.E", "C.D.E.", ".D.E.F",
			"D.E.F.", ".E.F.G", "E.F.G.", ".F.G.H", "F.G.H.", ".G.H.I"}},
		{"separator splitting n=6", 6, "abcdefg,hijklmn", []string{"ABCDEF", "BCDEFG", "HIJKLM", "IJKLMN"}},
		{"space is not a separator n=6", 6, "foo bar", []string{"FOO BA", "OO BAR"}},
		{"path with slashes n=6", 6, "/api/v1/users", []string{
			".API.V", "API.V1", "PI.V1.", "I.V1.U", ".V1.US", "V1.USE", "1.USER", ".USERS"}},
		{"short tokens skipped n=6", 6, "abcde,fghijk,xy", []string{"FGHIJK"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractFeatures(tt.n, tt.text, nil, nil, nil)
			require.Equal(t, tt.expected, textGrams(t, got, tt.n))
			require.Empty(t, numericKeys(got), "no digit run of %d, so no numeric grams", NumericNgramLength)

			v3out := v3.ExtractFeatures(tt.n, tt.text, nil, nil, nil)
			v3strs := make([]string, len(v3out))
			for i, k := range v3out {
				v3strs[i] = string(k[:tt.n])
			}
			require.Equal(t, v3strs, textGrams(t, got, tt.n), "v4 text grams must equal v3")
		})
	}
}

// TestExtractFeatures_NumericRule covers both halves of the numeric rule: an
// all-digit window of length n is never emitted, and every window of
// NumericNgramLength digits is emitted as a packed key.
//
// wantNumeric lists the digit runs expected to be packed, in order. wantNoGrams
// marks input that yields nothing at all, which is what makes a short numeric
// needle fall through to a full Loki scan rather than narrowing on a term that
// was never indexed.
func TestExtractFeatures_NumericRule(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		wantNumeric []string
		wantNoGrams bool
	}{
		{
			name:        "six digits are below the numeric length and carry no text gram",
			text:        "471853",
			wantNoGrams: true,
		},
		{
			name:        "eight digits are still below the numeric length",
			text:        "12345678",
			wantNoGrams: true,
		},
		{
			name:        "exactly the numeric length yields one key",
			text:        "123456789",
			wantNumeric: []string{"123456789"},
		},
		{
			name:        "one window per position in a longer run",
			text:        "1234567890",
			wantNumeric: []string{"123456789", "234567890"},
		},
		{
			name:        "twelve digits",
			text:        "123456789012",
			wantNumeric: []string{"123456789", "234567890", "345678901", "456789012"},
		},
		{
			name: "nanosecond timestamp indexes only its fraction",
			text: "ts=2026-08-12T12:39:34.933471853Z",
			// 2026, 08, 12, 12, 39 and 34 are all shorter than the numeric
			// length, so the sub-second fraction is the only numeric term.
			wantNumeric: []string{"933471853"},
		},
		{
			name: "nineteen digit id",
			text: "1779629282473030489",
			wantNumeric: []string{
				"177962928", "779629282", "796292824", "962928247", "629282473",
				"292824730", "928247303", "282473030", "824730304", "247303048",
				"473030489",
			},
		},
		{
			name:        "digits embedded in text keep their straddling grams",
			text:        "user=abc123456789",
			wantNumeric: []string{"123456789"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractFeatures(6, tt.text, nil, nil, nil)

			if tt.wantNoGrams {
				require.Empty(t, got, "must emit nothing so the query falls through")
				return
			}

			want := make([][8]byte, len(tt.wantNumeric))
			for i, d := range tt.wantNumeric {
				want[i] = packNumeric(d)
			}
			require.Equal(t, want, numericKeys(got))

			for _, g := range textGrams(t, got, 6) {
				require.False(t, allDigits(g), "all-digit gram %q must not be emitted as text", g)
			}
		})
	}
}

// TestExtractFeatures_QueryFindsWhatTheBuilderWrote checks that looking up a
// digit run on its own resolves to the same key the builder emitted for the
// surrounding line.
func TestExtractFeatures_QueryFindsWhatTheBuilderWrote(t *testing.T) {
	line := ExtractFeatures(6, "ts=2026-08-12T12:39:34.933471853Z", nil, nil, nil)
	needle := ExtractFeatures(6, "933471853", nil, nil, nil)
	require.Equal(t, numericKeys(line), numericKeys(needle))
}

// TestPackNumeric_Layout locks the key encoding: tag byte, 40-bit big-endian
// payload, bytes 6-7 zero (radixSortByNgram assumes that).
func TestPackNumeric_Layout(t *testing.T) {
	k := packNumeric("000000000")
	require.Equal(t, [8]byte{numericTag, 0, 0, 0, 0, 0, 0, 0}, k)

	k = packNumeric("000000001")
	require.Equal(t, [8]byte{numericTag, 0, 0, 0, 0, 1, 0, 0}, k)

	k = packNumeric("999999999")
	require.Equal(t, byte(numericTag), k[0])
	require.Equal(t, byte(0), k[6], "byte 6 must stay zero for the radix sort")
	require.Equal(t, byte(0), k[7], "byte 7 must stay zero for the radix sort")
	v := uint64(k[1])<<32 | uint64(k[2])<<24 | uint64(k[3])<<16 | uint64(k[4])<<8 | uint64(k[5])
	require.Equal(t, uint64(999999999), v)
}

// TestPackNumeric_DistinctValuesDistinctKeys guards against two different digit
// runs sharing a key, which would merge unrelated postings.
func TestPackNumeric_DistinctValuesDistinctKeys(t *testing.T) {
	seen := map[[8]byte]string{}
	for _, d := range []string{
		"000000000", "000000001", "100000000", "999999999",
		"123456789", "987654321", "933471853", "471853000",
	} {
		k := packNumeric(d)
		prev, dup := seen[k]
		require.False(t, dup, "%q and %q packed to the same key", d, prev)
		seen[k] = d
	}
}

// TestNumericKeysNeverCollideWithTextKeys is the reason for the tag byte: both
// kinds share one term dictionary, so their key spaces must be disjoint.
func TestNumericKeysNeverCollideWithTextKeys(t *testing.T) {
	corpus := []string{
		"level=info ts=2026-08-12T12:39:34.933471853Z caller=foo.go:12 msg=\"hello world\"",
		"tenant=836243 status=500 duration=1.234567ms path=/api/v1/query",
		"traceID=7f03adbc91acfb6810973856c4072fc4 spanID=27085f823edc762c",
		"id=1779629282473030489 count=0 bytes=43380000000000",
	}
	textKeys := map[[6]byte]string{}
	numKeys := map[[6]byte]struct{}{}
	for _, line := range corpus {
		for _, k := range ExtractFeatures(6, line, nil, nil, nil) {
			var short [6]byte
			copy(short[:], k[:6])
			if k[0] == numericTag {
				numKeys[short] = struct{}{}
				continue
			}
			textKeys[short] = string(k[:6])
			// A text gram only ever holds the transformed alphabet, so it can
			// never start with the tag byte.
			require.NotEqual(t, byte(numericTag), k[0])
			for _, b := range k[:6] {
				require.True(t, b == ' ' || b == '.' || (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z'),
					"unexpected byte %#x in text gram %q", b, string(k[:6]))
			}
		}
	}
	require.NotEmpty(t, textKeys)
	require.NotEmpty(t, numKeys)
	for k := range numKeys {
		_, clash := textKeys[k]
		require.False(t, clash, "numeric key collided with a text key")
	}
}

// TestExtractFeatures_BuildQuerySymmetry is the zero-false-negative property:
// for any needle occurring in a line, every gram the query emits must also have
// been emitted for the line.
func TestExtractFeatures_BuildQuerySymmetry(t *testing.T) {
	line := "req id=1779629282473030489 user=abc123456 ts=2026-08-12T12:39:34.933471853Z"
	built := map[[8]byte]struct{}{}
	for _, k := range ExtractFeatures(6, line, nil, nil, nil) {
		built[k] = struct{}{}
	}
	for _, needle := range []string{
		"1779629282473030489", "962928247", "abc123456", "933471853",
		"2026-08-12T12:39:34.933471853Z", "user=abc",
	} {
		for _, k := range ExtractFeatures(6, needle, nil, nil, nil) {
			_, ok := built[k]
			require.True(t, ok, "needle %q emitted a gram the line did not index", needle)
		}
	}
}

func TestExtractFeatures_IncludesLabelValues(t *testing.T) {
	got := ExtractFeatures(6, "", nil, []string{"labelvalue"}, nil)
	require.Contains(t, textGrams(t, got, 6), "LABELV")
}

func TestExtractFeatures_StructuredMetadata(t *testing.T) {
	sm := push.LabelsAdapter{{Name: "trace", Value: "1779629282473030489"}}
	got := ExtractFeatures(6, "", sm, nil, nil)
	require.Len(t, numericKeys(got), 11, "structured metadata goes through the numeric rule too")
}
