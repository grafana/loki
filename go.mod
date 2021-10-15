module github.com/grafana/loki

go 1.15

require (
	cloud.google.com/go/bigtable v1.3.0
	cloud.google.com/go/kms v1.0.0 // indirect
	cloud.google.com/go/pubsub v1.3.1
	cloud.google.com/go/storage v1.10.0
	github.com/Azure/azure-pipeline-go v0.2.3
	github.com/Azure/azure-storage-blob-go v0.13.0
	github.com/Masterminds/sprig/v3 v3.2.2
	github.com/NYTimes/gziphandler v1.1.1
	github.com/Workiva/go-datastructures v1.0.53
	github.com/alicebob/miniredis/v2 v2.14.3
	github.com/aws/aws-sdk-go v1.40.37
	github.com/bmatcuk/doublestar v1.2.2
	github.com/bradfitz/gomemcache v0.0.0-20190913173617-a41fca850d0b
	github.com/buger/jsonparser v1.1.1
	github.com/c2h5oh/datasize v0.0.0-20200112174442-28bbd4740fee
	github.com/cespare/xxhash v1.1.0
	github.com/cespare/xxhash/v2 v2.1.2
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf
	github.com/cortexproject/cortex v1.10.1-0.20211014125347-85c378182d0d
	github.com/davecgh/go-spew v1.1.1
	github.com/docker/docker v20.10.8+incompatible
	github.com/docker/go-plugins-helpers v0.0.0-20181025120712-1e6269c305b8
	github.com/drone/envsubst v1.0.2
	github.com/dustin/go-humanize v1.0.0
	github.com/facette/natsort v0.0.0-20181210072756-2cd4dd1e2dcb
	github.com/fatih/color v1.9.0
	github.com/felixge/fgprof v0.9.1
	github.com/fluent/fluent-bit-go v0.0.0-20190925192703-ea13c021720c
	github.com/fsouza/fake-gcs-server v1.7.0
	github.com/go-kit/log v0.2.0
	github.com/go-logfmt/logfmt v0.5.1
	github.com/go-redis/redis/v8 v8.11.4
	github.com/gocql/gocql v0.0.0-20200526081602-cd04bd7f22a7
	github.com/gofrs/flock v0.7.1 // indirect
	github.com/gofrs/uuid v4.0.0+incompatible
	github.com/gogo/protobuf v1.3.2 // remember to update loki-build-image/Dockerfile too
	github.com/golang/protobuf v1.5.2
	github.com/golang/snappy v0.0.4
	github.com/google/go-cmp v0.5.6
	github.com/gorilla/mux v1.8.0
	github.com/gorilla/websocket v1.4.2
	github.com/grafana/dskit v0.0.0-20211011144203-3a88ec0b675f
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0
	github.com/grpc-ecosystem/grpc-opentracing v0.0.0-20180507213350-8e809c8a8645
	github.com/hashicorp/consul/api v1.10.1
	github.com/hashicorp/golang-lru v0.5.4
	github.com/hpcloud/tail v1.0.0
	github.com/imdario/mergo v0.3.11
	github.com/influxdata/go-syslog/v3 v3.0.1-0.20201128200927-a1889d947b48
	github.com/influxdata/telegraf v1.16.3
	github.com/jmespath/go-jmespath v0.4.0
	github.com/joncrlsn/dque v2.2.1-0.20200515025108-956d14155fa2+incompatible
	github.com/json-iterator/go v1.1.11
	github.com/klauspost/compress v1.13.1
	github.com/klauspost/pgzip v1.2.5
	github.com/minio/minio-go/v7 v7.0.10
	github.com/mitchellh/mapstructure v1.4.1
	github.com/modern-go/reflect2 v1.0.1
	github.com/mwitkow/go-conntrack v0.0.0-20190716064945-2f068394615f
	github.com/ncw/swift v1.0.52
	github.com/oklog/run v1.1.0
	github.com/oklog/ulid v1.3.1
	github.com/opentracing-contrib/go-grpc v0.0.0-20210225150812-73cb765af46e
	github.com/opentracing-contrib/go-stdlib v1.0.0
	github.com/opentracing/opentracing-go v1.2.0
	// github.com/pierrec/lz4 v2.0.5+incompatible
	github.com/pierrec/lz4/v4 v4.1.7
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.31.1
	github.com/prometheus/prometheus v1.8.2-0.20211011171444-354d8d2ecfac
	github.com/segmentio/fasthash v1.0.2
	github.com/shurcooL/httpfs v0.0.0-20190707220628-8d4bc4ba7749
	github.com/shurcooL/vfsgen v0.0.0-20200824052919-0d455de96546
	github.com/sony/gobreaker v0.4.1
	github.com/spf13/afero v1.3.4
	github.com/stretchr/testify v1.7.0
	github.com/thanos-io/thanos v0.22.0
	github.com/tonistiigi/fifo v0.0.0-20190226154929-a9fb20d87448
	github.com/uber/jaeger-client-go v2.29.1+incompatible
	github.com/weaveworks/common v0.0.0-20211015155308-ebe5bdc2c89e
	go.etcd.io/bbolt v1.3.6
	go.uber.org/atomic v1.9.0
	go.uber.org/goleak v1.1.10
	golang.org/x/crypto v0.0.0-20210616213533-5ff15b29337e
	golang.org/x/net v0.0.0-20210903162142-ad29c8ab022f
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/sys v0.0.0-20210908233432-aa78b53d3365
	golang.org/x/time v0.0.0-20210723032227-1f47c861a9ac
	google.golang.org/api v0.57.0
	google.golang.org/grpc v1.40.0
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	gopkg.in/fsnotify.v1 v1.4.7
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b
	inet.af/netaddr v0.0.0-20210707202901-70468d781e6c
	k8s.io/klog v1.0.0
)

// Upgrade to run with gRPC 1.3.0 and above.
replace github.com/sercand/kuberesolver => github.com/sercand/kuberesolver v2.4.0+incompatible

replace github.com/hpcloud/tail => github.com/grafana/tail v0.0.0-20201004203643-7aa4e4a91f03

replace github.com/Azure/azure-sdk-for-go => github.com/Azure/azure-sdk-for-go v36.2.0+incompatible

replace k8s.io/client-go => k8s.io/client-go v0.21.0

replace k8s.io/api => k8s.io/api v0.21.0

replace k8s.io/apimachinery => k8s.io/apimachinery v0.21.0

replace github.com/hashicorp/consul => github.com/hashicorp/consul v1.5.1

// Use fork of gocql that has gokit logs and Prometheus metrics.
replace github.com/gocql/gocql => github.com/grafana/gocql v0.0.0-20200605141915-ba5dc39ece85

// Same as Cortex
// Using a 3rd-party branch for custom dialer - see https://github.com/bradfitz/gomemcache/pull/86
replace github.com/bradfitz/gomemcache => github.com/themihai/gomemcache v0.0.0-20180902122335-24332e2d58ab

// We only pin this version to avoid problems with running go get: github.com/thanos-io/thanos@main. That
// currently fails because Thanos isn't merging release branches to main branch, and Go modules system is then
// confused about which version is the latest one. v0.22.0 was released in July, but latest tag reachable from main
// is v0.19.1. We pin version from late september here. Feel free to remove when updating to later version.
replace github.com/thanos-io/thanos v0.22.0 => github.com/thanos-io/thanos v0.19.1-0.20210923155558-c15594a03c45
