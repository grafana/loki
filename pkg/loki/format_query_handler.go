package loki

import (
	"fmt"
	"net/http"

	"github.com/grafana/loki/pkg/logql/syntax"
	serverutil "github.com/grafana/loki/pkg/util/server"
)

func formatQueryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expr, err := syntax.ParseExpr(r.FormValue("query"))
		if err != nil {
			serverutil.WriteError(err, w)
			return
		}

		w.Header().Set("Content-Type", "application/text")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, "%s", syntax.Prettify(expr))
	}
}
