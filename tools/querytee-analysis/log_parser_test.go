package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLogfmt(t *testing.T) {
	t.Run("bare values", func(t *testing.T) {
		fields := parseLogfmt(`level=info msg=hello count=42`)
		assert.Equal(t, "info", fields["level"])
		assert.Equal(t, "hello", fields["msg"])
		assert.Equal(t, "42", fields["count"])
	})

	t.Run("quoted values", func(t *testing.T) {
		fields := parseLogfmt(`msg="hello world" query="{app=\"foo\"}"`)
		assert.Equal(t, "hello world", fields["msg"])
		assert.Equal(t, `{app="foo"}`, fields["query"])
	})

	t.Run("mixed bare and quoted", func(t *testing.T) {
		fields := parseLogfmt(`level=info msg="some message" count=5`)
		assert.Equal(t, "info", fields["level"])
		assert.Equal(t, "some message", fields["msg"])
		assert.Equal(t, "5", fields["count"])
	})

	t.Run("empty string", func(t *testing.T) {
		fields := parseLogfmt("")
		assert.Empty(t, fields)
	})

	t.Run("whitespace only", func(t *testing.T) {
		fields := parseLogfmt("   \t  ")
		assert.Empty(t, fields)
	})

	t.Run("no equals sign", func(t *testing.T) {
		fields := parseLogfmt("justtext")
		assert.Empty(t, fields)
	})

	t.Run("empty value", func(t *testing.T) {
		fields := parseLogfmt(`key= next=val`)
		assert.Equal(t, "", fields["key"])
		assert.Equal(t, "val", fields["next"])
	})

	t.Run("tabs as separators", func(t *testing.T) {
		fields := parseLogfmt("a=1\tb=2\tc=3")
		assert.Equal(t, "1", fields["a"])
		assert.Equal(t, "2", fields["b"])
		assert.Equal(t, "3", fields["c"])
	})

	t.Run("quoted value with spaces", func(t *testing.T) {
		fields := parseLogfmt(`query="count_over_time({app=\"foo\"} | logfmt [5m])"`)
		assert.Equal(t, `count_over_time({app="foo"} | logfmt [5m])`, fields["query"])
	})
}

func TestParseSingleLog(t *testing.T) {
	t.Run("valid mismatch line", func(t *testing.T) {
		line := `comparison_status=mismatch correlation_id=abc-123 tenant=my-tenant query="{app=\"foo\"}" query_type=range mismatch_cause=stream_entry_count cell_a_result_uri=gcs://bucket/a.json.gz cell_b_result_uri=gcs://bucket/b.json.gz cell_a_entries_returned=100 cell_b_entries_returned=95 start_time=1700000000000000000 end_time=1700003600000000000 cell_a_used_new_engine=true cell_b_used_new_engine=false`
		entry, err := parseSingleLog(line)
		require.NoError(t, err)

		assert.Equal(t, "abc-123", entry.CorrelationID)
		assert.Equal(t, "my-tenant", entry.Tenant)
		assert.Equal(t, `{app="foo"}`, entry.Query)
		assert.Equal(t, "range", entry.QueryType)
		assert.Equal(t, "stream_entry_count", entry.MismatchCause)
		assert.Equal(t, "gcs://bucket/a.json.gz", entry.CellAResultURI)
		assert.Equal(t, "gcs://bucket/b.json.gz", entry.CellBResultURI)
		assert.Equal(t, int64(100), entry.CellAEntriesReturned)
		assert.Equal(t, int64(95), entry.CellBEntriesReturned)
		assert.True(t, entry.CellAUsedNewEngine)
		assert.False(t, entry.CellBUsedNewEngine)
		assert.False(t, entry.StartTime.IsZero())
		assert.False(t, entry.EndTime.IsZero())
	})

	t.Run("not a mismatch line", func(t *testing.T) {
		line := `comparison_status=match correlation_id=abc`
		_, err := parseSingleLog(line)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a mismatch line")
	})

	t.Run("missing correlation_id", func(t *testing.T) {
		line := `comparison_status=mismatch tenant=t1`
		_, err := parseSingleLog(line)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing correlation_id")
	})

	t.Run("missing optional fields default to zero", func(t *testing.T) {
		line := `comparison_status=mismatch correlation_id=x`
		entry, err := parseSingleLog(line)
		require.NoError(t, err)

		assert.Equal(t, "", entry.Tenant)
		assert.Equal(t, int64(0), entry.CellAEntriesReturned)
		assert.Equal(t, int64(0), entry.CellAExecTimeMs)
		assert.True(t, entry.StartTime.IsZero())
		assert.False(t, entry.CellAUsedNewEngine)
	})
}

func TestParseMismatchLogs(t *testing.T) {
	t.Run("mixed valid and invalid lines", func(t *testing.T) {
		lines := []string{
			`comparison_status=mismatch correlation_id=a`,
			`comparison_status=match correlation_id=b`,
			`comparison_status=mismatch correlation_id=c`,
			`garbage line no equals`,
		}
		entries := parseMismatchLogs(lines)
		assert.Len(t, entries, 2)
		assert.Equal(t, "a", entries[0].CorrelationID)
		assert.Equal(t, "c", entries[1].CorrelationID)
	})

	t.Run("empty input", func(t *testing.T) {
		entries := parseMismatchLogs(nil)
		assert.Empty(t, entries)
	})
}

func TestParseInt64(t *testing.T) {
	assert.Equal(t, int64(0), parseInt64(""))
	assert.Equal(t, int64(42), parseInt64("42"))
	assert.Equal(t, int64(-1), parseInt64("-1"))
	assert.Equal(t, int64(0), parseInt64("not-a-number"))
}

func TestTimeFromUnixNano(t *testing.T) {
	t.Run("valid timestamp", func(t *testing.T) {
		ts := timeFromUnixNano("1700000000000000000")
		assert.False(t, ts.IsZero())
		assert.Equal(t, int64(1700000000), ts.Unix())
	})

	t.Run("zero returns zero time", func(t *testing.T) {
		ts := timeFromUnixNano("0")
		assert.True(t, ts.IsZero())
	})

	t.Run("empty returns zero time", func(t *testing.T) {
		ts := timeFromUnixNano("")
		assert.True(t, ts.IsZero())
	})

	t.Run("result is UTC", func(t *testing.T) {
		ts := timeFromUnixNano("1700000000000000000")
		assert.Equal(t, time.UTC, ts.Location())
	})
}
