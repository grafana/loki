package main

import (
	"fmt"

	"github.com/grafana/dskit/server"

	"github.com/grafana/loki/v3/pkg/dash"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

func main() {
	metrics := server.NewServerMetrics(server.Config{MetricsNamespace: constants.Loki})
	dashboardLoader, err := dash.NewDashboardLoader(metrics)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(dashboardLoader.WritesDashboard()))
}
