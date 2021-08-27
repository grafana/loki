package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/grafana/loki/pkg/entitlement"
	grpc "google.golang.org/grpc"
)

type entitlementClientOptions struct {
	grpcHost   string
	grpcPort   int
	action     string
	orgid      string
	userid     string
	labelKey   string
	labelValue string
}

var options entitlementClientOptions = entitlementClientOptions{
	grpcHost: "localhost",
	grpcPort: 21001,
	userid:   "fake",
	orgid:    "fake",
}

func parseFlags() {
	flag.StringVar(&options.grpcHost, "grpcHost", options.grpcHost, "grpcHost")
	flag.IntVar(&options.grpcPort, "grpcPort", options.grpcPort, "gRPC port")
	flag.StringVar(&options.action, "action", options.action, "action {read|write}")
	flag.StringVar(&options.orgid, "orgid", options.userid, "orgid")
	flag.StringVar(&options.userid, "userid", options.userid, "useid")
	flag.StringVar(&options.labelValue, "value", options.labelValue, "label value")
	flag.Parse()
}

func main() {
	parseFlags()
	conn, err := grpc.Dial(fmt.Sprintf("%s:%d", options.grpcHost, options.grpcPort), grpc.WithInsecure())
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	client := entitlement.NewEntitlementClient(conn)
	message := &entitlement.EntitlementRequest{Action: options.action, LabelValue: options.labelValue, OrgID: options.orgid, UserID: options.userid}

	res, err := client.Entitled(context.TODO(), message)
	if err != nil {
		panic(err)
	}
	if res.Entitled {
		fmt.Println("Entitled")
	} else {
		fmt.Println("Not entitled")
	}
}
