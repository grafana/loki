package query

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/prometheus/common/model"

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

	ui "github.com/grafana/termui/v3"
	"github.com/grafana/termui/v3/widgets"
)

var (
	availableColors = []ui.Color{
		ui.ColorBlack,
		ui.ColorRed,
		ui.ColorGreen,
		ui.ColorYellow,
		ui.ColorBlue,
		ui.ColorMagenta,
		ui.ColorCyan,
		ui.ColorWhite,
		ui.ColorPurple,
		ui.ColorOrange,
		ui.ColorPink,
		ui.ColorLightBlue,
		ui.ColorLightGreen,
		ui.ColorLightPurple,
		ui.ColorLightYellow,
		ui.ColorLightOrange,
	}
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

	Pretty   bool
	Live     bool
	PlotType string
	UiCtrl   UiController
}

type PlotType uint

const (
	Lines PlotType = iota
	StackedLines
	Bars
	StackedBars
	Points
	Table
)

func ParsePlotType(s string) (PlotType, error) {
	switch strings.ToLower(s) {
	case "lines":
		return Lines, nil
	case "stacked-lines":
		return StackedLines, nil
	case "bars":
		return Bars, nil
	case "stacked-bars":
		return StackedBars, nil
	case "points":
		return Points, nil
	default:
		return Lines, fmt.Errorf("unknown plot type: %s", s)
	}
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
	grid       *ui.Grid
	queryPanel *widgets.Paragraph

	plotType        PlotType
	graphPanel      *widgets.Plot
	stackedBarPanel *widgets.StackedBarChart
	legendPanel     *widgets.List
	legendDetail    *widgets.Paragraph

	showStats  bool
	statsPanel *widgets.Paragraph

	tablePanel   *widgets.Table
	tableDetails *widgets.Paragraph

	hiddenLabels []string

	currentMatrix loghttp.Matrix

	loadPanel *widgets.Paragraph
}

type UiPanelMeta struct {
	widget         ui.Drawable
	uiEventHandler func(ui.Event) bool
}

