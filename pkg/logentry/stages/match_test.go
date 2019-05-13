package stages

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
)

type hasRunStage bool

func (h *hasRunStage) Process(labels model.LabelSet, t *time.Time, entry *string) {
	*h = true
}

func Test_withMatcher(t *testing.T) {
	t.Parallel()
	tests := []struct {
		matcher string
		labels  map[string]string

		shouldRun bool
		wantErr   bool
	}{
		{"{foo=\"bar\"} |= \"foo\"", map[string]string{"foo": "bar"}, false, true},
		{"{foo=\"bar\"} |~ \"foo\"", map[string]string{"foo": "bar"}, false, true},
		{"foo", map[string]string{"foo": "bar"}, false, true},
		{"{}", map[string]string{"foo": "bar"}, false, true},
		{"{", map[string]string{"foo": "bar"}, false, true},
		{"", map[string]string{"foo": "bar"}, true, false},
		{"{foo=\"bar\"}", map[string]string{"foo": "bar"}, true, false},
		{"{foo=\"\"}", map[string]string{"foo": "bar"}, false, false},
		{"{foo=\"\"}", map[string]string{}, true, false},
		{"{foo!=\"bar\"}", map[string]string{"foo": "bar"}, false, false},
		{"{foo=\"bar\",bar!=\"test\"}", map[string]string{"foo": "bar"}, true, false},
		{"{foo=\"bar\",bar!=\"test\"}", map[string]string{"foo": "bar", "bar": "test"}, false, false},
		{"{foo=\"bar\",bar=~\"te.*\"}", map[string]string{"foo": "bar", "bar": "test"}, true, false},
		{"{foo=\"bar\",bar!~\"te.*\"}", map[string]string{"foo": "bar", "bar": "test"}, false, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.matcher, tt.labels), func(t *testing.T) {
			hasRun := hasRunStage(false)
			s, err := withMatcher(&hasRun, tt.matcher)
			if (err != nil) != tt.wantErr {
				t.Errorf("withMatcher() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if s != nil {
				ts, entry := time.Now(), ""
				s.Process(toLabelSet(tt.labels), &ts, &entry)

				if bool(hasRun) != tt.shouldRun {
					t.Error("stage ran but should have not")
				}
			}
		})
	}
}
