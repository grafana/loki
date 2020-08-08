package query

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/fatih/color"
	json "github.com/json-iterator/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/cfg"
	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/marshal"
	"github.com/grafana/loki/pkg/logql/stats"
	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/util/validation"
)

type streamEntryPair struct {
	entry  loghttp.Entry
	labels loghttp.LabelSet
}

// Query contains all necessary fields to execute instant and range queries and print the results.
type Query struct {
	QueryString     string
	Start           time.Time
	End             time.Time
	Limit           int
	BatchSize       int
	Forward         bool
	Step            time.Duration
	Interval        time.Duration
	Quiet           bool
	NoLabels        bool
	IgnoreLabelsKey []string
	ShowLabelsKey   []string
	FixedLabelsLen  int
	ColoredOutput   bool
	LocalConfig     string
}

// DoQuery executes the query and prints out the results
func (q *Query) DoQuery(c *client.Client, out output.LogOutput, statistics bool) {

	if q.LocalConfig != "" {
		if err := q.DoLocalQuery(out, statistics, c.OrgID); err != nil {
			log.Fatalf("Query failed: %+v", err)
		}
		return
	}

	d := q.resultsDirection()

	var resp *loghttp.QueryResponse
	var err error

	if q.isInstant() {
		resp, err = c.Query(q.QueryString, q.Limit, q.Start, d, q.Quiet)
		if err != nil {
			log.Fatalf("Query failed: %+v", err)
		}
		if statistics {
			q.printStats(resp.Data.Statistics)
		}
		_, _ = q.printResult(resp.Data.Result, out, nil)
	} else {
		if q.Limit < q.BatchSize {
			q.BatchSize = q.Limit
		}
		resultLength := q.BatchSize
		total := 0
		start := q.Start
		var lastEntry *loghttp.Entry
		// Make the assumption if the result size == batch size there will be more rows to query
		for resultLength == q.BatchSize && total < q.Limit {
			resp, err = c.QueryRange(q.QueryString, q.BatchSize, start, q.End, d, q.Step, q.Interval, q.Quiet)
			if err != nil {
				log.Fatalf("Query failed: %+v", err)
			}

			if statistics {
				q.printStats(resp.Data.Statistics)
			}

			resultLength, lastEntry = q.printResult(resp.Data.Result, out, lastEntry)
			// Was not a log stream query, there is no batching.
			if resultLength == -1 {
				break
			}
			total += resultLength
		}
	}

}

func (q *Query) printResult(value loghttp.ResultValue, out output.LogOutput, lastEntry *loghttp.Entry) (int, *loghttp.Entry) {
	length := -1
	var entry loghttp.Entry
	switch value.Type() {
	case logql.ValueTypeStreams:
		length, entry = q.printStream(value.(loghttp.Streams), out, lastEntry)
	case parser.ValueTypeScalar:
		q.printScalar(value.(loghttp.Scalar))
	case parser.ValueTypeMatrix:
		q.printMatrix(value.(loghttp.Matrix))
	case parser.ValueTypeVector:
		q.printVector(value.(loghttp.Vector))
	default:
		log.Fatalf("Unable to print unsupported type: %v", value.Type())
	}
	return length, &entry
}

// DoLocalQuery executes the query against the local store using a Loki configuration file.
func (q *Query) DoLocalQuery(out output.LogOutput, statistics bool, orgID string) error {

	var conf loki.Config
	conf.RegisterFlags(flag.CommandLine)
	if q.LocalConfig == "" {
		return errors.New("no supplied config file")
	}
	if err := cfg.YAML(q.LocalConfig)(&conf); err != nil {
		return err
	}

	if err := conf.Validate(util.Logger); err != nil {
		return err
	}

	limits, err := validation.NewOverrides(conf.LimitsConfig, nil)
	if err != nil {
		return err
	}

	querier, err := storage.NewStore(conf.StorageConfig, conf.ChunkStoreConfig, conf.SchemaConfig, limits, prometheus.DefaultRegisterer)
	if err != nil {
		return err
	}

	eng := logql.NewEngine(conf.Querier.Engine, querier)
	var query logql.Query

	if q.isInstant() {
		query = eng.Query(logql.NewLiteralParams(
			q.QueryString,
			q.Start,
			q.Start,
			0,
			0,
			q.resultsDirection(),
			uint32(q.Limit),
			nil,
		))
	} else {
		query = eng.Query(logql.NewLiteralParams(
			q.QueryString,
			q.Start,
			q.End,
			q.Step,
			q.Interval,
			q.resultsDirection(),
			uint32(q.Limit),
			nil,
		))
	}

	// execute the query
	ctx := user.InjectOrgID(context.Background(), orgID)
	result, err := query.Exec(ctx)
	if err != nil {
		return err
	}

	if statistics {
		q.printStats(result.Statistics)
	}

	value, err := marshal.NewResultValue(result.Data)
	if err != nil {
		return err
	}

	q.printResult(value, out, nil)
	return nil
}