func NewUiController(showStats bool, plotType PlotType) UiController {
	uiCtrl := UiController{
		plotType: plotType,
	}

	uiCtrl.queryPanel = widgets.NewParagraph()
	uiCtrl.queryPanel.Title = "Query"
	uiCtrl.queryPanel.WrapText = true

	uiCtrl.grid = ui.NewGrid()
	uiCtrl.grid.SetRect(0, 0, 0, 0) // Will be updated on Init()

	statsPanel := widgets.NewParagraph()
	statsPanel.Title = "Statistics"

	uiCtrl.loadPanel = widgets.NewParagraph()
	uiCtrl.loadPanel.Title = "Loading"

	if plotType == Table {
		uiCtrl.tablePanel = widgets.NewTable()
		uiCtrl.tablePanel.Title = "Results"
		uiCtrl.tableDetails = widgets.NewParagraph()
		uiCtrl.tableDetails.Title = "Table Details"
		uiCtrl.tableDetails.WrapText = true
		uiCtrl.tablePanel.RowStyles[0] = ui.NewStyle(ui.ColorWhite, ui.ColorClear, ui.ModifierBold)

		if showStats {
			uiCtrl.statsPanel = statsPanel

			uiCtrl.grid.Set(
				ui.NewRow(0.5/5, ui.NewCol(1.0, uiCtrl.queryPanel)),
				ui.NewRow(3.0/5, ui.NewCol(3.0/4, uiCtrl.tablePanel), ui.NewCol(1.0/4, uiCtrl.statsPanel)),
				ui.NewRow(0.5/5, ui.NewCol(1.0, uiCtrl.tableDetails)),
			)
		} else {
			uiCtrl.grid.Set(
				ui.NewRow(0.5/5, ui.NewCol(1.0, uiCtrl.queryPanel)),
				ui.NewRow(3.0/5, ui.NewCol(1.0, uiCtrl.tablePanel)),
				ui.NewRow(0.5/5, ui.NewCol(1.0, uiCtrl.tableDetails)),
			)
		}
	} else {
		var graphPanel ui.Drawable

		if plotType == Lines || plotType == Points {
			uiCtrl.graphPanel = widgets.NewPlot()
			uiCtrl.graphPanel.Title = "Graph"
			uiCtrl.graphPanel.AxesColor = ui.ColorWhite
			uiCtrl.graphPanel.XAxisFmter = func(v int) string {
				return uiCtrl.graphPanel.DataLabels[v]
			}

			if plotType == Lines {
				uiCtrl.graphPanel.PlotType = widgets.LineChart
				uiCtrl.graphPanel.Marker = widgets.MarkerBraille
			} else {
				uiCtrl.graphPanel.PlotType = widgets.ScatterPlot
				uiCtrl.graphPanel.Marker = widgets.MarkerDot
			}

			graphPanel = uiCtrl.graphPanel
		} else if plotType == StackedBars {
			uiCtrl.stackedBarPanel = widgets.NewStackedBarChart()
			uiCtrl.stackedBarPanel.Title = "Graph"
			uiCtrl.stackedBarPanel.BarWidth = 1
			uiCtrl.stackedBarPanel.BarGap = 0

			graphPanel = uiCtrl.stackedBarPanel
		}

		uiCtrl.legendPanel = widgets.NewList()
		uiCtrl.legendPanel.Title = "Legend"
		uiCtrl.legendPanel.WrapText = false
		uiCtrl.legendPanel.SelectedRowStyle = ui.NewStyle(ui.ColorBlack, ui.ColorGreen)

		uiCtrl.legendDetail = widgets.NewParagraph()
		uiCtrl.legendDetail.Title = "Legend Detail"
		uiCtrl.legendDetail.WrapText = true

		if showStats {
			uiCtrl.statsPanel = statsPanel

			uiCtrl.grid.Set(
				ui.NewRow(0.5/5, ui.NewCol(1.0, uiCtrl.queryPanel)),
				ui.NewRow(3.0/5, ui.NewCol(3.0/4, graphPanel), ui.NewCol(1.0/4, statsPanel)),
				ui.NewRow(1.0/5, ui.NewCol(1.0, uiCtrl.legendPanel)),
				ui.NewRow(0.5/5, ui.NewCol(1.0, uiCtrl.legendDetail)),
			)
		} else {
			uiCtrl.grid.Set(
				ui.NewRow(0.5/5, ui.NewCol(1.0, uiCtrl.queryPanel)),
				ui.NewRow(3.0/5, ui.NewCol(1.0, graphPanel)),
				ui.NewRow(1.0/5, ui.NewCol(1.0, uiCtrl.legendPanel)),
				ui.NewRow(0.5/5, ui.NewCol(1.0, uiCtrl.legendDetail)),
			)
		}
	}

	return uiCtrl
}

func (u *UiController) Init() error {
	err := ui.Init()
	if err != nil {
		return err
	}

	width, height := ui.TerminalDimensions()
	u.grid.SetRect(0, 0, width, height)

	u.loadPanel.SetRect(0, 0, 60, 10)
	go u.updateLoadPanel()

	return nil
}

