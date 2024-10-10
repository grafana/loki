package dash

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"text/template"

	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/table"
	"github.com/grafana/grafana-foundation-sdk/go/testdata"
	"github.com/grafana/grafana-foundation-sdk/go/text"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
	prom "github.com/prometheus/client_golang/prometheus"
)

type DashboardLoader struct {
	ml DependencyLoader

	// individual dashboards, pre-rendered at initialization and
	// accessed by http handlers via corresponding CamelCase methods
	writes []byte
}

func NewDashboardLoader(ml DependencyLoader) (*DashboardLoader, error) {
	dl := &DashboardLoader{
		ml: ml,
	}

	writes, err := dl.writesDashboard()
	if err != nil {
		return nil, err
	}

	dashboardJSON, err := json.MarshalIndent(writes, "", "  ")
	if err != nil {
		return nil, err
	}

	dl.writes = dashboardJSON

	return dl, nil
}

func (l *DashboardLoader) WritesDashboard() []byte {
	return l.writes
}

func (l *DashboardLoader) Writes() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// TODO(owen-d): versioning
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(l.WritesDashboard())
	})
}

// TODO(owen-d): add a help panel describing the necessary labels present (e.g. "cluster", "namespace")
// and some instant queries that show whether they exist or not to help debug.
func (l *DashboardLoader) writesDashboard() (dashboard.Dashboard, error) {
	// TODO(owen-d): map statuses to colors here
	var statusMap map[string]string

	// TODO: parameterize so caller can pass it's own scheme
	// (not everyone may use our k8s style)
	topologyFilters := []string{`namespace=~".*loki.*"`}

	var (
		server        = l.ml.Server()
		index         = l.ml.Index()
		objectStorage = l.ml.ObjectStorage()
	)

	reds := []*RedMethodBuilder{
		NewRedMethodBuilder(
			"Distributor",
			server.RequestDuration,
			statusMap,
			append([]string{`container="distributor", route="loki_api_v1_push"`}, topologyFilters...),
			[]string{"status_code"},
			"pod",
		),
		NewRedMethodBuilder(
			"Ingester",
			server.RequestDuration,
			statusMap,
			append([]string{`container="ingester"`}, topologyFilters...),
			[]string{"status_code"},
			"pod",
		),
		NewRedMethodBuilder(
			"Index",
			index.IndexQueryLatency,
			statusMap,
			append([]string{`container="ingester"`, `operation="index_chunk"`}, topologyFilters...),
			[]string{"status_code"},
			"pod",
		),
		NewRedMethodBuilder(
			fmt.Sprintf("Object Storage - %s", objectStorage.Backend),
			objectStorage.RequestDuration,
			statusMap,
			append([]string{`container="ingester"`}, topologyFilters...),
			[]string{"status_code"},
			"pod",
		),
	}

	builder := dashboard.NewDashboardBuilder("Loki Writes (generated)").
		Uid("loki-writes-generated").
		Tags([]string{"generated", "from", "go"}).
		Refresh("1m").
		Time("now-30m", "now").
		Timezone(common.TimeZoneUtc)

	l.WelcomeRow(objectStorage, builder)

	for _, red := range reds {
		if err := red.Build(builder); err != nil {
			return dashboard.Dashboard{}, err
		}
	}

	return builder.Build()
}