// SetInstant makes the Query an instant type
func (q *Query) SetInstant(time time.Time) {
	q.Start = time
	q.End = time
}

func (q *Query) isInstant() bool {
	return q.Start == q.End
}

func (q *Query) printStream(streams loghttp.Streams, out output.LogOutput, lastEntry *loghttp.Entry) (int, loghttp.Entry) {
	common := commonLabels(streams)

	// Remove the labels we want to show from common
	if len(q.ShowLabelsKey) > 0 {
		common = matchLabels(false, common, q.ShowLabelsKey)
	}

	if len(common) > 0 && !q.Quiet {
		log.Println("Common labels:", color.RedString(common.String()))
	}

	if len(q.IgnoreLabelsKey) > 0 && !q.Quiet {
		log.Println("Ignoring labels key:", color.RedString(strings.Join(q.IgnoreLabelsKey, ",")))
	}

	// Remove ignored and common labels from the cached labels and
	// calculate the max labels length
	maxLabelsLen := q.FixedLabelsLen
	for i, s := range streams {
		// Remove common labels
		ls := subtract(s.Labels, common)

		// Remove ignored labels
		if len(q.IgnoreLabelsKey) > 0 {
			ls = matchLabels(false, ls, q.IgnoreLabelsKey)
		}

		// Overwrite existing Labels
		streams[i].Labels = ls

		// Update max labels length
		len := len(ls.String())
		if maxLabelsLen < len {
			maxLabelsLen = len
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

	if q.Forward {
		sort.Slice(allEntries, func(i, j int) bool { return allEntries[i].entry.Timestamp.Before(allEntries[j].entry.Timestamp) })
	} else {
		sort.Slice(allEntries, func(i, j int) bool { return allEntries[i].entry.Timestamp.After(allEntries[j].entry.Timestamp) })
	}

	for _, e := range allEntries {
		// Skip the last entry if it overlaps, this happens because batching includes the last entry from the last batch
		if lastEntry != nil && e.entry.Timestamp == lastEntry.Timestamp && e.entry.Line == lastEntry.Line {
			continue
		}
		fmt.Println(out.Format(e.entry.Timestamp, e.labels, maxLabelsLen, e.entry.Line))
	}

	return len(allEntries), allEntries[len(allEntries)-1].entry
}

func (q *Query) printMatrix(matrix loghttp.Matrix) {
	// yes we are effectively unmarshalling and then immediately marshalling this object back to json.  we are doing this b/c
	// it gives us more flexibility with regard to output types in the future.  initially we are supporting just formatted json but eventually
	// we might add output options such as render to an image file on disk
	bytes, err := json.MarshalIndent(matrix, "", "  ")

	if err != nil {
		log.Fatalf("Error marshalling matrix: %v", err)
	}

	fmt.Print(string(bytes))
}

func (q *Query) printVector(vector loghttp.Vector) {
	bytes, err := json.MarshalIndent(vector, "", "  ")

	if err != nil {
		log.Fatalf("Error marshalling vector: %v", err)
	}

	fmt.Print(string(bytes))
}

func (q *Query) printScalar(scalar loghttp.Scalar) {
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

func (q *Query) printStats(stats stats.Result) {
	writer := tabwriter.NewWriter(os.Stderr, 0, 8, 0, '\t', 0)
	stats.Log(kvLogger{Writer: writer})
}

func (q *Query) resultsDirection() logproto.Direction {
	if q.Forward {
		return logproto.FORWARD
	}
	return logproto.BACKWARD
}