func (u *UiController) updateLoadPanel() {
	loadPanelFrames := [...]string{
		`
 _                      _ _
| |                    | (_)
| |      ___   ____  _ | |_ ____   ____
| |     / _ \ / _  |/ || | |  _ \ / _  |
| |____| |_| ( ( | ( (_| | | | | ( ( | |   _
|_______)___/ \_||_|\____|_|_| |_|\_|| |  (_)
                                 (_____|
`,
		`
 _                      _ _
| |                    | (_)
| |      ___   ____  _ | |_ ____   ____
| |     / _ \ / _  |/ || | |  _ \ / _  |
| |____| |_| ( ( | ( (_| | | | | ( ( | |   _    _
|_______)___/ \_||_|\____|_|_| |_|\_|| |  (_)  (_)
                                 (_____|
`,
		`
 _                      _ _
| |                    | (_)
| |      ___   ____  _ | |_ ____   ____
| |     / _ \ / _  |/ || | |  _ \ / _  |
| |____| |_| ( ( | ( (_| | | | | ( ( | |   _    _    _
|_______)___/ \_||_|\____|_|_| |_|\_|| |  (_)  (_)  (_)
                                 (_____|
`,
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	frame := 0

	for {
		select {
		case <-ticker.C:
			if u.loadPanel == nil {
				return
			}

			u.loadPanel.Text = loadPanelFrames[frame]

			frame++
			if frame > (len(loadPanelFrames) - 1) {
				frame = 0
			}

			ui.Render(u.loadPanel)
		}
	}
}

func (u *UiController) Close() {
	ui.Close()
}

func (u *UiController) Render() {
	u.loadPanel = nil

	ui.Clear()
	ui.Render(u.grid)
}

func (u *UiController) UpdateQuery(query string) {
	u.queryPanel.Text = query
}

func (u *UiController) UpdateGraph(matrix loghttp.Matrix) {
	data := make([][]float64, 0)
	colors := make([]ui.Color, 0)
	labels := make([]string, len(matrix))

	// Sort series alphabetically. This is needed so the legend is always in the same order.
	sort.Slice(matrix, func(i, j int) bool {
		return matrix[i].Metric.FastFingerprint() < matrix[j].Metric.FastFingerprint()
	})

	largestStreamSize := -1
	largestStreamIdx := 0
	for i, stream := range matrix {
		rowLabel := stream.Metric.String()

		// Only the series not hidden will have other value than 0.
		if !u.IsLabelHidden(rowLabel) {
			fv := make([]float64, len(stream.Values))
			for i, v := range stream.Values {
				fv[i] = float64(v.Value)
			}

			data = append(data, fv)
			colors = append(colors, u.GetColorForLabels(stream.Metric))
		}

		streamSize := len(stream.Values)
		if streamSize > largestStreamSize {
			largestStreamSize = streamSize
			largestStreamIdx = i
		}

		labels[i] = rowLabel
	}

	timestamps := make([]string, largestStreamSize)
	for i, value := range matrix[largestStreamIdx].Values {
		timestamps[i] = value.Timestamp.Time().UTC().Format("2006-01-02 15:04:05")
	}

	switch u.plotType {
	case Lines, Points:
		u.graphPanel.Data = data
		u.graphPanel.DataLabels = timestamps
		u.graphPanel.LineColors = colors
	case StackedBars:
		u.stackedBarPanel.Data = transpose(data)
		u.stackedBarPanel.BarColors = colors
	}

	u.legendPanel.Rows = u.GetLegend(labels, colors)
	u.UpdateLegendDetail()

	u.currentMatrix = matrix
}

func transpose(slice [][]float64) [][]float64 {
	maxSize := -1
	for i := range slice {
		size := len(slice[i])
		if size > maxSize {
			maxSize = size
		}
	}

	witdh := maxSize
	height := len(slice)

	result := make([][]float64, witdh)
	for i := range result {
		result[i] = make([]float64, height)
	}

	for i := 0; i < witdh; i++ {
		for j := 0; j < height; j++ {
			if i < len(slice[j]) {
				result[i][j] = slice[j][i]
			} else {
				result[i][j] = 0
			}
		}
	}

	return result
}

func (u *UiController) CreateTable(vector loghttp.Vector) {
	tableRows := make([][]string, len(vector)+1)
	keyRow := make([]string, 0)
	masterKeys := make(map[model.LabelName]bool)
	sortedMasterKeys := make([]model.LabelName, 0)

	for _, sample := range vector {
		for key := range sample.Metric {
			masterKeys[key] = true
		}
	}

	for key := range masterKeys {
		sortedMasterKeys = append(sortedMasterKeys, key)
	}

	// Sort row of keys
	sort.Slice(sortedMasterKeys, func(i, j int) bool {
		return sortedMasterKeys[i] < sortedMasterKeys[j]
	})

	for _, val := range sortedMasterKeys {
		keyRow = append(keyRow, string(val))
	}
	keyRow = append(keyRow, "timestamp")
	keyRow = append(keyRow, "value")

	tableRows[0] = keyRow

	for i, sample := range vector {
		row := make([]string, 0)

		for _, v := range sortedMasterKeys {
			row = append(row, string(sample.Metric[v]))
		}
		row = append(row, sample.Timestamp.String())
		row = append(row, sample.Value.String())

		tableRows[i+1] = row
	}

	u.tablePanel.Rows = tableRows
	u.UpdateTableDetails()

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
	cIndex := int(labels.FastFingerprint()) % len(availableColors)

	// NOTE(kavi): why Abs: because Fingerprint is uint64 and we need int for index operation.
	// converting from uint64 -> int can overflow to negative int. (happened when I tested it with loki-ops with huge number of streams)
	cIndex = int(math.Abs(float64(cIndex)))

	return availableColors[cIndex]
}

func (u *UiController) IsLabelHidden(label string) bool {
	for _, hiddenLabel := range u.hiddenLabels {
		if strings.Contains(hiddenLabel, label) {
			return true
		}

		if strings.Contains(label, hiddenLabel) {
			return true
		}
	}
	return false
}

func (u *UiController) GetLegend(labels []string, colors []ui.Color) []string {
	reverseStyleParserColorMap := make(map[ui.Color]string, len(ui.StyleParserColorMap))
	for k, v := range ui.StyleParserColorMap {
		reverseStyleParserColorMap[v] = k
	}

	legend := make([]string, len(labels))
	var colorIndex int
	for i, l := range labels {
		if u.IsLabelHidden(l) {
			legend[i] = u.GetHiddenLabel(l)
		} else {
			labelColor := colors[colorIndex]
			legend[i] = fmt.Sprintf("[%s](fg:%s)", l, reverseStyleParserColorMap[labelColor])

			// We have colors only for unhidden labels.
			colorIndex++
		}
	}

	return legend
}

func (q *Query) printVector(vector loghttp.Vector) {
	if q.Pretty {
		q.UiCtrl.CreateTable(vector)
		return
	}

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

func (u *UiController) HandleUiEvent(e ui.Event) bool {
	needsRenderUpdate := true
	needsGraphUpdate := false
	stop := false

	switch {
	case e.ID == "q" || e.ID == "<C-c>":
		stop = true
	case e.Type == ui.ResizeEvent:
		u.fitPanelsToTerminal(e.Payload.(ui.Resize))
	case e.ID == "j" || e.ID == "<Down>":
		if u.legendPanel != nil {
			u.legendPanel.ScrollDown()
			u.UpdateLegendDetail()
		} else if u.tablePanel != nil {
			u.tablePanel.ScrollDown()
			u.UpdateTableDetails()
		}
	case e.ID == "k" || e.ID == "<Up>":
		if u.legendPanel != nil {
			u.legendPanel.ScrollUp()
			u.UpdateLegendDetail()
		} else if u.tablePanel != nil {
			u.tablePanel.ScrollUp()
			u.UpdateTableDetails()
		}
	case e.ID == "<Enter>" && u.legendPanel != nil:
		selectedLabel := u.currentMatrix[u.legendPanel.SelectedRow].Metric.String()
		if u.IsLabelHidden(selectedLabel) {
			u.UnhideLabel(selectedLabel)
		} else {
			u.HideLabel(selectedLabel)
		}

		needsGraphUpdate = true
	case e.ID == "a":
		for _, sample := range u.currentMatrix {
			u.HideLabel(sample.Metric.String())
		}
		needsGraphUpdate = true
	case e.ID == "z":
		u.hiddenLabels = make([]string, 0)
		needsGraphUpdate = true
	default:
		needsRenderUpdate = false
	}

	if needsRenderUpdate {
		u.Render()
	}
	if needsGraphUpdate {
		// The legend gets refreshed along with the graph
		u.UpdateGraph(u.currentMatrix)
	}

	return stop
}

func (u *UiController) fitPanelsToTerminal(resize ui.Resize) {
	u.grid.SetRect(0, 0, resize.Width, resize.Height)
}

func (u *UiController) UpdateLegendDetail() {
	if len(u.legendPanel.Rows) == 0 {
		return
	}

	u.legendDetail.Text = u.legendPanel.Rows[u.legendPanel.SelectedRow]
}

func (u *UiController) UpdateTableDetails() {
	if len(u.tablePanel.Rows) == 0 {
		return
	}

	rows := u.tablePanel.Rows[u.tablePanel.SelectedRow]
	u.tableDetails.Text = strings.Join(rows, ", ")
}

func (u *UiController) UnhideLabel(label string) {
	for i, hiddenLabel := range u.hiddenLabels {
		if strings.Contains(hiddenLabel, label) {
			u.hiddenLabels = append(u.hiddenLabels[:i], u.hiddenLabels[i+1:]...)
			break
		}
	}
}

func (u *UiController) HideLabel(label string) {
	hiddenlabel := u.GetHiddenLabel(label)
	u.hiddenLabels = append(u.hiddenLabels, hiddenlabel)
}

func (u *UiController) GetHiddenLabel(label string) string {
	return fmt.Sprintf("[HIDDEN](fg:bold,fg:black) [%s](fg:black)", label)
}
