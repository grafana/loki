package loki

import (
	"fmt"
	"os"

	"github.com/opentracing/opentracing-go"
	"github.com/openzipkin/zipkin-go-opentracing"
)

func NewTracer() (opentracing.Tracer, error) {
	// create recorder.
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	recorder := zipkintracer.NewRecorder(globalCollector, false, hostname, "")

	// create tracer.
	tracer, err := zipkintracer.NewTracer(recorder)
	if err != nil {
		fmt.Printf("unable to create Zipkin tracer: %+v", err)
		os.Exit(-1)
	}

	return tracer, nil
}
