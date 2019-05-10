package metric

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
)

const expectedCount = `# HELP log_entries_total the total count of log entries
# TYPE log_entries_total counter
log_entries_total 10.0
log_entries_total{foo="bar"} 5.0
log_entries_total{bar="foo"} 5.0
log_entries_total{bar="foo",foo="bar"} 5.0
`

func Test_LogCount(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	handler := LogCount(reg)

	workerCount := 5
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{}), time.Now(), "")
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{"foo": "bar"}), time.Now(), "")
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{"bar": "foo"}), time.Now(), "")
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{"bar": "foo", "foo": "bar"}), time.Now(), "")
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{}), time.Now(), "")

		}()
	}
	wg.Wait()

	if err := testutil.GatherAndCompare(reg, strings.NewReader(expectedCount), "log_entries_total"); err != nil {
		t.Fatalf("missmatch metrics: %v", err)
	}

}

const expectedSize = `# HELP log_entries_bytes the total count of bytes
# TYPE log_entries_bytes counter
log_entries_bytes 35.0
log_entries_bytes{foo="bar"} 15.0
log_entries_bytes{bar="foo"} 10.0
log_entries_bytes{bar="foo",foo="bar"} 15.0
`

func Test_LogSize(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	handler := LogSize(reg)

	workerCount := 5
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{}), time.Now(), "foo")
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{"foo": "bar"}), time.Now(), "bar")
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{"bar": "foo"}), time.Now(), "fu")
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{"bar": "foo", "foo": "bar"}), time.Now(), "baz")
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{}), time.Now(), "more")

		}()
	}
	wg.Wait()

	if err := testutil.GatherAndCompare(reg, strings.NewReader(expectedSize), "log_entries_bytes"); err != nil {
		t.Fatalf("missmatch metrics: %v", err)
	}

}
