package print

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/fatih/color"

	"github.com/grafana/loki/v3/pkg/logcli/output"
	"github.com/grafana/loki/v3/pkg/logcli/util"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

type QueryResultPrinter struct {
	ShowLabelsKey   []string
	IgnoreLabelsKey []string
	Quiet           bool
	FixedLabelsLen  int
	Forward         bool
}

func NewQueryResultPrinter(showLabelsKey []string, ignoreLabelsKey []string, quiet bool, fixedLabelsLen int, forward bool) *QueryResultPrinter {
	return &QueryResultPrinter{
		ShowLabelsKey:   showLabelsKey,
		IgnoreLabelsKey: ignoreLabelsKey,
		Quiet:           quiet,
		FixedLabelsLen:  fixedLabelsLen,
		Forward:         forward,
	}
}

type streamEntryPair struct {
	entry  loghttp.Entry
	labels loghttp.LabelSet
}

func (r *QueryResultPrinter) PrintResult(value loghttp.ResultValue, out output.LogOutput, lastEntry []*loghttp.Entry) (int, []*loghttp.Entry) {
	length := -1
	var entry []*loghttp.Entry
	switch value.Type() {
	case logqlmodel.ValueTypeStreams:
		length, entry = r.printStream(value.(loghttp.Streams), out, lastEntry)
	case loghttp.ResultTypeScalar:
		printScalar(value.(loghttp.Scalar))
	case loghttp.ResultTypeMatrix:
		printMatrix(value.(loghttp.Matrix))
	case loghttp.ResultTypeVector:
		printVector(value.(loghttp.Vector))
	default:
		log.Fatalf("Unable to print unsupported type: %v", value.Type())
	}
	return length, entry
}

func (r *QueryResultPrinter) printStream(streams loghttp.Streams, out output.LogOutput, lastEntry []*loghttp.Entry) (int, []*loghttp.Entry) {
	common := commonLabels(streams)

	// Remove the labels we want to show from common
	if len(r.ShowLabelsKey) > 0 {
		common = matchLabels(false, common, r.ShowLabelsKey)
	}

	if len(common) > 0 && !r.Quiet {
		log.Println("Common labels:", color.RedString(common.String()))
	}

	if len(r.IgnoreLabelsKey) > 0 && !r.Quiet {
		log.Println("Ignoring labels key:", color.RedString(strings.Join(r.IgnoreLabelsKey, ",")))
	}

	if len(r.ShowLabelsKey) > 0 && !r.Quiet {
		log.Println("Print only labels key:", color.RedString(strings.Join(r.ShowLabelsKey, ",")))
	}

	// Remove ignored and common labels from the cached labels and
	// calculate the max labels length
	maxLabelsLen := r.FixedLabelsLen
	for i, s := range streams {
		// Remove common labels
		ls := subtract(s.Labels, common)

		if len(r.ShowLabelsKey) > 0 {
			ls = matchLabels(true, ls, r.ShowLabelsKey)
		}

		// Remove ignored labels
		if len(r.IgnoreLabelsKey) > 0 {
			ls = matchLabels(false, ls, r.IgnoreLabelsKey)
		}

		// Overwrite existing Labels
		streams[i].Labels = ls

		// Update max labels length
		length := len(ls.String())
		if maxLabelsLen < length {
			maxLabelsLen = length
		}
	}

	// sort and display entries
	allEntries := make([]streamEntryPair, 0)

	for _, s := range streams {
		for _, e := range s.Entries {
			allEntries = append(allEntries, streamEntryPair{
				entry:  e,
				labels: s.Labels,
			})
		}
	}

	if len(allEntries) == 0 {
		return 0, nil
	}

	if r.Forward {
		sort.Slice(allEntries, func(i, j int) bool { return allEntries[i].entry.Timestamp.Before(allEntries[j].entry.Timestamp) })
	} else {
		sort.Slice(allEntries, func(i, j int) bool { return allEntries[i].entry.Timestamp.After(allEntries[j].entry.Timestamp) })
	}

	printed := 0
	for _, e := range allEntries {
		// Skip the last entry if it overlaps, this happens because batching includes the last entry from the last batch
		if len(lastEntry) > 0 && e.entry.Timestamp == lastEntry[0].Timestamp {
			skip := false
			// Because many logs can share a timestamp in the unlucky event a batch ends with a timestamp
			// shared by multiple entries we have to check all that were stored to see if we've already
			// printed them.
			for _, le := range lastEntry {
				if e.entry.Line == le.Line {
					skip = true
				}
			}
			if skip {
				continue
			}
		}
		out.FormatAndPrintln(e.entry.Timestamp, e.labels, maxLabelsLen, e.entry.Line)
		printed++
	}

	// Loki allows multiple entries at the same timestamp, this is a bit of a mess if a batch ends
	// with an entry that shared multiple timestamps, so we need to keep a list of all these entries
	// because the next query is going to contain them too and we want to not duplicate anything already
	// printed.
	lel := []*loghttp.Entry{}
	// Start with the timestamp of the last entry
	le := allEntries[len(allEntries)-1].entry
	for i, e := range allEntries {
		// Save any entry which has this timestamp (most of the time this will only be the single last entry)
		if e.entry.Timestamp.Equal(le.Timestamp) {
			lel = append(lel, &allEntries[i].entry)
		}
	}

	return printed, lel
}

