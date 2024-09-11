module github.com/grafana/loki/v3

go 1.21.8

require (
	cloud.google.com/go/bigtable v1.29.0
	cloud.google.com/go/pubsub v1.42.0
	cloud.google.com/go/storage v1.43.0
	github.com/Azure/azure-pipeline-go v0.2.3
	github.com/Azure/azure-storage-blob-go v0.14.0
	github.com/Azure/go-autorest/autorest/adal v0.9.24
	github.com/Azure/go-autorest/autorest/azure/auth v0.5.13
	github.com/Masterminds/sprig/v3 v3.2.3
	github.com/NYTimes/gziphandler v1.1.1
	github.com/Shopify/sarama v1.38.1
	github.com/Workiva/go-datastructures v1.1.5
	github.com/alicebob/miniredis/v2 v2.30.4
	github.com/aliyun/aliyun-oss-go-sdk v2.2.10+incompatible
	github.com/aws/aws-sdk-go v1.54.19
	github.com/baidubce/bce-sdk-go v0.9.189
	github.com/bmatcuk/doublestar v1.3.4
	github.com/c2h5oh/datasize v0.0.0-20231215233829-aa82cc1e6500
	github.com/cespare/xxhash v1.1.0
	github.com/cespare/xxhash/v2 v2.3.0
	github.com/containerd/fifo v1.1.0
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf
	github.com/cristalhq/hedgedhttp v0.9.1
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc
	github.com/docker/docker v27.1.2+incompatible
	github.com/docker/go-plugins-helpers v0.0.0-20240701071450-45e2431495c8
	github.com/drone/envsubst v1.0.3
	github.com/dustin/go-humanize v1.0.1
	github.com/facette/natsort v0.0.0-20181210072756-2cd4dd1e2dcb
	github.com/fatih/color v1.16.0
	github.com/felixge/fgprof v0.9.4
	github.com/fluent/fluent-bit-go v0.0.0-20230731091245-a7a013e2473c
	github.com/fsouza/fake-gcs-server v1.7.0
	github.com/go-kit/log v0.2.1
	github.com/go-logfmt/logfmt v0.6.0
	github.com/go-redis/redis/v8 v8.11.5
	github.com/gocql/gocql v0.0.0-20200526081602-cd04bd7f22a7
	github.com/gogo/protobuf v1.3.2 // remember to update loki-build-image/Dockerfile too
	github.com/gogo/status v1.1.1
	github.com/golang/protobuf v1.5.4
	github.com/golang/snappy v0.0.4
	github.com/google/go-cmp v0.6.0
	github.com/google/renameio/v2 v2.0.0
	github.com/google/uuid v1.6.0
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/websocket v1.5.3
	github.com/grafana/cloudflare-go v0.0.0-20230110200409-c627cf6792f2
	github.com/grafana/dskit v0.0.0-20240905221822-931a021fb06b
	github.com/grafana/go-gelf/v2 v2.0.1
	github.com/grafana/gomemcache v0.0.0-20240229205252-cd6a66d6fb56
	github.com/grafana/regexp v0.0.0-20240518133315-a468a5bfb3bc
	github.com/grafana/tail v0.0.0-20230510142333-77b18831edf0
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0
	github.com/grpc-ecosystem/grpc-opentracing v0.0.0-20180507213350-8e809c8a8645
	github.com/hashicorp/consul/api v1.29.4
	github.com/hashicorp/golang-lru v0.6.0
	github.com/imdario/mergo v0.3.16
	github.com/influxdata/telegraf v1.16.3
	github.com/jmespath/go-jmespath v0.4.0
	github.com/joncrlsn/dque v0.0.0-20211108142734-c2ef48c5192a
	github.com/json-iterator/go v1.1.12
	github.com/klauspost/compress v1.17.9
	github.com/klauspost/pgzip v1.2.6
	github.com/leodido/go-syslog/v4 v4.1.0
	github.com/mattn/go-ieproxy v0.0.12
	github.com/minio/minio-go/v7 v7.0.76
	github.com/mitchellh/go-wordwrap v1.0.1
	github.com/mitchellh/mapstructure v1.5.0
	github.com/modern-go/reflect2 v1.0.2
	github.com/mwitkow/go-conntrack v0.0.0-20190716064945-2f068394615f
	github.com/oklog/run v1.1.0
	github.com/oklog/ulid v1.3.1
	github.com/opentracing-contrib/go-grpc v0.0.0-20210225150812-73cb765af46e
	github.com/opentracing-contrib/go-stdlib v1.0.0
	github.com/opentracing/opentracing-go v1.2.0
	github.com/oschwald/geoip2-golang v1.11.0
	// github.com/pierrec/lz4 v2.0.5+incompatible
	github.com/pierrec/lz4/v4 v4.1.21
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.19.1
	github.com/prometheus/client_model v0.6.1
	github.com/prometheus/common v0.55.0
	github.com/prometheus/prometheus v0.53.2-0.20240726125539-d4f098ae80fb
	github.com/segmentio/fasthash v1.0.3
	github.com/shurcooL/httpfs v0.0.0-20230704072500-f1e31cf0ba5c
	github.com/shurcooL/vfsgen v0.0.0-20200824052919-0d455de96546
	github.com/sony/gobreaker v0.5.0
	github.com/spf13/afero v1.11.0
	github.com/stretchr/testify v1.9.0
	github.com/uber/jaeger-client-go v2.30.0+incompatible
	github.com/xdg-go/scram v1.1.2
	go.etcd.io/bbolt v1.3.10
	go.uber.org/atomic v1.11.0
	go.uber.org/goleak v1.3.0
	golang.org/x/crypto v0.26.0
	golang.org/x/net v0.28.0
	golang.org/x/sync v0.8.0
	golang.org/x/sys v0.24.0
	golang.org/x/time v0.6.0
	google.golang.org/api v0.193.0
	google.golang.org/grpc v1.65.0
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/klog v1.0.0
)

