module github.com/grafana/loki

go 1.15

require (
	github.com/NYTimes/gziphandler v1.1.1
	github.com/aws/aws-lambda-go v1.17.0
	github.com/blang/semver v3.5.1+incompatible // indirect
	github.com/bmatcuk/doublestar v1.2.2
	github.com/c2h5oh/datasize v0.0.0-20200112174442-28bbd4740fee
	github.com/cespare/xxhash/v2 v2.1.1
	github.com/containerd/fifo v0.0.0-20190226154929-a9fb20d87448 // indirect
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf
	github.com/cortexproject/cortex v1.6.0
	github.com/davecgh/go-spew v1.1.1
	github.com/docker/docker v17.12.0-ce-rc1.0.20201009160326-9c15e82f19b0+incompatible
	github.com/docker/go-metrics v0.0.0-20181218153428-b84716841b82 // indirect
	github.com/docker/go-plugins-helpers v0.0.0-20181025120712-1e6269c305b8
	github.com/drone/envsubst v1.0.2
	github.com/dustin/go-humanize v1.0.0
	github.com/fatih/color v1.9.0
	github.com/fluent/fluent-bit-go v0.0.0-20190925192703-ea13c021720c
	github.com/go-kit/kit v0.10.0
	github.com/go-logfmt/logfmt v0.5.0
	github.com/gofrs/flock v0.7.1 // indirect
	github.com/gogo/protobuf v1.3.1 // remember to update loki-build-image/Dockerfile too
	github.com/golang/snappy v0.0.2
	github.com/gorilla/mux v1.7.3
	github.com/gorilla/websocket v1.4.2
	github.com/grpc-ecosystem/go-grpc-middleware v1.1.0
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.1-0.20191002090509-6af20e3a5340 // indirect
	github.com/grpc-ecosystem/grpc-opentracing v0.0.0-20180507213350-8e809c8a8645
	github.com/hashicorp/golang-lru v0.5.4
	github.com/hpcloud/tail v1.0.0
	github.com/imdario/mergo v0.3.9
	github.com/influxdata/go-syslog/v3 v3.0.1-0.20200510134747-836dce2cf6da
	github.com/jmespath/go-jmespath v0.4.0
	github.com/joncrlsn/dque v2.2.1-0.20200515025108-956d14155fa2+incompatible
	github.com/json-iterator/go v1.1.10
	github.com/klauspost/compress v1.11.3
	github.com/mitchellh/mapstructure v1.3.3
	github.com/moby/term v0.0.0-20200915141129-7f0af18e79f2 // indirect
	github.com/mwitkow/go-conntrack v0.0.0-20190716064945-2f068394615f
	github.com/opentracing/opentracing-go v1.2.0
	// github.com/pierrec/lz4 v2.0.5+incompatible
	github.com/pierrec/lz4/v4 v4.1.1
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.8.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.15.0
	github.com/prometheus/prometheus v1.8.2-0.20201119181812-c8f810083d3f
	github.com/segmentio/fasthash v1.0.2
	github.com/shurcooL/httpfs v0.0.0-20190707220628-8d4bc4ba7749
	github.com/shurcooL/vfsgen v0.0.0-20200824052919-0d455de96546
	github.com/stretchr/testify v1.6.1
	github.com/tonistiigi/fifo v0.0.0-20190226154929-a9fb20d87448
	github.com/uber/jaeger-client-go v2.25.0+incompatible
	github.com/ugorji/go v1.1.7 // indirect
	github.com/weaveworks/common v0.0.0-20201119133501-0619918236ec
	go.etcd.io/bbolt v1.3.5-0.20200615073812-232d8fc87f50
	go.uber.org/atomic v1.7.0
	golang.org/x/crypto v0.0.0-20201002170205-7f63de1d35b0
	golang.org/x/net v0.0.0-20201110031124-69a78807bb2b
	google.golang.org/grpc v1.33.1
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	gopkg.in/fsnotify.v1 v1.4.7
	gopkg.in/yaml.v2 v2.3.0
	gopkg.in/yaml.v3 v3.0.0-20200615113413-eeeca48fe776
	k8s.io/klog v1.0.0
)

replace github.com/hpcloud/tail => github.com/grafana/tail v0.0.0-20201004203643-7aa4e4a91f03

replace github.com/Azure/azure-sdk-for-go => github.com/Azure/azure-sdk-for-go v36.2.0+incompatible

// Keeping this same as Cortex to avoid dependency issues.
replace k8s.io/client-go => k8s.io/client-go v0.19.2

// >v1.2.0 has some conflict with prometheus/alertmanager. Hence prevent the upgrade till it's fixed.
replace github.com/satori/go.uuid => github.com/satori/go.uuid v1.2.0

// Use fork of gocql that has gokit logs and Prometheus metrics.
replace github.com/gocql/gocql => github.com/grafana/gocql v0.0.0-20200605141915-ba5dc39ece85

// Same as Cortex, we can't upgrade to grpc 1.30.0 until go.etcd.io/etcd will support it.
replace google.golang.org/grpc => google.golang.org/grpc v1.29.1

// Same as Cortex
// Using a 3rd-party branch for custom dialer - see https://github.com/bradfitz/gomemcache/pull/86
replace github.com/bradfitz/gomemcache => github.com/themihai/gomemcache v0.0.0-20180902122335-24332e2d58ab

// Fix errors like too many arguments in call to "github.com/go-openapi/errors".Required
//   have (string, string)
//   want (string, string, interface {})
replace github.com/go-openapi/errors => github.com/go-openapi/errors v0.19.4

replace github.com/go-openapi/validate => github.com/go-openapi/validate v0.19.8