func printMatrix(matrix loghttp.Matrix) {
	// yes we are effectively unmarshalling and then immediately marshalling this object back to json.  we are doing this b/c
	// it gives us more flexibility with regard to output types in the future.  initially we are supporting just formatted json but eventually
	// we might add output options such as render to an image file on disk
	bytes, err := json.MarshalIndent(matrix, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling matrix: %v", err)
	}

	fmt.Print(string(bytes))
}

func printVector(vector loghttp.Vector) {
	bytes, err := json.MarshalIndent(vector, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling vector: %v", err)
	}

	fmt.Print(string(bytes))
}

func printScalar(scalar loghttp.Scalar) {
	bytes, err := json.MarshalIndent(scalar, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling scalar: %v", err)
	}

	fmt.Print(string(bytes))
}

type kvLogger struct {
	*tabwriter.Writer
}

func (k kvLogger) Log(keyvals ...interface{}) error {
	for i := 0; i < len(keyvals); i += 2 {
		fmt.Fprintln(k.Writer, color.BlueString("%s", keyvals[i]), "\t", fmt.Sprintf("%v", keyvals[i+1]))
	}
	k.Flush()
	return nil
}

func (r *QueryResultPrinter) PrintStats(stats stats.Result) {
	writer := tabwriter.NewWriter(os.Stderr, 0, 8, 0, '\t', 0)
	stats.Log(kvLogger{Writer: writer})
}

func matchLabels(on bool, l loghttp.LabelSet, names []string) loghttp.LabelSet {
	return util.MatchLabels(on, l, names)
}

// return commonLabels labels between given labels set
func commonLabels(streams loghttp.Streams) loghttp.LabelSet {
	if len(streams) == 0 {
		return nil
	}

	result := streams[0].Labels
	for i := 1; i < len(streams); i++ {
		result = intersect(result, streams[i].Labels)
	}
	return result
}

// intersect two labels set
func intersect(a, b loghttp.LabelSet) loghttp.LabelSet {
	set := loghttp.LabelSet{}

	for ka, va := range a {
		if vb, ok := b[ka]; ok {
			if vb == va {
				set[ka] = va
			}
		}
	}
	return set
}

// subtract labels set b from labels set a
func subtract(a, b loghttp.LabelSet) loghttp.LabelSet {
	set := loghttp.LabelSet{}

	for ka, va := range a {
		if vb, ok := b[ka]; ok {
			if vb == va {
				continue
			}
		}
		set[ka] = va
	}
	return set
}