require (
	github.com/Azure/go-autorest/autorest v0.11.29
	github.com/DataDog/sketches-go v1.4.6
	github.com/DmitriyVTitov/size v1.5.0
	github.com/IBM/go-sdk-core/v5 v5.17.5
	github.com/IBM/ibm-cos-sdk-go v1.11.0
	github.com/axiomhq/hyperloglog v0.0.0-20240507144631-af9851f82b27
	github.com/buger/jsonparser v1.1.1
	github.com/coder/quartz v0.1.0
	github.com/d4l3k/messagediff v1.2.1
	github.com/dolthub/swiss v0.2.1
	github.com/efficientgo/core v1.0.0-rc.3
	github.com/fsnotify/fsnotify v1.7.0
	github.com/gogo/googleapis v1.4.1
	github.com/grafana/jsonparser v0.0.0-20240425183733-ea80629e1a32
	github.com/grafana/loki/pkg/push v0.0.0-20231124142027-e52380921608
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/hashicorp/raft v1.7.1
	github.com/hashicorp/raft-wal v0.4.1
	github.com/heroku/x v0.0.61
	github.com/influxdata/tdigest v0.0.2-0.20210216194612-fc98d27c9e8b
	github.com/mitchellh/colorstring v0.0.0-20190213212951-d06e56a500db
	github.com/ncw/swift/v2 v2.0.2
	github.com/prometheus/alertmanager v0.27.0
	github.com/prometheus/common/sigv4 v0.1.0
	github.com/richardartoul/molecule v1.0.0
	github.com/schollz/progressbar/v3 v3.14.6
	github.com/shirou/gopsutil/v4 v4.24.0-alpha.1
	github.com/thanos-io/objstore v0.0.0-20240818203309-0363dadfdfb1
	github.com/twmb/franz-go v1.17.1
	github.com/twmb/franz-go/pkg/kadm v1.13.0
	github.com/twmb/franz-go/pkg/kfake v0.0.0-20240821035758-b77dd13e2bfa
	github.com/twmb/franz-go/pkg/kmsg v1.8.0
	github.com/twmb/franz-go/plugin/kotel v1.5.0
	github.com/twmb/franz-go/plugin/kprom v1.1.0
	github.com/willf/bloom v2.0.3+incompatible
	go.opentelemetry.io/collector/pdata v1.12.0
	go4.org/netipx v0.0.0-20230125063823-8449b0a6169f
	golang.org/x/exp v0.0.0-20240325151524-a685a6edb6d8
	golang.org/x/oauth2 v0.22.0
	golang.org/x/text v0.17.0
	google.golang.org/protobuf v1.34.2
	gotest.tools v2.2.0+incompatible
	k8s.io/apimachinery v0.29.3
	k8s.io/utils v0.0.0-20240902221715-702e33fdd3c3
)

