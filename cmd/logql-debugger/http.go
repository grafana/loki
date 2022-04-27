package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"io/ioutil"
	"net/http"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/logql/syntax"
	util_log "github.com/grafana/loki/pkg/util/log"
)

func corsMiddlware() mux.MiddlewareFunc {
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

type debugServer struct {
}

func (s *debugServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	payload, err := ioutil.ReadAll(req.Body)
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
	expr, err := syntax.ParseLogSelector(requestBody.Query, true)
	if err != nil {
		writeError(req.Context(), w, err, http.StatusBadRequest, "invalid query")
		return
	}
	pipelineExpr, ok := expr.(*syntax.PipelineExpr)
	if !ok {
		writeError(req.Context(), w, fmt.Errorf("unsupported type of expression"), http.StatusBadRequest, "unsupported type of expression")
		return
	}
	stages := make([]string, 0, len(pipelineExpr.MultiStages))
	for _, stage := range pipelineExpr.MultiStages {
		stages = append(stages, stage.String())
	}
	pipeline, err := expr.Pipeline()
	if err != nil {
		writeError(req.Context(), w, err, http.StatusBadRequest, "can not create pipeline")
		return
	}
	debugger := log.NewPipelineDebugger(pipeline)
	response := Response{Stages: stages, Results: make([][]log.StageDebugRecord, 0, len(requestBody.Logs))}
	for _, line := range requestBody.Logs {
		debugRecords := debugger.DebugLine(line)
		response.Results = append(response.Results, debugRecords)
	}
	responseBody, err := json.Marshal(response)
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
	level.Error(util_log.WithContext(ctx, util_log.Logger)).Log("msg", msg, "err", err.Error())
	http.Error(w, err.Error(), statusCode)
}

type Request struct {
	Query string   `json:"query"`
	Logs  []string `json:"logs"`
}

type Response struct {
	Stages  []string                 `json:"stages"`
	Results [][]log.StageDebugRecord `json:"results"`
}
