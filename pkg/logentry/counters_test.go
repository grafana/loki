package logentry

import (
	"strings"
	"sync"
	"testing"
	"time"

	testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
)

var expected = `# HELP log_entries_total the total count of log entries
# TYPE log_entries_total counter
log_entries_total 10.0
log_entries_total{foo="bar"} 5.0
log_entries_total{bar="foo"} 5.0
log_entries_total{bar="foo",foo="bar"} 5.0
`

func Test_newCounters(t *testing.T) {
	t.Parallel()

	handler := newCounters()

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

	if err := testutil.GatherAndCompare(handler, strings.NewReader(expected), "log_entries_total"); err != nil {
		t.Fatalf("missmatch metrics: %v", err)
	}

}
