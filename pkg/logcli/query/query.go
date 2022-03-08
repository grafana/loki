package query

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/prometheus/common/model"
	"log"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
	json "github.com/json-iterator/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/storage"
	chunk "github.com/grafana/loki/pkg/storage/chunk/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	"github.com/grafana/loki/pkg/util/cfg"
	utillog "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/marshal"
	"github.com/grafana/loki/pkg/validation"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
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

	Pretty bool
	Live   bool
	UiCtrl UiController
}

// DoQuery executes the query and prints out the results
func (q *Query) DoQuery(c client.Client, out output.LogOutput, statistics bool) {
	if q.LocalConfig != "" {
		if err := q.DoLocalQuery(out, statistics, c.GetOrgID()); err != nil {
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
		resultLength := 0
		total := 0
		start := q.Start
		end := q.End
		var lastEntry []*loghttp.Entry
		for total < q.Limit {
			bs := q.BatchSize
			// We want to truncate the batch size if the remaining number
			// of items needed to reach the limit is less than the batch size
			if q.Limit-total < q.BatchSize {
				// Truncated batchsize is q.Limit - total, however we add to this
				// the length of the overlap from the last query to make sure we get the
				// correct amount of new logs knowing there will be some overlapping logs returned.
				bs = q.Limit - total + len(lastEntry)
			}
			resp, err = c.QueryRange(q.QueryString, bs, start, end, d, q.Step, q.Interval, q.Quiet)
			if err != nil {
				log.Fatalf("Query failed: %+v", err)
			}

			if statistics {
				q.printStats(resp.Data.Statistics)
			}

			resultLength, lastEntry = q.printResult(resp.Data.Result, out, lastEntry)

			// Was not a log stream query, or no results, no more batching
			if resultLength <= 0 {
				break
			}
			// Also no result, wouldn't expect to hit this.
			if len(lastEntry) == 0 {
				break
			}
			// Can only happen if all the results return in one request
			if resultLength == q.Limit {
				break
			}
			if len(lastEntry) >= q.BatchSize {
				log.Fatalf("Invalid batch size %v, the next query will have %v overlapping entries "+
					"(there will always be 1 overlapping entry but Loki allows multiple entries to have "+
					"the same timestamp, so when a batch ends in this scenario the next query will include "+
					"all the overlapping entries again).  Please increase your batch size to at least %v to account "+
					"for overlapping entryes\n", q.BatchSize, len(lastEntry), len(lastEntry)+1)
			}

			// Batching works by taking the timestamp of the last query and using it in the next query,
			// because Loki supports multiple entries with the same timestamp it's possible for a batch to have
			// fallen in the middle of a list of entries for the same time, so to make sure we get all entries
			// we start the query on the same time as the last entry from the last batch, and then we keep this last
			// entry and remove the duplicate when printing the results.
			// Because of this duplicate entry, we have to subtract it here from the total for each batch
			// to get the desired limit.
			total += resultLength
			// Based on the query direction we either set the start or end for the next query.
			// If there are multiple entries in `lastEntry` they have to have the same timestamp so we can pick just the first
			if q.Forward {
				start = lastEntry[0].Timestamp
			} else {
				// The end timestamp is exclusive on a backward query, so to make sure we get back an overlapping result
				// fudge the timestamp forward in time to make sure to get the last entry from this batch in the next query
				end = lastEntry[0].Timestamp.Add(1 * time.Nanosecond)
			}

		}
	}
}

func (q *Query) printResult(value loghttp.ResultValue, out output.LogOutput, lastEntry []*loghttp.Entry) (int, []*loghttp.Entry) {
	length := -1
	var entry []*loghttp.Entry
	switch value.Type() {
	case logqlmodel.ValueTypeStreams:
		length, entry = q.printStream(value.(loghttp.Streams), out, lastEntry)
	case loghttp.ResultTypeScalar:
		q.printScalar(value.(loghttp.Scalar))
	case loghttp.ResultTypeMatrix:
		q.printMatrix(value.(loghttp.Matrix))
	case loghttp.ResultTypeVector:
		q.printVector(value.(loghttp.Vector))
	default:
		log.Fatalf("Unable to print unsupported type: %v", value.Type())
	}
	return length, entry
}

// DoLocalQuery executes the query against the local store using a Loki configuration file.
func (q *Query) DoLocalQuery(out output.LogOutput, statistics bool, orgID string) error {
	var conf loki.Config
	conf.RegisterFlags(flag.CommandLine)
	if q.LocalConfig == "" {
		return errors.New("no supplied config file")
	}
	if err := cfg.YAML(q.LocalConfig, false)(&conf); err != nil {
		return err
	}

	if err := conf.Validate(); err != nil {
		return err
	}

	limits, err := validation.NewOverrides(conf.LimitsConfig, nil)
	if err != nil {
		return err
	}
	cm := chunk.NewClientMetrics()
	storage.RegisterCustomIndexClients(&conf.StorageConfig, cm, prometheus.DefaultRegisterer)
	conf.StorageConfig.BoltDBShipperConfig.Mode = shipper.ModeReadOnly
	chunkStore, err := chunk.NewStore(conf.StorageConfig.Config, conf.ChunkStoreConfig.StoreConfig, conf.SchemaConfig.SchemaConfig, limits, cm, prometheus.DefaultRegisterer, nil, utillog.Logger)
	if err != nil {
		return err
	}

	querier, err := storage.NewStore(conf.StorageConfig, conf.SchemaConfig, chunkStore, prometheus.DefaultRegisterer)
	if err != nil {
		return err
	}

	eng := logql.NewEngine(conf.Querier.Engine, querier, limits, utillog.Logger)
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
	return q.Start == q.End && q.Step == 0
}

func (q *Query) printStream(streams loghttp.Streams, out output.LogOutput, lastEntry []*loghttp.Entry) (int, []*loghttp.Entry) {
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

	if len(q.ShowLabelsKey) > 0 && !q.Quiet {
		log.Println("Print only labels key:", color.RedString(strings.Join(q.ShowLabelsKey, ",")))
	}

	// Remove ignored and common labels from the cached labels and
	// calculate the max labels length
	maxLabelsLen := q.FixedLabelsLen
	for i, s := range streams {
		// Remove common labels
		ls := subtract(s.Labels, common)

		if len(q.ShowLabelsKey) > 0 {
			ls = matchLabels(true, ls, q.ShowLabelsKey)
		}

		// Remove ignored labels
		if len(q.IgnoreLabelsKey) > 0 {
			ls = matchLabels(false, ls, q.IgnoreLabelsKey)
		}

		// Overwrite existing Labels
		streams[i].Labels = ls

		// Update max labels length
		l := len(ls.String())
		if maxLabelsLen < l {
			maxLabelsLen = l
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

	if q.Forward {
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

func (q *Query) printMatrix(matrix loghttp.Matrix) {
	if q.Pretty {
		q.UiCtrl.UpdateGraph(matrix)
		return
	}

	// yes we are effectively unmarshalling and then immediately marshalling this object back to json.  we are doing this b/c
	// it gives us more flexibility with regard to output types in the future.  initially we are supporting just formatted json but eventually
	// we might add output options such as render to an image file on disk
	bytes, err := json.MarshalIndent(matrix, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling matrix: %v", err)
	}

	fmt.Print(string(bytes))
}

type UiController struct {
	grid        *ui.Grid
	graphPanel  *widgets.Plot
	legendPanel *widgets.List

	showStats  bool
	statsPanel *widgets.Paragraph

	selectedLabel string

	currentMatrix loghttp.Matrix
}

type UiPanelMeta struct {
	widget         ui.Drawable
	uiEventHandler func(ui.Event) bool
}

func NewUiController(showStats bool) UiController {
	graphPanel := widgets.NewPlot()
	graphPanel.Title = "Graph"
	graphPanel.AxesColor = ui.ColorWhite

	legendPanel := widgets.NewList()
	legendPanel.Title = "Legend"
	legendPanel.WrapText = true
	legendPanel.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen)

	statsPanel := widgets.NewParagraph()
	statsPanel.Title = "Statistics"

	grid := ui.NewGrid()
	grid.SetRect(0, 0, 0, 0) // Will be updated on Init()

	if showStats {
		grid.Set(
			ui.NewRow(2.0/3, ui.NewCol(3.0/4, graphPanel), ui.NewCol(1.0/4, statsPanel)),
			ui.NewRow(1.0/3, ui.NewCol(1.0, legendPanel)),
		)
	} else {
		grid.Set(
			ui.NewRow(2.0/3, ui.NewCol(1.0, graphPanel)),
			ui.NewRow(1.0/3, ui.NewCol(1.0, legendPanel)),
		)
	}

	uiController := UiController{
		grid:        grid,
		graphPanel:  graphPanel,
		legendPanel: legendPanel,
		statsPanel:  statsPanel,
		showStats:   showStats,
	}

	return uiController
}

func (u *UiController) Init() error {
	err := ui.Init()
	if err != nil {
		return err
	}

	width, height := ui.TerminalDimensions()
	u.grid.SetRect(0, 0, width, height)

	return nil
}

func (u *UiController) Close() {
	ui.Close()
}

func (u *UiController) Render() {
	ui.Clear()
	ui.Render(u.grid)
}

func (u *UiController) UpdateGraph(matrix loghttp.Matrix) {
	data := make([][]float64, 0)
	colors := make([]ui.Color, 0)
	labels := make([]string, 0)

	selectedIndex := -1

	for i, stream := range matrix {
		fv := make([]float64, 0)
		for _, v := range stream.Values {
			fv = append(fv, float64(v.Value))
		}

		rowLabel := stream.Metric.String()

		data = append(data, fv)
		colors = append(colors, u.GetColorForLabels(stream.Metric))
		labels = append(labels, rowLabel)

		if u.selectedLabel != "" && strings.Contains(u.selectedLabel, rowLabel) {
			selectedIndex = i
		}
	}

	filteredColors := make([]ui.Color, 0)
	for i, c := range colors {
		if selectedIndex == -1 || selectedIndex == i {
			filteredColors = append(filteredColors, c)
		} else {
			filteredColors = append(filteredColors, ui.ColorBlack)
		}
	}

	u.graphPanel.Data = data
	u.graphPanel.LineColors = filteredColors

	u.legendPanel.Rows = u.GetLegend(labels, colors)
}

type uiStatsLogger struct {
	*tabwriter.Writer
}

func (k uiStatsLogger) Log(keyvals ...interface{}) error {
	for i := 0; i < len(keyvals); i += 2 {
		fmt.Fprintln(k.Writer, fmt.Sprintf("[%s](fg:bold)", keyvals[i]), "\t", fmt.Sprintf("%v", keyvals[i+1]))
	}
	k.Flush()
	return nil
}

func (u *UiController) UpdateStats(result stats.Result) {
	logLine := new(strings.Builder)

	writer := tabwriter.NewWriter(logLine, 0, 8, 0, '\t', 0)
	result.Log(uiStatsLogger{Writer: writer})

	u.statsPanel.Text = logLine.String()
}

func (u *UiController) GetColorForLabels(labels model.Metric) ui.Color {
	// There are 8 colors, but the last one is white which is used for borders,
	// so we use the first 7 colors. We also skip the first color, which is black.
	return ui.Color(labels.FastFingerprint()%7) + 1
}

func (u *UiController) GetLegend(labels []string, colors []ui.Color) []string {
	reverseStyleParserColorMap := make(map[ui.Color]string, len(ui.StyleParserColorMap))
	for k, v := range ui.StyleParserColorMap {
		reverseStyleParserColorMap[v] = k
	}

	var legend []string
	for i, l := range labels {
		labelColor := colors[i]
		if u.selectedLabel != "" && strings.Contains(u.selectedLabel, l) {
			labelColor = ui.ColorWhite
		}

		legend = append(legend, fmt.Sprintf("[%s](fg:%s)\n", l, reverseStyleParserColorMap[labelColor]))
	}

	return legend
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
	useColors bool
}

func (k kvLogger) Log(keyvals ...interface{}) error {
	for i := 0; i < len(keyvals); i += 2 {
		var key string
		if k.useColors {
			key = color.BlueString("%s", keyvals[i])
		} else {
			key = fmt.Sprintf("%s", keyvals[i])
		}
		value := fmt.Sprintf("%v", keyvals[i+1])

		fmt.Fprintln(k.Writer, key, "\t", value)
	}
	k.Flush()
	return nil
}

func (q *Query) printStats(stats stats.Result) {
	if q.Pretty {
		q.UiCtrl.UpdateStats(stats)
	} else {
		writer := tabwriter.NewWriter(os.Stderr, 0, 8, 0, '\t', 0)
		stats.Log(kvLogger{Writer: writer, useColors: true})
	}
}

func (q *Query) resultsDirection() logproto.Direction {
	if q.Forward {
		return logproto.FORWARD
	}
	return logproto.BACKWARD
}

func (u *UiController) HandleUiEvent(e ui.Event) {
	needsUpdate := true

	switch {
	case e.Type == ui.ResizeEvent:
		u.fitPanelsToTerminal(e.Payload.(ui.Resize))
	case e.ID == "j" || e.ID == "<Down>":
		u.legendPanel.ScrollDown()
	case e.ID == "k" || e.ID == "<Up>":
		u.legendPanel.ScrollUp()
	case e.ID == "<Enter>":
		if u.selectedLabel == "" {
			u.selectedLabel = u.legendPanel.Rows[u.legendPanel.SelectedRow]
		} else {
			u.selectedLabel = ""
		}

		// The legend gets refreshed along with the graph
		u.UpdateGraph(u.currentMatrix)
	default:
		needsUpdate = false
	}

	if needsUpdate {
		u.Render()
	}
}

func (u *UiController) fitPanelsToTerminal(resize ui.Resize) {
	u.grid.SetRect(0, 0, resize.Width, resize.Height)
}
