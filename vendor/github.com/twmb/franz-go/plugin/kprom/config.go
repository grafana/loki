package kprom

import (
	"reflect"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"maps"
)

type cfg struct {
	namespace string
	subsystem string

	reg      prometheus.Registerer
	gatherer prometheus.Gatherer

	withClientLabel  bool
	withConstLabels  prometheus.Labels
	histograms       map[Histogram][]float64
	defBuckets       []float64
	fetchProduceOpts fetchProduceOpts

	handlerOpts  promhttp.HandlerOpts
	goCollectors bool
}

func newCfg(namespace string, opts ...Opt) cfg {
	regGatherer := RegistererGatherer(prometheus.NewRegistry())
	cfg := cfg{
		namespace: namespace,
		reg:       regGatherer,
		gatherer:  regGatherer,

		defBuckets: DefBuckets,
		fetchProduceOpts: fetchProduceOpts{
			uncompressedBytes: true,
			labels:            []string{"node_id", "topic"},
		},
	}

	for _, opt := range opts {
		opt.apply(&cfg)
	}

	if cfg.goCollectors && cfg.reg != nil {
		cfg.reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
		cfg.reg.MustRegister(collectors.NewGoCollector())
	}

	return cfg
}

// Opt is an option to configure Metrics.
type Opt interface {
	apply(*cfg)
}

type opt struct{ fn func(*cfg) }

func (o opt) apply(c *cfg) { o.fn(c) }

type RegistererGatherer interface {
	prometheus.Registerer
	prometheus.Gatherer
}

// Registry sets the registerer and gatherer to add metrics to, rather than a
// new registry. Use this option if you want to configure both Gatherer and
// Registerer with the same object.
// Additionally, passing a `nil`  RegistererGatherer allows registering an external registry,
// exposing kprom as a custom prometheus collector, this option is mutually
// exclusive with GoCollectors
func Registry(rg RegistererGatherer) Opt {
	return opt{func(c *cfg) {
		if rg == nil || (reflect.ValueOf(rg).Kind() == reflect.Ptr && reflect.ValueOf(rg).IsNil()) {
			c.reg = nil
			c.gatherer = nil
		} else {
			c.reg = rg
			c.gatherer = rg
		}
	}}
}

// Registerer sets the registerer to add register to, rather than a new registry.
func Registerer(reg prometheus.Registerer) Opt {
	return opt{func(c *cfg) { c.reg = reg }}
}

// Gatherer sets the gatherer to add gather to, rather than a new registry.
func Gatherer(gatherer prometheus.Gatherer) Opt {
	return opt{func(c *cfg) { c.gatherer = gatherer }}
}

// GoCollectors adds the prometheus.NewProcessCollector and
// prometheus.NewGoCollector collectors the Metric's registry.
func GoCollectors() Opt {
	return opt{func(c *cfg) { c.goCollectors = true }}
}

// HandlerOpts sets handler options to use if you wish you use the
// Metrics.Handler function.
//
// This is only useful if you both (a) do not want to provide your own registry
// and (b) want to override the default handler options.
func HandlerOpts(opts promhttp.HandlerOpts) Opt {
	return opt{func(c *cfg) { c.handlerOpts = opts }}
}

// WithClientLabel adds a "cliend_id" label to all metrics.
func WithClientLabel() Opt {
	return opt{func(c *cfg) { c.withClientLabel = true }}
}

// WithStaticLabel adds a static label to all metrics.
func WithStaticLabel(labels prometheus.Labels) Opt {
	return opt{func(c *cfg) {
		c.withConstLabels = maps.Clone(labels)
	}}
}

// Subsystem sets the subsystem for the kprom metrics, overriding the default
// empty string.
func Subsystem(ss string) Opt {
	return opt{func(c *cfg) { c.subsystem = ss }}
}

// Buckets sets the buckets to be used with Histograms, overriding the default
// of [kprom.DefBuckets]. If custom buckets per histogram is needed,
// HistogramOpts can be used.
func Buckets(buckets []float64) Opt {
	return opt{func(c *cfg) { c.defBuckets = buckets }}
}

