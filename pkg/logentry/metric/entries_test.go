package metric

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/promtail/api"

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

var errorHandler = api.EntryHandlerFunc(func(labels model.LabelSet, time time.Time, entry string) error {
	if entry == "error" {
		return errors.New("")
	}
	return nil
})

func Test_LogCount(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	handler := LogCount(reg, errorHandler)

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
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{"foo": "bar"}), time.Now(), "error")
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{}), time.Now(), "error")

		}()
	}
	wg.Wait()

	if err := testutil.GatherAndCompare(reg, strings.NewReader(expectedCount), "log_entries_total"); err != nil {
		t.Fatalf("missmatch metrics: %v", err)
	}

}

const expectedSize = `# HELP log_entries_bytes the total count of bytes
# TYPE log_entries_bytes histogram
log_entries_bytes_bucket{le="16.0"} 10.0
log_entries_bytes_bucket{le="32.0"} 10.0
log_entries_bytes_bucket{le="64.0"} 10.0
log_entries_bytes_bucket{le="128.0"} 10.0
log_entries_bytes_bucket{le="256.0"} 10.0
log_entries_bytes_bucket{le="512.0"} 10.0
log_entries_bytes_bucket{le="1024.0"} 10.0
log_entries_bytes_bucket{le="2048.0"} 10.0
log_entries_bytes_bucket{le="+Inf"} 10.0
log_entries_bytes_sum 35.0
log_entries_bytes_count 10.0
log_entries_bytes_bucket{foo="bar",le="16.0"} 5.0
log_entries_bytes_bucket{foo="bar",le="32.0"} 5.0
log_entries_bytes_bucket{foo="bar",le="64.0"} 5.0
log_entries_bytes_bucket{foo="bar",le="128.0"} 5.0
log_entries_bytes_bucket{foo="bar",le="256.0"} 5.0
log_entries_bytes_bucket{foo="bar",le="512.0"} 5.0
log_entries_bytes_bucket{foo="bar",le="1024.0"} 5.0
log_entries_bytes_bucket{foo="bar",le="2048.0"} 5.0
log_entries_bytes_bucket{foo="bar",le="+Inf"} 5.0
log_entries_bytes_sum{foo="bar"} 15.0
log_entries_bytes_count{foo="bar"} 5.0
log_entries_bytes_bucket{bar="foo",le="16.0"} 5.0
log_entries_bytes_bucket{bar="foo",le="32.0"} 5.0
log_entries_bytes_bucket{bar="foo",le="64.0"} 5.0
log_entries_bytes_bucket{bar="foo",le="128.0"} 5.0
log_entries_bytes_bucket{bar="foo",le="256.0"} 5.0
log_entries_bytes_bucket{bar="foo",le="512.0"} 5.0
log_entries_bytes_bucket{bar="foo",le="1024.0"} 5.0
log_entries_bytes_bucket{bar="foo",le="2048.0"} 5.0
log_entries_bytes_bucket{bar="foo",le="+Inf"} 5.0
log_entries_bytes_sum{bar="foo"} 10.0
log_entries_bytes_count{bar="foo"} 5.0
log_entries_bytes_bucket{bar="foo",foo="bar",le="16.0"} 5.0
log_entries_bytes_bucket{bar="foo",foo="bar",le="32.0"} 5.0
log_entries_bytes_bucket{bar="foo",foo="bar",le="64.0"} 5.0
log_entries_bytes_bucket{bar="foo",foo="bar",le="128.0"} 5.0
log_entries_bytes_bucket{bar="foo",foo="bar",le="256.0"} 5.0
log_entries_bytes_bucket{bar="foo",foo="bar",le="512.0"} 5.0
log_entries_bytes_bucket{bar="foo",foo="bar",le="1024.0"} 5.0
log_entries_bytes_bucket{bar="foo",foo="bar",le="2048.0"} 5.0
log_entries_bytes_bucket{bar="foo",foo="bar",le="+Inf"} 5.0
log_entries_bytes_sum{bar="foo",foo="bar"} 15.0
log_entries_bytes_count{bar="foo",foo="bar"} 5.0
`

func Test_LogSize(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	handler := LogSize(reg, errorHandler)

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
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{}), time.Now(), "error")
			_ = handler.Handle(model.LabelSet(map[model.LabelName]model.LabelValue{"bar": "foo", "foo": "bar"}), time.Now(), "error")

		}()
	}
	wg.Wait()

	if err := testutil.GatherAndCompare(reg, strings.NewReader(expectedSize), "log_entries_bytes"); err != nil {
		t.Fatalf("missmatch metrics: %v", err)
	}

}
