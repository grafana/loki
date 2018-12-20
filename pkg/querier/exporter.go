package querier

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
)

// ExporterConfig for a exporter.
type ExporterConfig struct {
	RefreshRate         string        `yaml:"refresh_rate"`
	RefreshRateDuration time.Duration `yaml:"-"`

	Queries []struct {
		Name            string             `yaml:"name"`
		Query           string             `yaml:"query"`
		Limit           int64              `yaml:"limit"`
		Start           string             `yaml:"start"`
		StartDuration   time.Duration      `yaml:"-"`
		End             string             `yaml:"end"`
		EndDuration     time.Duration      `yaml:"-"`
		Direction       string             `yaml:"direction"`
		DirectionParsed logproto.Direction `yaml:"-"`
		Regexp          string             `yaml:"regexp"`
	} `yaml:"queries"`
}

// Exporter handles queries which should be exported for Prometheus.
type Exporter struct {
	cfg          ExporterConfig
	queryMetrics map[string]*prometheus.GaugeVec
}

// NewExporter makes a new Exporter.
func NewExporter(cfg ExporterConfig) (*Exporter, error) {
	refreshRateDuration, err := time.ParseDuration(cfg.RefreshRate)
	if err != nil {
		return nil, err
	}

	cfg.RefreshRateDuration = refreshRateDuration

	for index := 0; index < len(cfg.Queries); index++ {
		startDuration, err := time.ParseDuration(cfg.Queries[index].Start)
		if err != nil {
			return nil, err
		}

		endDuration, err := time.ParseDuration(cfg.Queries[index].End)
		if err != nil {
			return nil, err
		}

		var directionParsed logproto.Direction
		d, ok := logproto.Direction_value[strings.ToUpper(cfg.Queries[index].Direction)]
		if !ok {
			directionParsed = logproto.FORWARD
		} else {
			directionParsed = logproto.Direction(d)
		}

		cfg.Queries[index].StartDuration = startDuration
		cfg.Queries[index].EndDuration = endDuration
		cfg.Queries[index].DirectionParsed = directionParsed
	}

	return &Exporter{
		cfg:          cfg,
		queryMetrics: make(map[string]*prometheus.GaugeVec),
	}, nil
}

// RunExporter runs the defined queries
func RunExporter(q *Querier, e *Exporter) {
	for {
		for _, query := range e.cfg.Queries {
			request := logproto.QueryRequest{
				Query:     query.Query,
				Limit:     uint32(query.Limit),
				Start:     time.Now().Add(query.StartDuration),
				End:       time.Now().Add(query.EndDuration),
				Direction: query.DirectionParsed,
				Regex:     query.Regexp,
			}

			level.Debug(util.Logger).Log("request", fmt.Sprintf("%+v", request))

			ctx := user.InjectOrgID(context.Background(), "fake")
			result, err := q.Query(ctx, &request)
			if err != nil {
				level.Error(util.Logger).Log("message", "error while running the request", "error", err)
				continue
			}

			for _, stream := range result.Streams {
				labelNames := []string{}
				labels := prometheus.Labels{}
				labelValuePairs := strings.Split(stream.Labels[1:len(stream.Labels)-1], ",")

				hasher := md5.New()
				hasher.Write([]byte(stream.Labels))
				md5String := hex.EncodeToString(hasher.Sum(nil))
				name := "query_" + query.Name + "_labels_" + md5String

				for _, labelValuePair := range labelValuePairs {
					labelValuePairSlice := strings.Split(labelValuePair, "=")
					label := labelValuePairSlice[0]
					value := labelValuePairSlice[1]

					labels[strings.Trim(strings.TrimSpace(label), "_")] = strings.TrimSpace(value[1 : len(value)-1])
					labelNames = append(labelNames, strings.Trim(strings.TrimSpace(label), "_"))
				}

				e.addMetric(name, query.Name, labelNames, md5String)
				e.updateMetricValue(name, labels, float64(len(stream.Entries)))
			}
		}

		time.Sleep(e.cfg.RefreshRateDuration)
	}
}

func (e *Exporter) addMetric(name string, queryName string, labelNames []string, md5String string) {
	if _, ok := e.queryMetrics[name]; !ok {
		e.queryMetrics[name] = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   "loki",
			Name:        "query_" + queryName + "_total",
			Help:        "The total number of events for the " + queryName + " query.",
			ConstLabels: prometheus.Labels{"hash": md5String},
		}, labelNames)
	}
}

func (e *Exporter) updateMetricValue(name string, labels prometheus.Labels, value float64) {
	if _, ok := e.queryMetrics[name]; ok {
		e.queryMetrics[name].With(labels).Set(value)
	}
}