require (
	cel.dev/expr v0.16.0 // indirect
	cloud.google.com/go/auth v0.9.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.4 // indirect
	cloud.google.com/go/monitoring v1.21.0 // indirect
	github.com/AdaLogics/go-fuzz-headers v0.0.0-20230811130428-ced1acdcaa24 // indirect
	github.com/benbjohnson/immutable v0.4.0 // indirect
	github.com/coreos/etcd v3.3.27+incompatible // indirect
	github.com/coreos/pkg v0.0.0-20220810130054-c7d1c02cb6cf // indirect
	github.com/dlclark/regexp2 v1.4.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.3 // indirect
	github.com/go-ini/ini v1.67.0 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/goccy/go-json v0.10.3 // indirect
	github.com/hashicorp/go-msgpack/v2 v2.1.2 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/ncw/swift v1.0.53 // indirect
	github.com/pires/go-proxyproto v0.7.0 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.opentelemetry.io/otel/sdk v1.28.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.28.0 // indirect
)

require (
	cloud.google.com/go v0.115.1 // indirect
	cloud.google.com/go/compute/metadata v0.5.0 // indirect
	cloud.google.com/go/iam v1.2.0 // indirect
	cloud.google.com/go/longrunning v0.6.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.13.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.7.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.10.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5 v5.7.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4 v4.3.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v1.3.0 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20210617225240-d185dfc1b5a1 // indirect
	github.com/Azure/go-autorest v14.2.0+incompatible // indirect
	github.com/Azure/go-autorest/autorest/azure/cli v0.4.6 // indirect
	github.com/Azure/go-autorest/autorest/date v0.3.0 // indirect
	github.com/Azure/go-autorest/logger v0.2.1 // indirect
	github.com/Azure/go-autorest/tracing v0.6.0 // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.2.2 // indirect
	github.com/Code-Hex/go-generics-cache v1.5.1 // indirect
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/Masterminds/semver/v3 v3.2.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/alecthomas/chroma v0.10.0
	github.com/alecthomas/template v0.0.0-20190718012654-fb15b899a751 // indirect
	github.com/alecthomas/units v0.0.0-20240626203959-61d1e3462e30 // indirect
	github.com/alicebob/gopher-json v0.0.0-20200520072559-a9ecdc9d1d3a // indirect
	github.com/armon/go-metrics v0.4.1 // indirect
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/aws/aws-sdk-go-v2 v1.16.0 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.15.1 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.11.0 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.12.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.1.7 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.4.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.3.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.9.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.11.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.16.1 // indirect
	github.com/aws/smithy-go v1.11.1 // indirect
	github.com/bboreham/go-loser v0.0.0-20230920113527-fcc2c21820a3 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/census-instrumentation/opencensus-proto v0.4.1 // indirect
	github.com/cncf/xds/go v0.0.0-20240723142845-024c85f92f20 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/dennwc/varint v1.0.0 // indirect
	github.com/dgryski/go-metro v0.0.0-20180109044635-280f6062b5bc // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/digitalocean/godo v1.118.0 // indirect
	github.com/dimchansky/utfbom v1.1.1 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-metrics v0.0.1 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/dolthub/maphash v0.1.0 // indirect
	github.com/eapache/go-resiliency v1.3.0 // indirect
	github.com/eapache/go-xerial-snappy v0.0.0-20230111030713-bf00bc1b83b6 // indirect
	github.com/eapache/queue v1.1.0 // indirect
	github.com/edsrzf/mmap-go v1.1.0 // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/envoyproxy/go-control-plane v0.13.0 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.1.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-kit/kit v0.12.0 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/analysis v0.22.2 // indirect
	github.com/go-openapi/errors v0.22.0 // indirect
	github.com/go-openapi/jsonpointer v0.20.2 // indirect
	github.com/go-openapi/jsonreference v0.20.4 // indirect
	github.com/go-openapi/loads v0.21.5 // indirect
	github.com/go-openapi/spec v0.20.14 // indirect
	github.com/go-openapi/strfmt v0.23.0 // indirect
	github.com/go-openapi/swag v0.22.9 // indirect
	github.com/go-openapi/validate v0.23.0 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.19.0 // indirect
	github.com/go-zookeeper/zk v1.0.3 // indirect
	github.com/gofrs/flock v0.8.1 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.1 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20240711041743-f6c9dda6c6da // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.2 // indirect
	github.com/googleapis/gax-go/v2 v2.13.0 // indirect
	github.com/gophercloud/gophercloud v1.13.0 // indirect
	github.com/grafana/pyroscope-go/godeltaprof v0.1.8 // indirect
	github.com/hailocab/go-hostpool v0.0.0-20160125115350-e80d13ce29ed // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-msgpack v1.1.5 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.7 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/go-sockaddr v1.0.6 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/hashicorp/memberlist v0.5.0 // indirect
	github.com/hashicorp/serf v0.10.1 // indirect
	github.com/huandu/xstrings v1.3.3 // indirect
	github.com/jcmturner/aescts/v2 v2.0.0 // indirect
	github.com/jcmturner/dnsutils/v2 v2.0.0 // indirect
	github.com/jcmturner/gofork v1.7.6 // indirect
	github.com/jcmturner/gokrb5/v8 v8.4.3 // indirect
	github.com/jcmturner/rpc/v2 v2.0.3 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/julienschmidt/httprouter v1.3.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.8 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/leodido/ragel-machinery v0.0.0-20190525184631-5f46317e436b // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/miekg/dns v1.1.61 // indirect
	github.com/minio/md5-simd v1.1.2 // indirect
	github.com/mitchellh/copystructure v1.0.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.1 // indirect
	github.com/moby/term v0.0.0-20210619224110-3f7ff695adc6 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.0 // indirect
	github.com/oschwald/maxminddb-golang v1.13.0 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/exporter-toolkit v0.11.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20201227073835-cf1acfcdf475 // indirect
	github.com/rs/xid v1.6.0 // indirect
	github.com/sean-/seed v0.0.0-20170313163322-e2103e2c3529 // indirect
	github.com/sercand/kuberesolver/v5 v5.1.1 // indirect
	github.com/shopspring/decimal v1.2.0 // indirect
	github.com/sirupsen/logrus v1.9.3
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/cast v1.3.1 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/uber/jaeger-lib v2.4.1+incompatible // indirect
	github.com/ugorji/go/codec v1.1.7 // indirect
	github.com/willf/bitset v1.1.11 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/yuin/gopher-lua v1.1.0 // indirect
	go.etcd.io/etcd/api/v3 v3.5.4 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.5.4 // indirect
	go.etcd.io/etcd/client/v3 v3.5.4 // indirect
	go.mongodb.org/mongo-driver v1.14.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/collector/semconv v0.105.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.53.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.53.0 // indirect
	go.opentelemetry.io/otel v1.28.0
	go.opentelemetry.io/otel/metric v1.28.0 // indirect
	go.opentelemetry.io/otel/trace v1.28.0
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.21.0 // indirect
	golang.org/x/mod v0.19.0 // indirect
	golang.org/x/term v0.23.0 // indirect
	golang.org/x/tools v0.23.0 // indirect
	google.golang.org/genproto v0.0.0-20240820151423-278611b39280 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240820151423-278611b39280 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240820151423-278611b39280 // indirect
	gopkg.in/fsnotify/fsnotify.v1 v1.4.7 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gotest.tools/v3 v3.4.0 // indirect
	k8s.io/api v0.29.3 // indirect
	k8s.io/client-go v0.29.3 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.0.0-20240228011516-70dd3763d340 // indirect
	rsc.io/binaryregexp v0.2.0 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)

replace github.com/Azure/azure-sdk-for-go => github.com/Azure/azure-sdk-for-go v36.2.0+incompatible

replace github.com/Azure/azure-storage-blob-go => github.com/MasslessParticle/azure-storage-blob-go v0.14.1-0.20240322194317-344980fda573

replace github.com/hashicorp/consul => github.com/hashicorp/consul v1.14.5

// Use fork of gocql that has gokit logs and Prometheus metrics.
replace github.com/gocql/gocql => github.com/grafana/gocql v0.0.0-20200605141915-ba5dc39ece85

exclude k8s.io/client-go v8.0.0+incompatible

// Replace memberlist with our fork which includes some fixes that haven't been
// merged upstream yet.
replace github.com/hashicorp/memberlist => github.com/grafana/memberlist v0.3.1-0.20220714140823-09ffed8adbbe

// Insist on the optimised version of grafana/regexp
replace github.com/grafana/regexp => github.com/grafana/regexp v0.0.0-20240518133315-a468a5bfb3bc

replace github.com/grafana/loki/pkg/push => ./pkg/push

// leodido fork his project to continue support
replace github.com/influxdata/go-syslog/v3 => github.com/leodido/go-syslog/v4 v4.1.0
