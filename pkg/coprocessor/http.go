package coprocessor

import (
	"errors"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
	"net/http"

	"github.com/go-kit/log/level"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/util/marshal"
	serverutil "github.com/grafana/loki/pkg/util/server"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

func WrapQueryRangePreQuery(querierObserver QuerierObserver) middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if querierObserver == nil {
				next.ServeHTTP(w, req)
				return
			}
			request, err := loghttp.ParseRangeQuery(req)
			if err != nil {
				serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, err.Error()), w)
				return
			}
			log, newCtx := spanlogger.New(req.Context(), "querierObserver.PreQuery")
			pass, err := querierObserver.PreQuery(newCtx, *request)
			if err != nil {
				level.Warn(log).Log("msg", "querierObserver preScan fail", "err", err)
				next.ServeHTTP(w, req)
				return
			}

			if pass == true {
				next.ServeHTTP(w, req)
				return
			}
			//if pass==false, do not query backend,just return an empty result.
			result, err := genEmptyResult(request.Query)
			if err != nil {
				level.Warn(log).Log("msg", "querierObserver genEmptyResult fail", "err", err)
				next.ServeHTTP(w, req)
				return
			}
			if err := marshal.WriteQueryResponseJSON(result, w); err != nil {
				serverutil.WriteError(err, w)
				return
			}
		})
	})
}

func genEmptyResult(logql string) (logqlmodel.Result, error) {
	expr, err := syntax.ParseExpr(logql)
	if err != nil {
		return logqlmodel.Result{}, err
	}
	var value promql_parser.Value
	switch expr.(type) {
	case syntax.SampleExpr:
		value = promql.Matrix{}
	case syntax.LogSelectorExpr:
		value = logqlmodel.Streams{}
	default:
		return logqlmodel.Result{}, errors.New("unexpected type")
	}

	result := logqlmodel.Result{
		Data: value,
	}
	return result, nil
}
