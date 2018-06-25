package main

import (
	"flag"
	"net/http"
	"os"

	"github.com/opentracing-contrib/go-stdlib/nethttp"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/promlog"
	log "github.com/sirupsen/logrus"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"github.com/weaveworks/cortex/pkg/ring"
	"github.com/weaveworks/cortex/pkg/util"
	"google.golang.org/grpc"

	"github.com/grafana/logish/pkg/flagext"
	"github.com/grafana/logish/pkg/querier"
)

func main() {
	var (
		flagset           = flag.NewFlagSet("", flag.ExitOnError)
		operationNameFunc = nethttp.OperationNameFunc(func(r *http.Request) string {
			return r.URL.RequestURI()
		})
		serverConfig = server.Config{
			MetricsNamespace: "logish",
			GRPCMiddleware: []grpc.UnaryServerInterceptor{
				middleware.ServerUserHeaderInterceptor,
			},
		}
		ringConfig    ring.Config
		querierConfig querier.Config
		logLevel      = promlog.AllowedLevel{}
	)
	flagext.Var(flagset, &logLevel, "log.level", "info", "")
	flagext.RegisterConfigs(flagset, &serverConfig, &ringConfig, &querierConfig)
	flagset.Parse(os.Args[1:])

	logging.Setup(logLevel.String())
	util.InitLogger(logLevel)

	r, err := ring.New(ringConfig)
	if err != nil {
		log.Fatalf("Error initializing ring: %v", err)
	}
	defer r.Stop()

	querier, err := querier.New(querierConfig, r)
	if err != nil {
		log.Fatalf("Error initializing querier: %v", err)
	}

	server, err := server.New(serverConfig)
	if err != nil {
		log.Fatalf("Error initializing server: %v", err)
	}
	defer server.Shutdown()

	httpMiddleware := middleware.Merge(
		middleware.Func(func(handler http.Handler) http.Handler {
			return nethttp.Middleware(opentracing.GlobalTracer(), handler, operationNameFunc)
		}),
		middleware.AuthenticateUser,
	)

	server.HTTP.Handle("/api/prom/query", httpMiddleware.Wrap(http.HandlerFunc(querier.QueryHandler)))
	server.HTTP.Handle("/api/prom/label/{name}/values", httpMiddleware.Wrap(http.HandlerFunc(querier.LabelHandler)))
	server.Run()
}
