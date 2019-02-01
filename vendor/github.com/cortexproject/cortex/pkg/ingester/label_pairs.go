package ingester

import (
	"bytes"
	"sort"
	"strings"

	"github.com/prometheus/common/model"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util/extract"
)

// A series is uniquely identified by its set of label name/value
// pairs, which may arrive in any order over the wire
type labelPairs []client.LabelPair

// We sort the set for faster lookup, and use a separate type to let
// the compiler check usage.
type sortedLabelPairs []client.LabelPair

func (s sortedLabelPairs) Len() int           { return len(s) }
func (s sortedLabelPairs) Less(i, j int) bool { return bytes.Compare(s[i].Name, s[j].Name) < 0 }
func (s sortedLabelPairs) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

var labelNameBytes = []byte(model.MetricNameLabel)

func (a labelPairs) String() string {
	var b strings.Builder

	metricName, err := extract.MetricNameFromLabelPairs(a)
	numLabels := len(a) - 1
	if err != nil {
		numLabels = len(a)
	}
	b.Write(metricName)
	b.WriteByte('{')
	count := 0
	for _, pair := range a {
		if !pair.Name.Equal(labelNameBytes) {
			b.Write(pair.Name)
			b.WriteString("=\"")
			b.Write(pair.Value)
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

func (s sortedLabelPairs) String() string {
	return labelPairs(s).String()
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

func (a labelPairs) copyValuesAndSort() sortedLabelPairs {
	c := make(sortedLabelPairs, len(a))
	// Since names and values may point into a much larger buffer,
	// make a copy of all the names and values, in one block for efficiency
	totalLength := 0
	for _, pair := range a {
		totalLength += len(pair.Name) + len(pair.Value)
	}
	copyBytes := make([]byte, totalLength)
	pos := 0
	copyByteSlice := func(val []byte) []byte {
		start := pos
		pos += copy(copyBytes[pos:], val)
		return copyBytes[start:pos]
	}
	for i, pair := range a {
		c[i].Name = copyByteSlice(pair.Name)
		c[i].Value = copyByteSlice(pair.Value)
	}
	sort.Sort(c)
	return c
}

func (s sortedLabelPairs) valueForName(name []byte) []byte {
	pos := sort.Search(len(s), func(i int) bool { return bytes.Compare(s[i].Name, name) >= 0 })
	if pos == len(s) || !bytes.Equal(s[pos].Name, name) {
		return nil
	}
	return s[pos].Value
}

// Check if s and b contain the same name/value pairs
func (s sortedLabelPairs) equal(b labelPairs) bool {
	if len(s) != len(b) {
		return false
	}
	for _, pair := range b {
		found := s.valueForName(pair.Name)
		if found == nil || !bytes.Equal(found, pair.Value) {
			return false
		}
	}
	return true
}
