package postings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBloomFilterMayContain_CorruptedBytes is the load-bearing correctness
// pin for the silent-true behaviour preserved from
// metastore.bloomFilterMayContain (index_sections_reader.go:866-868). A
// corrupted bloom MUST return true ("may contain") — returning false would
// cause a false negative (a section silently dropped when it might actually
// contain the value). Document the rationale in the helper's doc comment;
// this test fails immediately if the contract is ever changed.
//
// Internal test (package postings) because bloomFilterMayContain is
// unexported by design — only the helper-facing MatchSections is exported.
func TestBloomFilterMayContain_CorruptedBytes(t *testing.T) {
	require.True(t,
		bloomFilterMayContain([]byte("totally not a valid bloom filter payload"), "anything"),
		"corrupted bloom payload must return true to avoid a false negative "+
			"(per metastore.bloomFilterMayContain line 866-868 +  anti-pattern)",
	)
	// Empty payload also must NOT cause a false negative.
	require.True(t, bloomFilterMayContain(nil, "anything"),
		"nil bloom payload must return true")
	require.True(t, bloomFilterMayContain([]byte{}, "anything"),
		"empty bloom payload must return true")
}

// TestBloomFilterMayContainObserved_CorruptionFlag is the regression
// pin. The observable variant returns a deserializeFailed=true alongside
// "may contain" for any payload that fails to deserialise — caller
// (MatchSections) uses that signal to increment
// xcap.StatPostingsBloomDeserializeFailures so corruption is visible in
// flight rather than silently swallowed.
func TestBloomFilterMayContainObserved_CorruptionFlag(t *testing.T) {
	mayContain, deserializeFailed := bloomFilterMayContainObserved(
		[]byte("totally not a valid bloom filter payload"), "anything")
	require.True(t, mayContain,
		"corrupted bloom must yield 'may contain' (no false negative)")
	require.True(t, deserializeFailed,
		"corruption must be flagged for the observability counter")

	mayContain, deserializeFailed = bloomFilterMayContainObserved(nil, "anything")
	require.True(t, mayContain, "nil bloom payload must yield 'may contain'")
	require.True(t, deserializeFailed,
		"nil bloom payload must be flagged for the observability counter")
}