// WelcomeRow adds 3 panels:
// * [text panel] A description
// * [instant query panel] Cell-metadata:
//   - A detected object storage backend (s3)
//   - the loki version running
//
// * [instant query panel] A list of required conventions
// and whether or not they're satisfied
//   - cluster, namespace, container labels
func (l *DashboardLoader) WelcomeRow(objectStorage *ObjectStorageMetrics, builder *dashboard.DashboardBuilder) {
	builder.WithRow(dashboard.NewRowBuilder("Welcome"))
	builder.WithPanel(
		text.NewPanelBuilder().
			Span(8).
			Title("Welcome to the Loki Writes Dashboard").
			Description("This dashboard provides insights into Loki's write path performance.").
			Transparent(true).
			Mode(text.TextModeMarkdown).
			Content("## Welcome to the Loki Writes Dashboard\n\nThis dashboard provides insights into Loki's write path performance, including request rates, latency distributions, and object storage metrics.\n\nThe panels below show key metrics and system information to help you monitor and optimize Loki's write operations."),
	)

	builder.WithPanel(
		table.NewPanelBuilder().
			Span(8).
			Title("System Information").
			Description("Key information about the Loki system").
			WithTarget(
				testdata.NewDataqueryBuilder().
					ScenarioId(testdata.DataqueryScenarioIdCsvContent).
					CsvContent(
						fmt.Sprintf(
							`Field,Value
Version,%s
Backend,%s`,
							l.ml.BuildInfo().Version,
							l.ml.ObjectStorage().Backend,
						),
					),
			),
	)

}

func forceTpl(name, s string) *template.Template {
	res, _ := template.New(name).Parse(s)
	return res
}

type RedMethodBuilder struct {
	// * loki_request_duration_seconds
	// 1st panel = request rates, partitioned by status
	// 2nd panel = request latency distributions, partitioned by route
	// * loki_request_duration_seconds_
	// 3rd panel = per-pod request latency distributions, partitioned by route.

	// row title
	title string

	// histogramvec
	metric *prom.HistogramVec

	// status(required)
	status map[string]string

	// topologyFilters(optional) are filters to limit metric selection,
	// e.g. `cluster=~"foo.*"` or `namespace="loki"`
	topologyFilters []string

	// partitionFields(optional)
	// e.g. "route"
	partitionFields []string

	// drilldown(optional)
	drilldown string
}

func NewRedMethodBuilder(
	title string,
	metric *prom.HistogramVec,
	status map[string]string,
	topologyLabels []string,
	partitions []string,
	drilldown string,
) *RedMethodBuilder {
	return &RedMethodBuilder{
		title:           title,
		metric:          metric,
		status:          status,
		topologyFilters: topologyLabels,
		partitionFields: partitions,
		drilldown:       drilldown,
	}
}

func (b *RedMethodBuilder) baseMetricName() string {
	ch := make(chan *prom.Desc, 1)
	b.metric.Describe(ch)
	return extractFQDN(<-ch)
}

// extracts the metric name from help string:
// `Desc{fqName: "test_metric", help: "", constLabels: {}, variableLabels: {}}`
// ->
// `test_metric`
func extractFQDN(desc *prom.Desc) string {
	s := desc.String()
	// trim unrelated
	after, _ := strings.CutPrefix(s, `Desc{fqName: "`)
	// find where target ends
	n := strings.Index(after, `", help:`)
	return after[:n]
}

func (b *RedMethodBuilder) TemplateArgs() TemplateArgs {
	return TemplateArgs{
		BaseName:        b.baseMetricName(),
		TopologyLabels:  strings.Join(b.topologyFilters, ", "),
		PartitionFields: strings.Join(b.partitionFields, ", "),
	}
}

type TemplateArgs struct {
	BaseName        string
	TopologyLabels  string
	PartitionFields string
}

// Map helps turn a template args struct into an arbitrary map.
// mainly useful for extending the list of keys to send to a template
func (a TemplateArgs) Map() map[string]string {
	ty := reflect.TypeOf(a)
	fields := reflect.VisibleFields(ty)
	res := make(map[string]string, len(fields))
	val := reflect.ValueOf(a)
	for _, f := range fields {
		res[f.Name] = val.FieldByName(f.Name).String()
	}
	return res
}

func (a TemplateArgs) Run(tpl *template.Template) (string, error) {
	return RunTemplate(tpl, a)
}

func RunTemplate(tpl *template.Template, args any) (string, error) {
	var b strings.Builder
	if err := tpl.Execute(&b, args); err != nil {
		return "", err
	}
	return b.String(), nil
}

