package stages

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
)

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
		if gotV != model.LabelValue(v) {
			t.Fatalf("mismatch label value got: %s/%s want %s/%s", k, gotV, k, model.LabelValue(v))
		}
	}
}
