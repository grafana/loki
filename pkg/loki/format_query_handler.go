package loki

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/util/server"
)

func formatQueryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			statusCode = http.StatusOK
			status     = "success"
			formatted  string
			errStr     string
		)

		expr, err := syntax.ParseExpr(r.FormValue("query"))
		if err != nil {
			statusCode = http.StatusBadRequest
			status = "invalid-query"
			errStr = err.Error()
			if strings.Contains(errStr, "parse error at line 1") && strings.Contains(errStr, "unexpected IDENTIFIER") {
				// User probably sent a query like "foo", ie. they are trying
				// to use a grep like syntax. Help them find the right place to
				// submit a proper query
				errStr += " (queries should use LogQL: https://grafana.com/docs/loki/latest/logql/log_queries/)"
			}
		}

		if err == nil {
			formatted = syntax.Prettify(expr)
		}

		resp := FormatQueryResponse{
			Status: status,
			Data:   formatted,
			Err:    errStr,
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(statusCode)

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			server.WriteError(err, w)
		}

	}
}

type FormatQueryResponse struct {
	Status string `json:"status"`
	Data   string `json:"data,omitempty"`
	Err    string `json:"error,omitempty"`
}
