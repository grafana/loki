package loki

import (
	"encoding/json"
	"net/http"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/server"
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
