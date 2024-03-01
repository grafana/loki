package loggerinfo

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"

	"github.com/grafana/loki/pkg/push"
	"github.com/prometheus/prometheus/model/labels"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/loggerinfo/drain"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
)

var loggerinfo LoggerInfo

type LoggerInfo struct {
	m sync.Mutex

	services map[string]*serviceLoggerInfo
}

const serviceNameLabel = "service_name"

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

	patterns *drain.Drain
}

var drainConfig = &drain.Config{
	LogClusterDepth: readConfig[int]("LOG_CLUSTER_DEPTH", strconv.Atoi, 4),
	SimTh:           0.4,
	MaxChildren:     100,
	ParamString:     "<*>",
	MaxClusters:     0,
}

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
		patterns: drain.New(drainConfig),
	}
}

func (s *serviceLoggerInfo) push(stream push.Stream) {
	for _, entry := range stream.Entries {
		s.patterns.Train(entry.Line, entry.Timestamp.UnixNano())
	}
}

type PatternsRequest struct {
	Expr      string `json:"query"`
	StartTime int64  `json:"from"`
	EndTime   int64  `json:"to"`
}

type PatternsResponse struct {
	Patterns []*Pattern `json:"patterns"`
}

type Pattern struct {
	Name    string     `json:"name"`
	Pattern string     `json:"pattern"`
	Samples []string   `json:"sampleLogLines"`
	Volume  [][2]int64 `json:"volumeTimeSeries"`
	Matches int64      `json:"matches"`
}

func Patterns(w http.ResponseWriter, r *http.Request) {
	var req PatternsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_, _ = w.Write([]byte(err.Error()))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	svcName, err := getServiceName(req.Expr)
	if err != nil {
		_, _ = w.Write([]byte(err.Error()))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	patterns := loggerinfo.serviceInstance(svcName).getPatterns(req.StartTime, req.EndTime)
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

func (s *serviceLoggerInfo) getPatterns(start, end int64) PatternsResponse {
	s.m.Lock()
	defer s.m.Unlock()
	resp := PatternsResponse{Patterns: make([]*Pattern, 0, 1<<10)}
	s.patterns.Iterate(func(cluster *drain.LogCluster) bool {
		volume := cluster.Volume.ForRange(start, end)
		matches := volume.Matches()
		if matches == 0 {
			return true
		}
		resp.Patterns = append(resp.Patterns, &Pattern{
			Name:    "", // TODO
			Pattern: cluster.String(),
			Matches: matches,
			Volume:  volume.Values,
			Samples: cluster.Samples,
		})
		return true
	})
	sort.Slice(resp.Patterns, func(i, j int) bool {
		return resp.Patterns[i].Pattern <= resp.Patterns[j].Pattern
	})
	return resp
}
