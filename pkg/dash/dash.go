package dash

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"text/template"

	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"

	prom "github.com/prometheus/client_golang/prometheus"
)

func forceTpl(name, s string) *template.Template {
	res, _ := template.New(name).Parse(s)
	return res
}

func foo() {
	builder := dashboard.NewDashboardBuilder("Sample dashboard").
		Uid("generated-from-go").
		Tags([]string{"generated", "from", "go"}).
		Refresh("1m").
		Time("now-30m", "now").
		Timezone(common.TimeZoneBrowser).
		WithRow(dashboard.NewRowBuilder("Overview")).
		WithPanel(
			timeseries.NewPanelBuilder().
				Title("Network Received").
				Unit("bps").
				Min(0).
				WithTarget(
					prometheus.NewDataqueryBuilder().
						Expr(`rate(node_network_receive_bytes_total{job="integrations/raspberrypi-node", device!="lo"}[$__rate_interval]) * 8`).
						LegendFormat("{{ device }}"),
				),
		)

	sampleDashboard, err := builder.Build()
	if err != nil {
		panic(err)
	}
	dashboardJson, err := json.MarshalIndent(sampleDashboard, "", "  ")
	if err != nil {
		panic(err)
	}

	fmt.Println(string(dashboardJson))
}

type RedMethodBuilder struct {
	// * loki_request_duration_seconds
	// 1st panel = request rates, partitioned by status
	// 2nd panel = request latency distributions, partitioned by route
	// * loki_request_duration_seconds_
	// 3rd panel = per-pod request latency distributions, partitioned by route.

	// panel title
	title string

	// histogramvec
	metric *prom.HistogramVec

	// status(required)
	status map[string]string

	// topologyLabels(optional) are filters to limit metric selection,
	// e.g. `cluster=~"foo.*"` or `namespace="loki"`
	topologyLabels []string

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
		topologyLabels:  topologyLabels,
		partitionFields: partitions,
		drilldown:       drilldown,
	}
}

func (b *RedMethodBuilder) baseMetricName() string {
	ch := make(chan *prom.Desc, 1)
	b.metric.Describe(ch)
	return extractFQDN(<-ch)
}

func extractFQDN(desc *prom.Desc) string {
	s := desc.String()
	// trim unrelated
	after, _ := strings.CutPrefix(s, "Desc{fqName: ")
	// find where target ends
	n := strings.Index(after, ", help:")
	return after[:n]
}

func (b *RedMethodBuilder) TemplateArgs() TemplateArgs {
	return TemplateArgs{
		BaseName:        b.baseMetricName(),
		TopologyLabels:  strings.Join(b.topologyLabels, ", "),
		PartitionFields: strings.Join(b.partitionFields, ", "),
	}
}

type TemplateArgs struct {
	BaseName        string
	TopologyLabels  string
	PartitionFields string
}

// Map helps turn a template args struct into an abritrary map.
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

func (a TemplateArgs) Run(tpl *template.Template) string {
	return RunTemplate(tpl, a)
}

func RunTemplate(tpl *template.Template, args any) string {
	var b strings.Builder
	tpl.Execute(&b, args)
	return b.String()
}

var qpsTemplate = forceTpl(
	"qps",
	`
	sum(
		rate(
			{{.BaseName}}_sum{ {{.TopologyLabels}} }
			[$__rate_interval]
		)
	) by ({{.PartitionFields}})
	/
	sum(
		rate(
			{{.BaseName}}_count{ {{.TopologyLabels}} }
			[$__rate_interval]
		)
	) by ({{.PartitionFields}})
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
		) by ({{.PartitionFields}})
	)
	`,
)

func (b *RedMethodBuilder) QPSPanel() *timeseries.PanelBuilder {
	args := b.TemplateArgs()
	qry := prometheus.NewDataqueryBuilder().
		Expr(
			args.Run(qpsTemplate),
		).
		LegendFormat(
			fmt.Sprintf(
				`{{ %s }}`,
				args.PartitionFields,
			),
		)
		// TODO: how to assign predefined colors by status?

	return timeseries.NewPanelBuilder().
		Title(b.title).
		Unit("qps").
		Min(0).
		WithTarget(qry)
}

func (b *RedMethodBuilder) LatencyPanel() *timeseries.PanelBuilder {
	args := b.TemplateArgs()

	var queries []*prometheus.DataqueryBuilder

	for _, q := range []string{"50,90,99"} {
		extended := args.Map()

		// append the le field appropriately to the partition fields
		// we use in the queries
		extended["Quantile"] = "0." + q
		if extended["PartitionFields"] == "" {
			extended["PartitionFields"] = "le"
		} else {
			extended["PartitionFields"] = extended["PartitionFields"] + ", le"
		}

		legend := "p" + q
		// if partition fields are in use, we'll want to include them
		// in our legend
		if args.PartitionFields != "" {
			legend = args.PartitionFields + ", " + legend
		}

		qry := prometheus.NewDataqueryBuilder().
			Expr(
				RunTemplate(latencyTemplate, extended),
			).
			LegendFormat(
				fmt.Sprintf(
					`{{ %s }}`,
					legend,
				),
			)

		queries = append(queries, qry)
	}

	panel := timeseries.NewPanelBuilder().
		Title(b.title).
		Unit("latency").
		Min(0)

	for _, qry := range queries {
		panel = panel.WithTarget(qry)
	}

	return panel
}
