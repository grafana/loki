package main

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dash"
)

func main() {
	dashboardLoader, err := dash.NewDashboardLoader(dash.DummyLoader)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(dashboardLoader.WritesDashboard()))
}
