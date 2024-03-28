package loggerinfo

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/grafana/loki/pkg/push"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	// "github.com/grafana/loki/pkg/loggerinfo/drain"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/util"
)

var loggerinfo LoggerInfo

type LoggerInfo struct {
	m sync.Mutex

	services map[string]*serviceLoggerInfo
}

const serviceNameLabel = "service"

func Push(req *logproto.PushRequest) { loggerinfo.Push(req) }

func (s *LoggerInfo) Push(req *logproto.PushRequest) {
	for _, stream := range req.Streams {
		p, _ := promql_parser.ParseMetric(stream.Labels)
		if sn := p.Get(serviceNameLabel); sn != "" {
			s.serviceInstance(sn).push(stream)
		}
	}
}

func (s *LoggerInfo) serviceInstance(serviceName string) *serviceLoggerInfo {
	s.m.Lock()
	defer s.m.Unlock()
	if s.services == nil {
		s.services = make(map[string]*serviceLoggerInfo)
	}
	si, ok := s.services[serviceName]
	if !ok {
		si = newServiceLoggerInfo()
		s.services[serviceName] = si
	}
	return si
}

type serviceLoggerInfo struct {
	m sync.Mutex

	logfmtTokens []string
	// patterns     *drain.Drain
}

// var drainConfig = &drain.Config{
// 	LogClusterDepth: readConfig[int]("LOG_CLUSTER_DEPTH", strconv.Atoi, 4),
// 	SimTh:           readConfig[float64]("LOG_SIM_TH", parseFloat, 0.4),
// 	MaxChildren:     100,
// 	ParamString:     "<*>",
// 	MaxClusters:     0,
// }

func parseFloat(s string) (float64, error) { return strconv.ParseFloat(s, 64) }

func readConfig[T any](name string, fn func(string) (T, error), d T) T {
	v := os.Getenv(name)
	if v == "" {
		return d
	}
	c, err := fn(v)
	if err != nil {
		panic(err)
	}
	return c
}

func newServiceLoggerInfo() *serviceLoggerInfo {
	return &serviceLoggerInfo{
		logfmtTokens: readConfig[[]string]("LOG_LOGFMT_KEYS", parseLogFmtKeys, nil),
		// patterns:     drain.New(drainConfig),
	}
}

func parseLogFmtKeys(s string) ([]string, error) {
	return strings.Split(s, ","), nil
}

func (s *serviceLoggerInfo) push(stream push.Stream) {
	s.m.Lock()
	defer s.m.Unlock()
	for _, entry := range stream.Entries {
		if len(s.logfmtTokens) > 0 && IsLogFmt(entry.Line) {
			// tokens := TokenizeLogFmt(entry.Line, s.logfmtTokens...)
			// s.patterns.TrainTokens(entry.Line, tokens, LogFmtPattern, entry.Timestamp.UnixNano())
			continue
		}
		// s.patterns.Train(entry.Line, entry.Timestamp.UnixNano())
	}
}

type PatternsResponse struct {
	Patterns []*Pattern `json:"patterns"`
}

type Pattern struct {
	Name    string             `json:"name"`
	Pattern string             `json:"pattern"`
	Samples []string           `json:"sampleLogLines"`
	Volume  []model.SamplePair `json:"volumeTimeSeries"`
	Matches int64              `json:"matches"`
}

func Patterns(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		_, _ = w.Write([]byte(err.Error()))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	from, err := util.ParseTime(r.Form.Get("from"))
	if err != nil {
		_, _ = w.Write([]byte(err.Error()))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	to, err := util.ParseTime(r.Form.Get("to"))
	if err != nil {
		_, _ = w.Write([]byte(err.Error()))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	svcName, err := getServiceName(r.Form.Get("query"))
	if err != nil {
		_, _ = w.Write([]byte(err.Error()))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var minMatches int64
	if s := r.Form.Get("minMatches"); s != "" {
		if minMatches, err = strconv.ParseInt(s, 10, 64); err != nil {
			_, _ = w.Write([]byte(err.Error()))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	patterns := loggerinfo.serviceInstance(svcName).getPatterns(model.Time(from), model.Time(to), minMatches)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(patterns)
}

func getServiceName(expr string) (string, error) {
	matchers, err := syntax.ParseMatchers(expr, true)
	if err != nil {
		return "", err
	}
	for _, m := range matchers {
		if m.Name == serviceNameLabel && m.Type == labels.MatchEqual {
			return m.Value, nil
		}
	}
	return "", errors.New("service_name matcher is required")
}

// get patterns start and end are in milliseconds
func (s *serviceLoggerInfo) getPatterns(start, end model.Time, minMatches int64) PatternsResponse {
	s.m.Lock()
	defer s.m.Unlock()
	resp := PatternsResponse{Patterns: make([]*Pattern, 0, 1<<10)}
	// s.patterns.Iterate(func(cluster *drain.LogCluster) bool {
	// 	volume := cluster.Volume.ForRange(start, end)
	// 	matches := volume.Matches()
	// 	if matches == 0 || (minMatches > 0 && matches < minMatches) {
	// 		return true
	// 	}
	// 	resp.Patterns = append(resp.Patterns, &Pattern{
	// 		Name:    "", // TODO
	// 		Pattern: cluster.String(),
	// 		Matches: matches,
	// 		Volume:  volume.Values,
	// 		Samples: cluster.Samples,
	// 	})
	// 	return true
	// })
	// sort.Slice(resp.Patterns, func(i, j int) bool {
	// 	return resp.Patterns[i].Pattern <= resp.Patterns[j].Pattern
	// })
	return resp
}
