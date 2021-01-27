module github.com/influxdata/telegraf

go 1.15

require (
	cloud.google.com/go v0.53.0
	cloud.google.com/go/datastore v1.1.0 // indirect
	cloud.google.com/go/pubsub v1.2.0
	code.cloudfoundry.org/clock v1.0.0 // indirect
	collectd.org v0.3.0
	github.com/Azure/azure-event-hubs-go/v3 v3.2.0
	github.com/Azure/azure-storage-queue-go v0.0.0-20181215014128-6ed74e755687
	github.com/Azure/go-ansiterm v0.0.0-20170929234023-d6e3b3328b78 // indirect
	github.com/Azure/go-autorest/autorest v0.9.3
	github.com/Azure/go-autorest/autorest/azure/auth v0.4.2
	github.com/BurntSushi/toml v0.3.1
	github.com/Mellanox/rdmamap v0.0.0-20191106181932-7c3c4763a6ee
	github.com/Microsoft/ApplicationInsights-Go v0.4.2
	github.com/Microsoft/go-winio v0.4.9 // indirect
	github.com/Shopify/sarama v1.27.1
	github.com/StackExchange/wmi v0.0.0-20180116203802-5d049714c4a6 // indirect
	github.com/aerospike/aerospike-client-go v1.27.0
	github.com/alecthomas/units v0.0.0-20190717042225-c3de453c63f4
	github.com/amir/raidman v0.0.0-20170415203553-1ccc43bfb9c9
	github.com/apache/thrift v0.12.0
	github.com/aristanetworks/glog v0.0.0-20191112221043-67e8567f59f3 // indirect
	github.com/aristanetworks/goarista v0.0.0-20190325233358-a123909ec740
	github.com/armon/go-metrics v0.3.0 // indirect
	github.com/aws/aws-sdk-go v1.34.34
	github.com/benbjohnson/clock v1.0.3
	github.com/bitly/go-hostpool v0.1.0 // indirect
	github.com/bmizerany/assert v0.0.0-20160611221934-b7ed37b82869
	github.com/caio/go-tdigest v2.3.0+incompatible // indirect
	github.com/cenkalti/backoff v2.0.0+incompatible // indirect
	github.com/cisco-ie/nx-telemetry-proto v0.0.0-20190531143454-82441e232cf6
	github.com/cockroachdb/apd v1.1.0 // indirect
	github.com/containerd/containerd v1.4.1 // indirect
	github.com/couchbase/go-couchbase v0.0.0-20180501122049-16db1f1fe037
	github.com/couchbase/gomemcached v0.0.0-20180502221210-0da75df14530 // indirect
	github.com/couchbase/goutils v0.0.0-20180530154633-e865a1461c8a // indirect
	github.com/denisenkom/go-mssqldb v0.0.0-20190707035753-2be1aa521ff4
	github.com/dgrijalva/jwt-go/v4 v4.0.0-preview1
	github.com/dimchansky/utfbom v1.1.0
	github.com/docker/distribution v2.6.0-rc.1.0.20170726174610-edc3ab29cdff+incompatible // indirect
	github.com/docker/docker v17.12.0-ce-rc1.0.20200916142827-bd33bbf0497b+incompatible
	github.com/docker/go-connections v0.3.0 // indirect
	github.com/docker/go-units v0.3.3 // indirect
	github.com/docker/libnetwork v0.8.0-dev.2.0.20181012153825-d7b61745d166
	github.com/eclipse/paho.mqtt.golang v1.2.0
	github.com/ericchiang/k8s v1.2.0
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32
	github.com/glinton/ping v0.1.4-0.20200311211934-5ac87da8cd96
	github.com/go-logfmt/logfmt v0.4.0
	github.com/go-ole/go-ole v1.2.1 // indirect
	github.com/go-redis/redis v6.15.9+incompatible
	github.com/go-sql-driver/mysql v1.5.0
	github.com/goburrow/modbus v0.1.0
	github.com/goburrow/serial v0.1.0 // indirect
	github.com/gobwas/glob v0.2.3
	github.com/gofrs/uuid v2.1.0+incompatible
	github.com/gogo/protobuf v1.2.2-0.20190723190241-65acae22fc9d
	github.com/golang/geo v0.0.0-20190916061304-5b978397cfec
	github.com/golang/protobuf v1.3.5
	github.com/google/go-cmp v0.5.2
	github.com/google/go-github/v32 v32.1.0
	github.com/gopcua/opcua v0.1.12
	github.com/gorilla/mux v1.6.2
	github.com/hailocab/go-hostpool v0.0.0-20160125115350-e80d13ce29ed // indirect
	github.com/harlow/kinesis-consumer v0.3.1-0.20181230152818-2f58b136fee0
	github.com/hashicorp/consul v1.2.1
	github.com/hashicorp/go-msgpack v0.5.5 // indirect
	github.com/hashicorp/go-rootcerts v0.0.0-20160503143440-6bb64b370b90 // indirect
	github.com/hashicorp/memberlist v0.1.5 // indirect
	github.com/hashicorp/serf v0.8.1 // indirect
	github.com/influxdata/go-syslog/v2 v2.0.1
	github.com/influxdata/tail v1.0.1-0.20200707181643-03a791b270e4
	github.com/influxdata/toml v0.0.0-20190415235208-270119a8ce65
	github.com/influxdata/wlog v0.0.0-20160411224016-7c63b0a71ef8
	github.com/jackc/fake v0.0.0-20150926172116-812a484cc733 // indirect
	github.com/jackc/pgx v3.6.0+incompatible
	github.com/kardianos/service v1.0.0
	github.com/karrick/godirwalk v1.16.1
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51
	github.com/kubernetes/apimachinery v0.0.0-20190119020841-d41becfba9ee
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/leesper/go_rng v0.0.0-20190531154944-a612b043e353 // indirect
	github.com/lib/pq v1.3.0 // indirect
	github.com/mailru/easyjson v0.0.0-20180717111219-efc7eb8984d6 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.1
	github.com/mdlayher/apcupsd v0.0.0-20200608131503-2bf01da7bf1b
	github.com/miekg/dns v1.0.14
	github.com/mitchellh/go-testing-interface v1.0.0 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/multiplay/go-ts3 v1.0.0
	github.com/naoina/go-stringutil v0.1.0 // indirect
	github.com/nats-io/nats-server/v2 v2.1.4
	github.com/nats-io/nats.go v1.9.1
	github.com/newrelic/newrelic-telemetry-sdk-go v0.2.0
	github.com/nsqio/go-nsq v1.0.7
	github.com/openconfig/gnmi v0.0.0-20180912164834-33a1865c3029
	github.com/opencontainers/go-digest v1.0.0-rc1 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/opentracing-contrib/go-observer v0.0.0-20170622124052-a52f23424492 // indirect
	github.com/opentracing/opentracing-go v1.0.2 // indirect
	github.com/openzipkin/zipkin-go-opentracing v0.3.4
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.5.1
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.9.1
	github.com/prometheus/procfs v0.0.8
	github.com/safchain/ethtool v0.0.0-20200218184317-f459e2d13664
	github.com/samuel/go-zookeeper v0.0.0-20180130194729-c4fab1ac1bec // indirect
	github.com/satori/go.uuid v1.2.1-0.20181028125025-b2ce2384e17b // indirect
	github.com/shirou/gopsutil v2.20.9+incompatible
	github.com/shopspring/decimal v0.0.0-20200105231215-408a2507e114 // indirect
	github.com/sirupsen/logrus v1.4.2
	github.com/soniah/gosnmp v1.25.0
	github.com/streadway/amqp v0.0.0-20180528204448-e5adc2ada8b8
	github.com/stretchr/testify v1.6.1
	github.com/tbrandon/mbserver v0.0.0-20170611213546-993e1772cc62
	github.com/tedsuo/ifrit v0.0.0-20191009134036-9a97d0632f00 // indirect
	github.com/tidwall/gjson v1.6.0
	github.com/vishvananda/netlink v0.0.0-20171020171820-b2de5d10e38e // indirect
	github.com/vishvananda/netns v0.0.0-20180720170159-13995c7128cc // indirect
	github.com/vjeantet/grok v1.0.0
	github.com/vmware/govmomi v0.19.0
	github.com/wavefronthq/wavefront-sdk-go v0.9.2
	github.com/wvanbergen/kafka v0.0.0-20171203153745-e2edea948ddf
	github.com/wvanbergen/kazoo-go v0.0.0-20180202103751-f72d8611297a // indirect
	github.com/xdg/scram v0.0.0-20180814205039-7eeb5667e42c
	github.com/yuin/gopher-lua v0.0.0-20180630135845-46796da1b0b4 // indirect
	go.starlark.net v0.0.0-20200901195727-6e684ef5eeee
	golang.org/x/exp v0.0.0-20200224162631-6cc2880d07d6 // indirect
	golang.org/x/lint v0.0.0-20200302205851-738671d3881b // indirect
	golang.org/x/net v0.0.0-20200904194848-62affa334b73
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	golang.org/x/sync v0.0.0-20200317015054-43a5402ce75a
	golang.org/x/sys v0.0.0-20200826173525-f9321e4c35a6
	golang.org/x/text v0.3.3
	golang.org/x/tools v0.0.0-20200317043434-63da46f3035e // indirect
	golang.zx2c4.com/wireguard/wgctrl v0.0.0-20200205215550-e35592f146e4
	gonum.org/v1/gonum v0.6.2 // indirect
	google.golang.org/api v0.20.0
	google.golang.org/genproto v0.0.0-20200317114155-1f3552e48f24
	google.golang.org/grpc v1.28.0
	gopkg.in/fatih/pool.v2 v2.0.0 // indirect
	gopkg.in/gorethink/gorethink.v3 v3.0.5
	gopkg.in/ldap.v3 v3.1.0
	gopkg.in/mgo.v2 v2.0.0-20180705113604-9856a29383ce
	gopkg.in/olivere/elastic.v5 v5.0.70
	gopkg.in/yaml.v2 v2.2.8
	gotest.tools v2.2.0+incompatible // indirect
	honnef.co/go/tools v0.0.1-2020.1.3 // indirect
	k8s.io/apimachinery v0.17.1 // indirect
	modernc.org/sqlite v1.7.4
)

// replaced due to https://github.com/satori/go.uuid/issues/73
replace github.com/satori/go.uuid => github.com/gofrs/uuid v3.2.0+incompatible
