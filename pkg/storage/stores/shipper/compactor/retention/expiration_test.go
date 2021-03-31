package retention

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func Test_expirationChecker_Expired(t *testing.T) {
	e := NewExpirationChecker(map[string]StreamRule{
		string(labelsSeriesID(labels.Labels{labels.Label{Name: "foo", Value: "bar"}})): {
			UserID:   "1",
			Duration: 2 * time.Hour,
		},
		string(labelsSeriesID(labels.Labels{labels.Label{Name: "foo", Value: "bar"}})): {
			UserID:   "2",
			Duration: 1 * time.Hour,
		},
	}, fakeRule{
		tenants: map[string]time.Duration{
			"1": time.Hour,
			"2": 24 * time.Hour,
		},
	})
	tests := []struct {
		name string
		ref  ChunkRef
		want bool
	}{
		{"expired tenant", newChunkRef("1", `{foo="buzz"}`, model.Now().Add(2*time.Hour), model.Now().Add(3*time.Hour)), true},
		{"just expired tenant", newChunkRef("1", `{foo="buzz"}`, model.Now().Add(1*time.Hour), model.Now().Add(3*time.Hour)), false},
		{"not expired tenant", newChunkRef("1", `{foo="buzz"}`, model.Now().Add(30*time.Minute), model.Now().Add(3*time.Hour)), false},
		{"not expired tenant by far", newChunkRef("2", `{foo="buzz"}`, model.Now().Add(30*time.Minute), model.Now().Add(3*time.Hour)), false},
		{"expired stream override", newChunkRef("2", `{foo="bar"}`, model.Now().Add(3*time.Hour), model.Now().Add(4*time.Hour)), true},
		{"non expired stream override", newChunkRef("1", `{foo="bar"}`, model.Now().Add(1*time.Hour), model.Now().Add(4*time.Hour)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, e.Expired(&tt.ref))
		})
	}
}
