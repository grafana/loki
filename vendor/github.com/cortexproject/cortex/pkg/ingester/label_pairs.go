package ingester

import (
	"sort"
	"strings"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/util/extract"
)

// A series is uniquely identified by its set of label name/value
// pairs, which may arrive in any order over the wire
type labelPairs []cortexpb.LabelAdapter

func (a labelPairs) String() string {
	var b strings.Builder

	metricName, err := extract.MetricNameFromLabelAdapters(a)
	numLabels := len(a) - 1
	if err != nil {
		numLabels = len(a)
	}
	b.WriteString(metricName)
	b.WriteByte('{')
	count := 0
	for _, pair := range a {
		if pair.Name != model.MetricNameLabel {
			b.WriteString(pair.Name)
			b.WriteString("=\"")
			b.WriteString(pair.Value)
			b.WriteByte('"')
			count++
			if count < numLabels {
				b.WriteByte(',')
			}
		}
	}
	b.WriteByte('}')
	return b.String()
}

// Remove any label where the value is "" - Prometheus 2+ will remove these
// before sending, but other clients such as Prometheus 1.x might send us blanks.
func (a *labelPairs) removeBlanks() {
	for i := 0; i < len(*a); {
		if len((*a)[i].Value) == 0 {
			// Delete by swap with the value at the end of the slice
			(*a)[i] = (*a)[len(*a)-1]
			(*a) = (*a)[:len(*a)-1]
			continue // go round and check the data that is now at position i
		}
		i++
	}
}

func valueForName(s labels.Labels, name string) (string, bool) {
	pos := sort.Search(len(s), func(i int) bool { return s[i].Name >= name })
	if pos == len(s) || s[pos].Name != name {
		return "", false
	}
	return s[pos].Value, true
}

// Check if a and b contain the same name/value pairs
func (a labelPairs) equal(b labels.Labels) bool {
	if len(a) != len(b) {
		return false
	}
	// Check as many as we can where the two sets are in the same order
	i := 0
	for ; i < len(a); i++ {
		if b[i].Name != string(a[i].Name) {
			break
		}
		if b[i].Value != string(a[i].Value) {
			return false
		}
	}
	// Now check remaining values using binary search
	for ; i < len(a); i++ {
		v, found := valueForName(b, a[i].Name)
		if !found || v != a[i].Value {
			return false
		}
	}
	return true
}
