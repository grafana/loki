package logqlanalyzer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func CorsMiddleware() mux.MiddlewareFunc {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(200)
				return
			}
			h.ServeHTTP(w, r)
		})
	}
}

type LogQLAnalyzeHandler struct {
	analyzer logQLAnalyzer
}

func (s *LogQLAnalyzeHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	payload, err := io.ReadAll(req.Body)
	if err != nil {
		writeError(req.Context(), w, err, http.StatusBadRequest, "unable to read request body")
		return
	}
	requestBody := &Request{}
	err = json.Unmarshal(payload, requestBody)
	if err != nil {
		writeError(req.Context(), w, err, http.StatusBadRequest, "unable unmarshal request body")
		return
	}
	result, err := s.analyzer.analyze(requestBody.Query, requestBody.Logs)
	if err != nil {
		writeError(req.Context(), w, err, http.StatusBadRequest, "unable to analyze query")
		return
	}
	responseBody, err := json.Marshal(result)
	if err != nil {
		writeError(req.Context(), w, err, http.StatusInternalServerError, "can not marshal the response")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if n, err := w.Write(responseBody); err != nil {
		level.Error(util_log.WithContext(req.Context(), util_log.Logger)).Log("msg", "error writing response", "bytesWritten", n, "err", err)
	}
}

func writeError(ctx context.Context, w http.ResponseWriter, err error, statusCode int, msg string) {
	level.Error(util_log.WithContext(ctx, util_log.Logger)).Log("msg", msg, "err", err)
	http.Error(w, err.Error(), statusCode)
}

type Request struct {
	Query string   `json:"query"`
	Logs  []string `json:"logs"`
}

type Result struct {
	StreamSelector string       `json:"stream_selector"`
	Stages         []string     `json:"stages"`
	Results        []LineResult `json:"results"`
}

type LineResult struct {
	OriginLine   string        `json:"origin_line"`
	StageRecords []StageRecord `json:"stage_records"`
}

type StageRecord struct {
	LineBefore   string  `json:"line_before"`
	LabelsBefore []Label `json:"labels_before"`
	LineAfter    string  `json:"line_after"`
	LabelsAfter  []Label `json:"labels_after"`
	FilteredOut  bool    `json:"filtered_out"`
}

type Label struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
