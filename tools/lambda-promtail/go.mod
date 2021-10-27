module main

go 1.16

require (
	github.com/aws/aws-lambda-go v1.26.0
	github.com/cortexproject/cortex v1.10.0
	github.com/gogo/protobuf v1.3.2
	github.com/golang/snappy v0.0.4
	github.com/grafana/loki v1.6.1
	github.com/prometheus/common v0.30.0
)

replace k8s.io/client-go => k8s.io/client-go v0.21.0
