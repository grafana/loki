package retention

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/validation"
)

type fakeLimits struct {
	perTenant map[string]time.Duration
	perStream map[string][]validation.StreamRetention
}

func (f fakeLimits) RetentionPeriod(userID string) time.Duration {
	return f.perTenant[userID]
}

func (f fakeLimits) StreamRetention(userID string) []validation.StreamRetention {
	return f.perStream[userID]
}

func Test_expirationChecker_Expired(t *testing.T) {
	e := NewExpirationChecker(&fakeLimits{
		perTenant: map[string]time.Duration{
			"1": time.Hour,
			"2": 24 * time.Hour,
		},
		perStream: map[string][]validation.StreamRetention{
			"1": {
				{Period: model.Duration(2 * time.Hour), Priority: 10, Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				{Period: model.Duration(2 * time.Hour), Priority: 1, Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "foo", "ba.+")}},
			},
			"2": {
				{Period: model.Duration(1 * time.Hour), Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				{Period: model.Duration(2 * time.Hour), Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "foo", "ba.")}},
			},
		},
	})
	tests := []struct {
		name string
		ref  ChunkEntry
		want bool
	}{
		{"expired tenant", newChunkEntry("1", `{foo="buzz"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-2*time.Hour)), true},
		{"just expired tenant", newChunkEntry("1", `{foo="buzz"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-1*time.Hour+(10*time.Microsecond))), false},
		{"not expired tenant", newChunkEntry("1", `{foo="buzz"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-30*time.Minute)), false},
		{"not expired tenant by far", newChunkEntry("2", `{foo="buzz"}`, model.Now().Add(-72*time.Hour), model.Now().Add(-3*time.Hour)), false},
		{"expired stream override", newChunkEntry("2", `{foo="bar"}`, model.Now().Add(-12*time.Hour), model.Now().Add(-10*time.Hour)), true},
		{"non expired stream override", newChunkEntry("1", `{foo="bar"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-90*time.Minute)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, nonDeletedIntervals := e.Expired(tt.ref, model.Now())
			require.Equal(t, tt.want, actual)
			require.Nil(t, nonDeletedIntervals)
		})
	}
}