// DefBuckets are the default Histogram buckets. The default buckets are
// tailored to broadly measure the kafka timings (in seconds).
var DefBuckets = []float64{0.001, 0.002, 0.004, 0.008, 0.016, 0.032, 0.064, 0.128, 0.256, 0.512, 1.024, 2.048}

// A Histogram is an identifier for a kprom histogram that can be enabled
type Histogram uint8

const (
	ReadWait           Histogram = iota // Enables {ns}_{ss}_read_wait_seconds.
	ReadTime                            // Enables {ns}_{ss}_read_time_seconds.
	WriteWait                           // Enables {ns}_{ss}_write_wait_seconds.
	WriteTime                           // Enables {ns}_{ss}_write_time_seconds.
	RequestDurationE2E                  // Enables {ns}_{ss}_request_durationE2E_seconds.
	RequestThrottled                    // Enables {ns}_{ss}_request_throttled_seconds.
)

// HistogramOpts allows histograms to be enabled with custom buckets
type HistogramOpts struct {
	Enable  Histogram
	Buckets []float64
}

// HistogramsFromOpts allows the user full control of what histograms to enable
// and define buckets to be used with each histogram.
//
//	metrics, _ := kprom.NewMetrics(
//	 kprom.HistogramsFromOpts(
//	 	kprom.HistogramOpts{
//	 		Enable:  kprom.ReadWait,
//	 		Buckets: prometheus.LinearBuckets(10, 10, 8),
//	 	},
//	 	kprom.HistogramOpts{
//	 		Enable: kprom.ReadeTime,
//	 		// kprom default bucket will be used
//	 	},
//	 ),
//	)
func HistogramsFromOpts(hs ...HistogramOpts) Opt {
	return opt{func(c *cfg) {
		c.histograms = make(map[Histogram][]float64)
		for _, h := range hs {
			c.histograms[h.Enable] = h.Buckets
		}
	}}
}

// Histograms sets the histograms to be enabled for kprom, overiding the
// default of disabling all histograms.
//
//	metrics, _ := kprom.NewMetrics(
//		kprom.Histograms(
//			kprom.RequestDurationE2E,
//		),
//	)
func Histograms(hs ...Histogram) Opt {
	hos := make([]HistogramOpts, 0)
	for _, h := range hs {
		hos = append(hos, HistogramOpts{Enable: h})
	}
	return HistogramsFromOpts(hos...)
}

// A Detail is a label that can be set on fetch/produce metrics
type Detail uint8

const (
	ByNode            Detail = iota // Include label "node_id" for fetch and produce metrics.
	ByTopic                         // Include label "topic" for fetch and produce metrics.
	Batches                         // Report number of fetched and produced batches.
	Records                         // Report the number of fetched and produced records.
	CompressedBytes                 // Report the number of fetched and produced compressed bytes.
	UncompressedBytes               // Report the number of fetched and produced uncompressed bytes.
	ConsistentNaming                // Renames {fetch,produce}_bytes_total to {fetch,produce}_uncompressed_bytes_total, making the names consistent with the CompressedBytes detail.
)

type fetchProduceOpts struct {
	labels            []string
	batches           bool
	records           bool
	compressedBytes   bool
	uncompressedBytes bool
	consistentNaming  bool
}

// FetchAndProduceDetail determines details for fetch/produce metrics,
// overriding the default of (UncompressedBytes, ByTopic, ByNode).
func FetchAndProduceDetail(details ...Detail) Opt {
	return opt{
		func(c *cfg) {
			labelsDeduped := make(map[Detail]string)
			c.fetchProduceOpts = fetchProduceOpts{}
			for _, l := range details {
				switch l {
				case ByTopic:
					labelsDeduped[ByTopic] = "topic"
				case ByNode:
					labelsDeduped[ByNode] = "node_id"
				case Batches:
					c.fetchProduceOpts.batches = true
				case Records:
					c.fetchProduceOpts.records = true
				case UncompressedBytes:
					c.fetchProduceOpts.uncompressedBytes = true
				case CompressedBytes:
					c.fetchProduceOpts.compressedBytes = true
				case ConsistentNaming:
					c.fetchProduceOpts.consistentNaming = true
				}
			}
			var labels []string
			for _, l := range labelsDeduped {
				labels = append(labels, l)
			}
			c.fetchProduceOpts.labels = labels
		},
	}
}
