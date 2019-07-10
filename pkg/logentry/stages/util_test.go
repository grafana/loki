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

// Verify the formatting of float conversion to make sure there are not any trailing zeros,
// and also make sure unix timestamps are converted properly
func TestGetString(t *testing.T) {
	var f64, f64_1 float64
	var f32 float32
	f64 = 1
	f64_1 = 1562723913000
	f32 = 2.02
	s64, err := getString(f64)
	if err != nil {
		t.Errorf("Failed to get string from float... this shouldn't have happened: %v", err)
		return
	}
	s64_1, err := getString(f64_1)
	if err != nil {
		t.Errorf("Failed to get string from float... this shouldn't have happened: %v", err)
		return
	}
	s32, err := getString(f32)
	if err != nil {
		t.Errorf("Failed to get string from float... this shouldn't have happened: %v", err)
		return
	}
	assert.Equal(t, "1", s64)
	assert.Equal(t, "2.02", s32)
	assert.Equal(t, "1562723913000", s64_1)
}