var qpsTemplate = forceTpl(
	"qps",
	`
	sum(
		rate(
			{{.BaseName}}_count{ {{.TopologyLabels}} }
			[$__rate_interval]
		)
	) by ( {{.PartitionFields}} )
	`,
)

var latencyTemplate = forceTpl(
	"latency",
	`
	histogram_quantile(
	  {{.Quantile}},
		sum(
			rate(
			  {{.BaseName}}_bucket{ {{.TopologyLabels}} }
				[$__rate_interval]
			)
		) by (le)
	)
	* 1e3
	`,
)

func (b *RedMethodBuilder) QPSPanel() (*timeseries.PanelBuilder, error) {
	args := b.TemplateArgs()
	gen, err := args.Run(qpsTemplate)
	if err != nil {
		return nil, err
	}
	qry := prometheus.NewDataqueryBuilder().
		Expr(
			gen,
		).
		LegendFormat(
			fmt.Sprintf(
				`{{ %s }}`,
				args.PartitionFields,
			),
		)

		// TODO: how to assign predefined colors by status?
	return timeseries.NewPanelBuilder().
		Title("qps").
		Span(8).
		Min(0).
		Unit("short").
		WithTarget(qry).
		Legend(
			common.NewVizLegendOptionsBuilder().ShowLegend(true).DisplayMode(common.LegendDisplayModeList).Placement(common.LegendPlacementBottom),
		), nil
}

func (b *RedMethodBuilder) LatencyPanels() (res []cog.Builder[dashboard.Panel], err error) {
	rounds := []TemplateArgs{
		b.TemplateArgs(),
	}

	if b.drilldown != "" {
		next := b.TemplateArgs()
		if next.PartitionFields == "" {
			next.PartitionFields = b.drilldown
		} else {
			next.PartitionFields = next.PartitionFields + ", " + b.drilldown
		}

		rounds = append(rounds, next)
	}

	for _, args := range rounds {

		var queries []*prometheus.DataqueryBuilder

		for _, q := range []string{"50", "90", "99"} {
			extended := args.Map()

			// append the le field appropriately to the partition fields
			// we use in the queries
			extended["Quantile"] = "0." + q
			if extended["PartitionFields"] == "" {
				extended["PartitionFields"] = "le"
			} else {
				extended["PartitionFields"] = extended["PartitionFields"] + ", le"
			}

			gen, err := RunTemplate(latencyTemplate, extended)
			if err != nil {
				return nil, err
			}

			qry := prometheus.NewDataqueryBuilder().
				Expr(
					gen,
				).
				LegendFormat("p" + q)

			queries = append(queries, qry)
		}

		panel := timeseries.NewPanelBuilder().
			Title("latency").
			Span(8).
			Unit("ms").
			Min(0).
			Legend(
				common.NewVizLegendOptionsBuilder().ShowLegend(true).DisplayMode(common.LegendDisplayModeList).Placement(common.LegendPlacementBottom),
			)

		for _, qry := range queries {
			panel = panel.WithTarget(qry)
		}

		res = append(res, panel)
	}

	return res, nil
}

func (b *RedMethodBuilder) Row() *dashboard.RowBuilder {
	return dashboard.NewRowBuilder(b.title)
}

func (b *RedMethodBuilder) Panels() ([]cog.Builder[dashboard.Panel], error) {
	qpsPanel, err := b.QPSPanel()
	if err != nil {
		return nil, err
	}

	latencyPanels, err := b.LatencyPanels()
	if err != nil {
		return nil, err
	}

	return append([]cog.Builder[dashboard.Panel]{qpsPanel}, latencyPanels...), nil
}

func (b *RedMethodBuilder) Build(builder *dashboard.DashboardBuilder) error {
	builder.WithRow(b.Row())
	panels, err := b.Panels()
	if err != nil {
		return err
	}
	for _, panel := range panels {
		builder.WithPanel(panel)
	}
	return nil
}
