package logs

import (
	"bytes"
	"context"
	"slices"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/stretchr/testify/require"
)

func TestRowReader_NoPredicates(t *testing.T) {
	logsSection := buildSection(t)

	readBuf := make([]Record, 3)
	rowReader := NewRowReader(logsSection)
	n, err := rowReader.Read(context.Background(), readBuf)
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestRowReader_StreamIDPredicate(t *testing.T) {
	logsSection := buildSection(t)

	readBuf := make([]Record, 3)
	rowReader := NewRowReader(logsSection)

	err := rowReader.MatchStreams(slices.Values([]int64{1}))
	require.NoError(t, err)
	n, err := rowReader.Read(context.Background(), readBuf)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func buildSection(t *testing.T) *Section {
	logsBuilder := NewBuilder(nil, BuilderOptions{
		StripeMergeLimit: 2,
	})
	logsBuilder.Append(Record{
		StreamID:  1,
		Timestamp: time.Now(),
		Line:      []byte("test"),
	})
	logsBuilder.Append(Record{
		StreamID:  2,
		Timestamp: time.Now(),
		Line:      []byte("test2"),
	})

	out := bytes.NewBuffer(nil)
	b := dataobj.NewBuilder()
	err := b.Append(logsBuilder)
	require.NoError(t, err)
	_, err = b.Flush(out)
	require.NoError(t, err)

	obj, err := dataobj.FromReaderAt(bytes.NewReader(out.Bytes()), int64(out.Len()))
	require.NoError(t, err)

	var logsSection *Section
	for _, section := range obj.Sections() {
		logsSection, err = Open(context.Background(), section)
		require.NoError(t, err)
	}
	return logsSection
}
