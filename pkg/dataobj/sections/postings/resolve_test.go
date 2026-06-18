package postings_test

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

func TestStreamResolver_ZeroMatchers(t *testing.T) {
	r := postings.NewStreamResolver(nil, nil, time.Unix(0, 0), time.Unix(0, 100))
	refs, err := r.Resolve(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, refs)
}

func TestStreamResolver_SingleSectionLabelMatch(t *testing.T) {
	ctx := context.Background()
	secs, closer := buildResolveTestSection(t, []labelPosting{
		{name: "app", value: "nginx", streamID: 1, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
		{name: "app", value: "loki", streamID: 2, obj: "obj-a", section: 0, minTs: 10, maxTs: 20},
	}, nil)
	defer closer()

	m := labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")
	r := postings.NewStreamResolver([]*labels.Matcher{m}, nil, time.Unix(0, 0), time.Unix(0, 1000))
	refs, err := r.Resolve(ctx, secs)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Equal(t, int64(1), refs[0].StreamID)
	require.Equal(t, "obj-a", refs[0].ObjectPath)
	require.Equal(t, int64(0), refs[0].SectionIndex)
}
