package stages

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

// nolint
func mustParseTime(layout, value string) time.Time {
	t, err := time.Parse(layout, value)
	if err != nil {
		panic(err)
	}
	return t
}

func toLabelSet(lbs map[string]string) model.LabelSet {
	res := model.LabelSet{}
	for k, v := range lbs {
		res[model.LabelName(k)] = model.LabelValue(v)
	}
	return res
}

func assertLabels(t *testing.T, expect map[string]string, got model.LabelSet) {
	if len(expect) != len(got) {
		t.Fatalf("labels are not equal in size want: %s got: %s", expect, got)
	}
	for k, v := range expect {
		gotV, ok := got[model.LabelName(k)]
		if !ok {
			t.Fatalf("missing expected label key: %s", k)
		}
		assert.Equal(t, model.LabelValue(v), gotV, "mismatch label value")
	}
}
