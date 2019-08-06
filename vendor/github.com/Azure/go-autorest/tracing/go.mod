module github.com/Azure/go-autorest/tracing

go 1.12

require (
	// later releases of ocagent aren't compatible with our version of opencensus
	contrib.go.opencensus.io/exporter/ocagent v0.3.0
	// keep this pre-v0.22.0 to avoid dependency on protobuf v1.3+
	go.opencensus.io v0.21.0
)

// pin this to v0.1.0 to avoid breaking changes incompatible with our version of ocagent
replace github.com/census-instrumentation/opencensus-proto => github.com/census-instrumentation/opencensus-proto v0.1.0
