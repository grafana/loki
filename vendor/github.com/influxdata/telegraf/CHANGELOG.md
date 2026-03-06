<!-- markdownlint-disable MD013 MD024 -->
# Changelog

## v1.37.3 [2026-02-23]

### Bugfixes

- [#18195](https://github.com/influxdata/telegraf/pull/18195) `common.jolokia2` Add Jolokia 2.x compatibility for proxy target tag
- [#18378](https://github.com/influxdata/telegraf/pull/18378) `common.opcua` Include node ID in duplicate metric check
- [#18335](https://github.com/influxdata/telegraf/pull/18335) `inputs.disk` Preserve device tag for virtual filesystems
- [#18374](https://github.com/influxdata/telegraf/pull/18374) `inputs.docker` Remove pre-filtering of states
- [#18383](https://github.com/influxdata/telegraf/pull/18383) `inputs.docker_log` Remove pre-filtering of states
- [#18347](https://github.com/influxdata/telegraf/pull/18347) `inputs.jenkins` Report all concurrent builds
- [#18377](https://github.com/influxdata/telegraf/pull/18377) `inputs.prometheus` Add thread safety and proper cleanup for shared informer factories
- [#18304](https://github.com/influxdata/telegraf/pull/18304) `inputs.prometheus` Cleanup shared informers on stop
- [#18367](https://github.com/influxdata/telegraf/pull/18367) `inputs.upsd` Stop silently dropping mandatory variables from additional_fields
- [#18386](https://github.com/influxdata/telegraf/pull/18386) `serializers.template` Unwrap tracking metrics

### Dependency Updates

- [#18354](https://github.com/influxdata/telegraf/pull/18354) `deps` Bump cloud.google.com/go/auth from 0.18.1 to 0.18.2
- [#18324](https://github.com/influxdata/telegraf/pull/18324) `deps` Bump cloud.google.com/go/bigquery from 1.72.0 to 1.73.1
- [#18319](https://github.com/influxdata/telegraf/pull/18319) `deps` Bump cloud.google.com/go/pubsub/v2 from 2.3.0 to 2.4.0
- [#18298](https://github.com/influxdata/telegraf/pull/18298) `deps` Bump cloud.google.com/go/storage from 1.59.1 to 1.59.2
- [#18361](https://github.com/influxdata/telegraf/pull/18361) `deps` Bump cloud.google.com/go/storage from 1.59.2 to 1.60.0
- [#18376](https://github.com/influxdata/telegraf/pull/18376) `deps` Bump filippo.io/edwards25519 from 1.1.0 to 1.1.1
- [#18292](https://github.com/influxdata/telegraf/pull/18292) `deps` Bump github.com/ClickHouse/clickhouse-go/v2 from 2.42.0 to 2.43.0
- [#18295](https://github.com/influxdata/telegraf/pull/18295) `deps` Bump github.com/IBM/nzgo/v12 from 12.0.10 to 12.0.11
- [#18297](https://github.com/influxdata/telegraf/pull/18297) `deps` Bump github.com/SAP/go-hdb from 1.14.18 to 1.14.19
- [#18328](https://github.com/influxdata/telegraf/pull/18328) `deps` Bump github.com/SAP/go-hdb from 1.14.19 to 1.14.22
- [#18364](https://github.com/influxdata/telegraf/pull/18364) `deps` Bump github.com/SAP/go-hdb from 1.14.22 to 1.15.0
- [#18358](https://github.com/influxdata/telegraf/pull/18358) `deps` Bump github.com/alitto/pond/v2 from 2.6.0 to 2.6.2
- [#18289](https://github.com/influxdata/telegraf/pull/18289) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.282.0 to 1.285.0
- [#18362](https://github.com/influxdata/telegraf/pull/18362) `deps` Bump github.com/coocood/freecache from 1.2.4 to 1.2.5
- [#18299](https://github.com/influxdata/telegraf/pull/18299) `deps` Bump github.com/coreos/go-systemd/v22 from 22.6.0 to 22.7.0
- [#18294](https://github.com/influxdata/telegraf/pull/18294) `deps` Bump github.com/golang-jwt/jwt/v5 from 5.3.0 to 5.3.1
- [#18291](https://github.com/influxdata/telegraf/pull/18291) `deps` Bump github.com/google/cel-go from 0.26.1 to 0.27.0
- [#18330](https://github.com/influxdata/telegraf/pull/18330) `deps` Bump github.com/klauspost/compress from 1.18.3 to 1.18.4
- [#18268](https://github.com/influxdata/telegraf/pull/18268) `deps` Bump github.com/lxc/incus/v6 from 6.20.0 to 6.21.0
- [#18296](https://github.com/influxdata/telegraf/pull/18296) `deps` Bump github.com/nats-io/nats-server/v2 from 2.12.3 to 2.12.4
- [#18356](https://github.com/influxdata/telegraf/pull/18356) `deps` Bump github.com/p4lang/p4runtime from 1.4.1 to 1.5.0
- [#18326](https://github.com/influxdata/telegraf/pull/18326) `deps` Bump github.com/prometheus-community/pro-bing from 0.7.0 to 0.8.0
- [#18355](https://github.com/influxdata/telegraf/pull/18355) `deps` Bump github.com/redis/go-redis/v9 from 9.17.3 to 9.18.0
- [#18293](https://github.com/influxdata/telegraf/pull/18293) `deps` Bump github.com/shirou/gopsutil/v4 from 4.25.11 to 4.26.1
- [#18331](https://github.com/influxdata/telegraf/pull/18331) `deps` Bump github.com/snowflakedb/gosnowflake from 1.18.1 to 1.19.0
- [#18323](https://github.com/influxdata/telegraf/pull/18323) `deps` Bump github.com/vertica/vertica-sql-go from 1.3.4 to 1.3.5
- [#18290](https://github.com/influxdata/telegraf/pull/18290) `deps` Bump go.mongodb.org/mongo-driver from 1.17.7 to 1.17.8
- [#18332](https://github.com/influxdata/telegraf/pull/18332) `deps` Bump go.mongodb.org/mongo-driver from 1.17.8 to 1.17.9
- [#18318](https://github.com/influxdata/telegraf/pull/18318) `deps` Bump golang.org/x/mod from 0.32.0 to 0.33.0
- [#18322](https://github.com/influxdata/telegraf/pull/18322) `deps` Bump golang.org/x/net from 0.49.0 to 0.50.0
- [#18333](https://github.com/influxdata/telegraf/pull/18333) `deps` Bump golang.org/x/term from 0.39.0 to 0.40.0
- [#18329](https://github.com/influxdata/telegraf/pull/18329) `deps` Bump golang.org/x/text from 0.33.0 to 0.34.0
- [#18300](https://github.com/influxdata/telegraf/pull/18300) `deps` Bump google.golang.org/api from 0.262.0 to 0.264.0
- [#18317](https://github.com/influxdata/telegraf/pull/18317) `deps` Bump google.golang.org/api from 0.264.0 to 0.265.0
- [#18357](https://github.com/influxdata/telegraf/pull/18357) `deps` Bump google.golang.org/grpc from 1.78.0 to 1.79.1
- [#18363](https://github.com/influxdata/telegraf/pull/18363) `deps` Bump k8s.io/client-go from 0.35.0 to 0.35.1
- [#18327](https://github.com/influxdata/telegraf/pull/18327) `deps` Bump modernc.org/sqlite from 1.44.3 to 1.45.0
- [#18288](https://github.com/influxdata/telegraf/pull/18288) `deps` Bump super-linter/super-linter from 8.3.2 to 8.4.0
- [#18315](https://github.com/influxdata/telegraf/pull/18315) `deps` Bump super-linter/super-linter from 8.4.0 to 8.5.0
- [#18353](https://github.com/influxdata/telegraf/pull/18353) `deps` Bump the aws-sdk-go-v2 group with 2 updates
- [#18316](https://github.com/influxdata/telegraf/pull/18316) `deps` Bump the aws-sdk-go-v2 group with 2 updates
- [#18314](https://github.com/influxdata/telegraf/pull/18314) `deps` Bump tj-actions/changed-files from 47.0.1 to 47.0.2
- [#18372](https://github.com/influxdata/telegraf/pull/18372) `deps` Update github.com/pion/dtls from v2 to v3

## v1.37.2 [2026-02-02]

### Bugfixes

- [#18254](https://github.com/influxdata/telegraf/pull/18254) `inputs.cisco_telemetry_mdt` Handle DME events correctly
- [#18177](https://github.com/influxdata/telegraf/pull/18177) `inputs.nftables` Handle named counter references in JSON output
- [#18233](https://github.com/influxdata/telegraf/pull/18233) `inputs.procstat` Handle newer versions of systemd correctly
- [#18225](https://github.com/influxdata/telegraf/pull/18225) `inputs.statsd` Handle negative lengths
- [#18278](https://github.com/influxdata/telegraf/pull/18278) `parsers.dropwizard` Correct sample config setting name for tag path

### Dependency Updates

- [#18204](https://github.com/influxdata/telegraf/pull/18204) `deps` Bump aws-sdk-go-v2 group with 11 updates
- [#18260](https://github.com/influxdata/telegraf/pull/18260) `deps` Bump aws-sdk-go-v2 group with 2 updates
- [#18265](https://github.com/influxdata/telegraf/pull/18265) `deps` Bump cloud.google.com/go/auth from 0.18.0 to 0.18.1
- [#18212](https://github.com/influxdata/telegraf/pull/18212) `deps` Bump cloud.google.com/go/storage from 1.58.0 to 1.59.0
- [#18243](https://github.com/influxdata/telegraf/pull/18243) `deps` Bump cloud.google.com/go/storage from 1.59.0 to 1.59.1
- [#18237](https://github.com/influxdata/telegraf/pull/18237) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azcore from 1.20.0 to 1.21.0
- [#18216](https://github.com/influxdata/telegraf/pull/18216) `deps` Bump github.com/SAP/go-hdb from 1.14.16 to 1.14.17
- [#18236](https://github.com/influxdata/telegraf/pull/18236) `deps` Bump github.com/SAP/go-hdb from 1.14.17 to 1.14.18
- [#18270](https://github.com/influxdata/telegraf/pull/18270) `deps` Bump github.com/apache/arrow-go/v18 from 18.5.0 to 18.5.1
- [#18235](https://github.com/influxdata/telegraf/pull/18235) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.279.1 to 1.279.2
- [#18206](https://github.com/influxdata/telegraf/pull/18206) `deps` Bump github.com/gosnmp/gosnmp from 1.43.1 to 1.43.2
- [#18240](https://github.com/influxdata/telegraf/pull/18240) `deps` Bump github.com/hashicorp/consul/api from 1.33.0 to 1.33.2
- [#18242](https://github.com/influxdata/telegraf/pull/18242) `deps` Bump github.com/klauspost/compress from 1.18.2 to 1.18.3
- [#18266](https://github.com/influxdata/telegraf/pull/18266) `deps` Bump github.com/linkedin/goavro/v2 from 2.14.1 to 2.15.0
- [#18239](https://github.com/influxdata/telegraf/pull/18239) `deps` Bump github.com/microsoft/go-mssqldb from 1.9.5 to 1.9.6
- [#18210](https://github.com/influxdata/telegraf/pull/18210) `deps` Bump github.com/miekg/dns from 1.1.69 to 1.1.70
- [#18264](https://github.com/influxdata/telegraf/pull/18264) `deps` Bump github.com/miekg/dns from 1.1.70 to 1.1.72
- [#18271](https://github.com/influxdata/telegraf/pull/18271) `deps` Bump github.com/redis/go-redis/v9 from 9.17.2 to 9.17.3
- [#18244](https://github.com/influxdata/telegraf/pull/18244) `deps` Bump github.com/sirupsen/logrus from 1.9.3 to 1.9.4
- [#18262](https://github.com/influxdata/telegraf/pull/18262) `deps` Bump github.com/tdrn-org/go-tr064 from 0.2.2 to 0.2.3
- [#18267](https://github.com/influxdata/telegraf/pull/18267) `deps` Bump go.mongodb.org/mongo-driver from 1.17.6 to 1.17.7
- [#18269](https://github.com/influxdata/telegraf/pull/18269) `deps` Bump go.step.sm/crypto from 0.75.0 to 0.76.0
- [#18215](https://github.com/influxdata/telegraf/pull/18215) `deps` Bump golang.org/x/crypto from 0.46.0 to 0.47.0
- [#18208](https://github.com/influxdata/telegraf/pull/18208) `deps` Bump golang.org/x/mod from 0.31.0 to 0.32.0
- [#18207](https://github.com/influxdata/telegraf/pull/18207) `deps` Bump golang.org/x/net from 0.48.0 to 0.49.0
- [#18217](https://github.com/influxdata/telegraf/pull/18217) `deps` Bump gonum.org/v1/gonum from 0.16.0 to 0.17.0
- [#18261](https://github.com/influxdata/telegraf/pull/18261) `deps` Bump google.golang.org/api from 0.257.0 to 0.262.0
- [#18213](https://github.com/influxdata/telegraf/pull/18213) `deps` Bump modernc.org/sqlite from 1.42.2 to 1.43.0
- [#18241](https://github.com/influxdata/telegraf/pull/18241) `deps` Bump modernc.org/sqlite from 1.43.0 to 1.44.2
- [#18263](https://github.com/influxdata/telegraf/pull/18263) `deps` Bump modernc.org/sqlite from 1.44.2 to 1.44.3

## v1.37.1 [2026-01-12]

### Bugfixes

- [#18138](https://github.com/influxdata/telegraf/pull/18138) `config` Add missing validation for labels in plugins
- [#18108](https://github.com/influxdata/telegraf/pull/18108) `config` Make labels and selectors conform to specification
- [#18144](https://github.com/influxdata/telegraf/pull/18144) `inputs.procstat` Isolate process cache per filter to fix tag collision
- [#18191](https://github.com/influxdata/telegraf/pull/18191) `outputs.sql` Populate column cache for existing tables

### Dependency Updates

- [#18125](https://github.com/influxdata/telegraf/pull/18125) `deps` Bump cloud.google.com/go/auth from 0.17.0 to 0.18.0
- [#18140](https://github.com/influxdata/telegraf/pull/18140) `deps` Bump cloud.google.com/go/auth from 0.17.0 to 0.18.0
- [#18094](https://github.com/influxdata/telegraf/pull/18094) `deps` Bump cloud.google.com/go/storage from 1.57.2 to 1.58.0
- [#18157](https://github.com/influxdata/telegraf/pull/18157) `deps` Bump github.com/BurntSushi/toml from 1.5.0 to 1.6.0
- [#18124](https://github.com/influxdata/telegraf/pull/18124) `deps` Bump github.com/ClickHouse/clickhouse-go/v2 from 2.41.0 to 2.42.0
- [#18101](https://github.com/influxdata/telegraf/pull/18101) `deps` Bump github.com/SAP/go-hdb from 1.14.13 to 1.14.14
- [#18153](https://github.com/influxdata/telegraf/pull/18153) `deps` Bump github.com/SAP/go-hdb from 1.14.14 to 1.14.15
- [#18199](https://github.com/influxdata/telegraf/pull/18199) `deps` Bump github.com/SAP/go-hdb from 1.14.15 to 1.14.16
- [#18123](https://github.com/influxdata/telegraf/pull/18123) `deps` Bump github.com/apache/arrow-go/v18 from 18.4.1 to 18.5.0
- [#18200](https://github.com/influxdata/telegraf/pull/18200) `deps` Bump github.com/apache/inlong/inlong-sdk/dataproxy-sdk-twins/dataproxy-sdk-golang from 1.0.6 to 1.0.7
- [#18197](https://github.com/influxdata/telegraf/pull/18197) `deps` Bump github.com/gophercloud/gophercloud/v2 from 2.9.0 to 2.10.0
- [#18198](https://github.com/influxdata/telegraf/pull/18198) `deps` Bump github.com/gosnmp/gosnmp from 1.42.1 to 1.43.1
- [#18132](https://github.com/influxdata/telegraf/pull/18132) `deps` Bump github.com/jedib0t/go-pretty/v6 from 6.7.5 to 6.7.7
- [#18169](https://github.com/influxdata/telegraf/pull/18169) `deps` Bump github.com/jedib0t/go-pretty/v6 from 6.7.7 to 6.7.8
- [#18189](https://github.com/influxdata/telegraf/pull/18189) `deps` Bump github.com/likexian/whois from 1.15.6 to 1.15.7
- [#18187](https://github.com/influxdata/telegraf/pull/18187) `deps` Bump github.com/likexian/whois-parser from 1.24.20 to 1.24.21
- [#18150](https://github.com/influxdata/telegraf/pull/18150) `deps` Bump github.com/lxc/incus/v6 from 6.19.1 to 6.20.0
- [#18130](https://github.com/influxdata/telegraf/pull/18130) `deps` Bump github.com/miekg/dns from 1.1.68 to 1.1.69
- [#18149](https://github.com/influxdata/telegraf/pull/18149) `deps` Bump github.com/nats-io/nats-server/v2 from 2.12.2 to 2.12.3
- [#18147](https://github.com/influxdata/telegraf/pull/18147) `deps` Bump github.com/nats-io/nats.go from 1.47.0 to 1.48.0
- [#18172](https://github.com/influxdata/telegraf/pull/18172) `deps` Bump github.com/netsampler/goflow2/v2 from 2.2.3 to 2.2.6
- [#18190](https://github.com/influxdata/telegraf/pull/18190) `deps` Bump github.com/prometheus/common from 0.67.4 to 0.67.5
- [#18102](https://github.com/influxdata/telegraf/pull/18102) `deps` Bump github.com/prometheus/prometheus from 0.307.3 to 0.308.0
- [#18155](https://github.com/influxdata/telegraf/pull/18155) `deps` Bump github.com/prometheus/prometheus from 0.308.0 to 0.308.1
- [#18129](https://github.com/influxdata/telegraf/pull/18129) `deps` Bump github.com/snowflakedb/gosnowflake from 1.18.0 to 1.18.1
- [#18103](https://github.com/influxdata/telegraf/pull/18103) `deps` Bump github.com/tinylib/msgp from 1.5.0 to 1.6.1
- [#18188](https://github.com/influxdata/telegraf/pull/18188) `deps` Bump github.com/tinylib/msgp from 1.6.1 to 1.6.3
- [#18186](https://github.com/influxdata/telegraf/pull/18186) `deps` Bump github.com/yuin/goldmark from 1.7.13 to 1.7.15
- [#18201](https://github.com/influxdata/telegraf/pull/18201) `deps` Bump github.com/yuin/goldmark from 1.7.15 to 1.7.16
- [#18092](https://github.com/influxdata/telegraf/pull/18092) `deps` Bump go.step.sm/crypto from 0.74.0 to 0.75.0
- [#18098](https://github.com/influxdata/telegraf/pull/18098) `deps` Bump golang.org/x/crypto from 0.45.0 to 0.46.0
- [#18100](https://github.com/influxdata/telegraf/pull/18100) `deps` Bump golang.org/x/mod from 0.30.0 to 0.31.0
- [#18127](https://github.com/influxdata/telegraf/pull/18127) `deps` Bump golang.org/x/net from 0.47.0 to 0.48.0
- [#18095](https://github.com/influxdata/telegraf/pull/18095) `deps` Bump golang.org/x/oauth2 from 0.33.0 to 0.34.0
- [#18093](https://github.com/influxdata/telegraf/pull/18093) `deps` Bump golang.org/x/sync from 0.18.0 to 0.19.0
- [#18096](https://github.com/influxdata/telegraf/pull/18096) `deps` Bump golang.org/x/sys from 0.38.0 to 0.39.0
- [#18099](https://github.com/influxdata/telegraf/pull/18099) `deps` Bump golang.org/x/term from 0.37.0 to 0.38.0
- [#18097](https://github.com/influxdata/telegraf/pull/18097) `deps` Bump golang.org/x/text from 0.31.0 to 0.32.0
- [#18104](https://github.com/influxdata/telegraf/pull/18104) `deps` Bump google.golang.org/api from 0.256.0 to 0.257.0
- [#18173](https://github.com/influxdata/telegraf/pull/18173) `deps` Bump google.golang.org/grpc from 1.77.0 to 1.78.0
- [#18131](https://github.com/influxdata/telegraf/pull/18131) `deps` Bump google.golang.org/protobuf from 1.36.10 to 1.36.11
- [#18128](https://github.com/influxdata/telegraf/pull/18128) `deps` Bump k8s.io/api from 0.34.2 to 0.34.3
- [#18148](https://github.com/influxdata/telegraf/pull/18148) `deps` Bump k8s.io/apimachinery from 0.34.3 to 0.35.0
- [#18126](https://github.com/influxdata/telegraf/pull/18126) `deps` Bump k8s.io/client-go from 0.34.2 to 0.34.3
- [#18154](https://github.com/influxdata/telegraf/pull/18154) `deps` Bump k8s.io/client-go from 0.34.3 to 0.35.0
- [#18152](https://github.com/influxdata/telegraf/pull/18152) `deps` Bump modernc.org/sqlite from 1.40.1 to 1.41.0
- [#18171](https://github.com/influxdata/telegraf/pull/18171) `deps` Bump modernc.org/sqlite from 1.41.0 to 1.42.2
- [#18170](https://github.com/influxdata/telegraf/pull/18170) `deps` Bump software.sslmate.com/src/go-pkcs12 from 0.6.0 to 0.7.0
- [#18158](https://github.com/influxdata/telegraf/pull/18158) `deps` Bump super-linter/super-linter from 8.3.0 to 8.3.1
- [#18174](https://github.com/influxdata/telegraf/pull/18174) `deps` Bump super-linter/super-linter from 8.3.1 to 8.3.2
- [#18091](https://github.com/influxdata/telegraf/pull/18091) `deps` Bump the aws-sdk-go-v2 group with 11 updates
- [#18146](https://github.com/influxdata/telegraf/pull/18146) `deps` Bump the aws-sdk-go-v2 group with 3 updates
- [#18121](https://github.com/influxdata/telegraf/pull/18121) `deps` Bump the aws-sdk-go-v2 group with 8 updates
- [#18120](https://github.com/influxdata/telegraf/pull/18120) `deps` Bump tj-actions/changed-files from 47.0.0 to 47.0.1
- [#18115](https://github.com/influxdata/telegraf/pull/18115) `deps` Update golangci-lint to 2.7.2

## v1.37.0 [2025-12-08]

### Important Changes

- PR [#17966](https://github.com/influxdata/telegraf/pull/17966) introduced the strict handling of environment variables
  to prevent security issues. However, strict handling prevents using environment variables for non-string settings as
  the configuration before replacing the variables must be TOML conform. To provide security-by-default, we will change
  the **default behavior of Telegraf to the strict environment variable handling with v1.38.0**!
  Please make sure your configuration works in the now conditions by using the `--strict-env-handling` flag! If your
  configuration works in strict mode or you are not using environment variables, **do not** add the flag as it will be
  removed later and ignore the new warning at startup. In case you need the current behavior please add
  `--non-strict-env-handling` when starting Telegraf to prepare for the upcoming change!

### New Plugins

- [#17993](https://github.com/influxdata/telegraf/pull/17993) `inputs.logql` Add plugin
- [#17604](https://github.com/influxdata/telegraf/pull/17604) `inputs.nftables` Add plugin
- [#17701](https://github.com/influxdata/telegraf/pull/17701) `inputs.promql` Add plugin
- [#17831](https://github.com/influxdata/telegraf/pull/17831) `inputs.timex` Add plugin
- [#17875](https://github.com/influxdata/telegraf/pull/17875) `outputs.arc` Add plugin
- [#17998](https://github.com/influxdata/telegraf/pull/17998) `outputs.heartbeat` Add plugin
- [#17921](https://github.com/influxdata/telegraf/pull/17921) `secretstores.googlecloud` Add plugin
- [#17844](https://github.com/influxdata/telegraf/pull/17844) `secretstores.vault` Add plugin

### Features

- [#18084](https://github.com/influxdata/telegraf/pull/18084) `config` Allow specifying env-handling mode for config check
- [#17753](https://github.com/influxdata/telegraf/pull/17753) `config` Remove deprecated options
- [#17915](https://github.com/influxdata/telegraf/pull/17915) `config` Store loaded sources
- [#17080](https://github.com/influxdata/telegraf/pull/17080) `internal` Add support for parsing a timestamp in a TimeZone
- [#17916](https://github.com/influxdata/telegraf/pull/17916) `logging` Allow registering callbacks for logging events
- [#17749](https://github.com/influxdata/telegraf/pull/17749) `models` Implement collection of plugin-internal statistics for all types
- [#18044](https://github.com/influxdata/telegraf/pull/18044) `common.socket` Add option to specify source IP restrictions
- [#17760](https://github.com/influxdata/telegraf/pull/17760) `inputs.aerospike` Remove deprecated options
- [#17759](https://github.com/influxdata/telegraf/pull/17759) `inputs.cpu` Add number of physical CPUs
- [#17761](https://github.com/influxdata/telegraf/pull/17761) `inputs.gnmi` Remove deprecated options
- [#17732](https://github.com/influxdata/telegraf/pull/17732) `inputs.influxdb_v2_listener` Implement ping endpoint
- [#17733](https://github.com/influxdata/telegraf/pull/17733) `inputs.influxdb_v2_listener` Migrate to selfstat collector
- [#17965](https://github.com/influxdata/telegraf/pull/17965) `inputs.ldap` Support external SASL bind (#17477)
- [#17478](https://github.com/influxdata/telegraf/pull/17478) `inputs.ldap` Support ldapi protocol
- [#17743](https://github.com/influxdata/telegraf/pull/17743) `inputs.modbus` Remove deprecated plugin option values
- [#17762](https://github.com/influxdata/telegraf/pull/17762) `inputs.mongodb` Remove deprecated options
- [#17792](https://github.com/influxdata/telegraf/pull/17792) `inputs.nats_consumer` Acknowledge messages on delivery
- [#17710](https://github.com/influxdata/telegraf/pull/17710) `inputs.nats_consumer` Allow configuring Jetstream stream
- [#17742](https://github.com/influxdata/telegraf/pull/17742) `inputs.net` Remove deprecated plugin option value
- [#17624](https://github.com/influxdata/telegraf/pull/17624) `inputs.netflow` Add datatypes to PEN mapping
- [#17697](https://github.com/influxdata/telegraf/pull/17697) `inputs.netflow` Add support for float32 datatype
- [#17906](https://github.com/influxdata/telegraf/pull/17906) `inputs.opcua` Add namespace URI support
- [#17825](https://github.com/influxdata/telegraf/pull/17825) `inputs.opcua` Add remote certificate trust configuration
- [#17752](https://github.com/influxdata/telegraf/pull/17752) `inputs.opcua` Remove deprecated options
- [#17991](https://github.com/influxdata/telegraf/pull/17991) `inputs.opcua` Support persistent self-signed client certificates
- [#17633](https://github.com/influxdata/telegraf/pull/17633) `inputs.rabbitmq` Add type tag to queues
- [#18080](https://github.com/influxdata/telegraf/pull/18080) `inputs.s7comm` Add option idle_timeout
- [#17550](https://github.com/influxdata/telegraf/pull/17550) `inputs.smart` Parse vendor specific ratio values
- [#17948](https://github.com/influxdata/telegraf/pull/17948) `inputs.snmp` Add option to stop polling on first error
- [#17375](https://github.com/influxdata/telegraf/pull/17375) `inputs.sql` Add Vertica support
- [#17924](https://github.com/influxdata/telegraf/pull/17924) `inputs.sqlserver` Add support for LPC and named-pipe protocols
- [#17796](https://github.com/influxdata/telegraf/pull/17796) `inputs.sqlserver` Set pool size and idle connection
- [#17872](https://github.com/influxdata/telegraf/pull/17872) `inputs.statsd` Improve performance
- [#17763](https://github.com/influxdata/telegraf/pull/17763) `inputs.win_perf_counters` Remove deprecated options
- [#17751](https://github.com/influxdata/telegraf/pull/17751) `inputs.zookeeper` Remove deprecated option
- [#17950](https://github.com/influxdata/telegraf/pull/17950) `outputs.amon` Deprecate plugin
- [#18062](https://github.com/influxdata/telegraf/pull/18062) `outputs.heartbeat` Add configuration information
- [#18050](https://github.com/influxdata/telegraf/pull/18050) `outputs.heartbeat` Add optional statistics output
- [#17869](https://github.com/influxdata/telegraf/pull/17869) `outputs.mongodb` Add PLAIN authentication support and validation
- [#17755](https://github.com/influxdata/telegraf/pull/17755) `outputs.mqtt` Remove deprecated option
- [#18048](https://github.com/influxdata/telegraf/pull/18048) `outputs.nats` Add secret-support for credentials
- [#18007](https://github.com/influxdata/telegraf/pull/18007) `outputs.nats` Support nkey seed authentication
- [#17409](https://github.com/influxdata/telegraf/pull/17409) `outputs.remotefile` Add compression for remotefile plugin
- [#17764](https://github.com/influxdata/telegraf/pull/17764) `parsers.binary` Remove deprecated options
- [#17754](https://github.com/influxdata/telegraf/pull/17754) `parsers.xpath` Remove deprecated options
- [#17576](https://github.com/influxdata/telegraf/pull/17576) `processors.execd` Add log prefixing
- [#17741](https://github.com/influxdata/telegraf/pull/17741) `processors.template` Remove deprecated template syntax

### Bugfixes

- [#18064](https://github.com/influxdata/telegraf/pull/18064) `common.opcua` Skip file permission check on Windows
- [#18012](https://github.com/influxdata/telegraf/pull/18012) `inputs.docker_log` Remove hard-coded API version
- [#17960](https://github.com/influxdata/telegraf/pull/17960) `inputs.opcua` Add private key for certificate-based user authentication
- [#18036](https://github.com/influxdata/telegraf/pull/18036) `inputs.procstat` Make port conversion more robust
- [#18014](https://github.com/influxdata/telegraf/pull/18014) `outputs.influxdb_v2` Correct calculation of amount of batches for concurrent writes

### Dependency Updates

- [#18051](https://github.com/influxdata/telegraf/pull/18051) `deps` Bump actions/checkout from 5 to 6
- [#18021](https://github.com/influxdata/telegraf/pull/18021) `deps` Bump cloud.google.com/go/storage from 1.57.1 to 1.57.2
- [#18055](https://github.com/influxdata/telegraf/pull/18055) `deps` Bump github.com/ClickHouse/clickhouse-go/v2 from 2.40.3 to 2.41.0
- [#18019](https://github.com/influxdata/telegraf/pull/18019) `deps` Bump github.com/SAP/go-hdb from 1.14.12 to 1.14.13
- [#18076](https://github.com/influxdata/telegraf/pull/18076) `deps` Bump github.com/alitto/pond/v2 from 2.5.0 to 2.6.0
- [#18074](https://github.com/influxdata/telegraf/pull/18074) `deps` Bump github.com/aws/smithy-go from 1.23.2 to 1.24.0
- [#18020](https://github.com/influxdata/telegraf/pull/18020) `deps` Bump github.com/gophercloud/gophercloud/v2 from 2.8.0 to 2.9.0
- [#17887](https://github.com/influxdata/telegraf/pull/17887) `deps` Bump github.com/hashicorp/consul/api from 1.32.4 to 1.33.0
- [#18024](https://github.com/influxdata/telegraf/pull/18024) `deps` Bump github.com/jedib0t/go-pretty/v6 from 6.7.1 to 6.7.2
- [#18056](https://github.com/influxdata/telegraf/pull/18056) `deps` Bump github.com/jedib0t/go-pretty/v6 from 6.7.2 to 6.7.5
- [#18072](https://github.com/influxdata/telegraf/pull/18072) `deps` Bump github.com/klauspost/compress from 1.18.1 to 1.18.2
- [#18071](https://github.com/influxdata/telegraf/pull/18071) `deps` Bump github.com/lxc/incus/v6 from 6.18.0 to 6.19.1
- [#18018](https://github.com/influxdata/telegraf/pull/18018) `deps` Bump github.com/microsoft/go-mssqldb from 1.9.3 to 1.9.4
- [#18017](https://github.com/influxdata/telegraf/pull/18017) `deps` Bump github.com/nats-io/nats-server/v2 from 2.12.1 to 2.12.2
- [#18054](https://github.com/influxdata/telegraf/pull/18054) `deps` Bump github.com/prometheus/common from 0.67.2 to 0.67.4
- [#18053](https://github.com/influxdata/telegraf/pull/18053) `deps` Bump github.com/redis/go-redis/v9 from 9.16.0 to 9.17.0
- [#18073](https://github.com/influxdata/telegraf/pull/18073) `deps` Bump github.com/redis/go-redis/v9 from 9.17.0 to 9.17.2
- [#18027](https://github.com/influxdata/telegraf/pull/18027) `deps` Bump github.com/safchain/ethtool from 0.6.2 to 0.7.0
- [#18070](https://github.com/influxdata/telegraf/pull/18070) `deps` Bump github.com/shirou/gopsutil/v4 from 4.25.10 to 4.25.11
- [#18057](https://github.com/influxdata/telegraf/pull/18057) `deps` Bump github.com/snowflakedb/gosnowflake from 1.17.0 to 1.18.0
- [#17815](https://github.com/influxdata/telegraf/pull/17815) `deps` Bump github.com/vertica/vertica-sql-go from 1.3.3 to 1.3.4
- [#18031](https://github.com/influxdata/telegraf/pull/18031) `deps` Bump go.opentelemetry.io/collector/pdata from 1.45.0 to 1.46.0
- [#18043](https://github.com/influxdata/telegraf/pull/18043) `deps` Bump golang.org/x/crypto from 0.44.0 to 0.45.0
- [#18023](https://github.com/influxdata/telegraf/pull/18023) `deps` Bump golang.org/x/mod from 0.29.0 to 0.30.0
- [#18029](https://github.com/influxdata/telegraf/pull/18029) `deps` Bump golang.org/x/net from 0.46.0 to 0.47.0
- [#18025](https://github.com/influxdata/telegraf/pull/18025) `deps` Bump google.golang.org/api from 0.255.0 to 0.256.0
- [#18058](https://github.com/influxdata/telegraf/pull/18058) `deps` Bump google.golang.org/grpc from 1.76.0 to 1.77.0
- [#18033](https://github.com/influxdata/telegraf/pull/18033) `deps` Bump k8s.io/client-go from 0.34.1 to 0.34.2
- [#18030](https://github.com/influxdata/telegraf/pull/18030) `deps` Bump modernc.org/sqlite from 1.40.0 to 1.40.1
- [#18069](https://github.com/influxdata/telegraf/pull/18069) `deps` Bump super-linter/super-linter from 8.2.1 to 8.3.0
- [#18052](https://github.com/influxdata/telegraf/pull/18052) `deps` Bump the aws-sdk-go-v2 group with 11 updates
- [#18015](https://github.com/influxdata/telegraf/pull/18015) `deps` Bump the aws-sdk-go-v2 group with 9 updates

## v1.36.4 [2025-11-17]

### Bugfixes

- [#17873](https://github.com/influxdata/telegraf/pull/17873) `common.kafka` Avoid API version requests for SASLv0 handshakes
- [#17966](https://github.com/influxdata/telegraf/pull/17966) `config` Implement strict envvar handling to prevent insecure text replacement
- [#17877](https://github.com/influxdata/telegraf/pull/17877) `inputs.kinesis_consumer` Ignore expired parent shards
- [#17908](https://github.com/influxdata/telegraf/pull/17908) `inputs.tail` Handle missing read permissions for directory globbing
- [#17968](https://github.com/influxdata/telegraf/pull/17968) `inputs.turbostat` Allow floating point intervals
- [#17953](https://github.com/influxdata/telegraf/pull/17953) `inputs.zfs` Avoid panic by handling explicitly empty kstat metrics
- [#17949](https://github.com/influxdata/telegraf/pull/17949) `outputs.influxdb_v2` Handle serialization errors correctly
- [#17920](https://github.com/influxdata/telegraf/pull/17920) `outputs.loki` Sanitize colons in label names
- [#17990](https://github.com/influxdata/telegraf/pull/17990) `outputs.sql` Mark table as found during initial existence check

### Dependency Updates

- [#17935](https://github.com/influxdata/telegraf/pull/17935) `deps` Bump cloud.google.com/go/bigquery from 1.71.0 to 1.72.0
- [#17897](https://github.com/influxdata/telegraf/pull/17897) `deps` Bump cloud.google.com/go/pubsub/v2 from 2.2.1 to 2.3.0
- [#17943](https://github.com/influxdata/telegraf/pull/17943) `deps` Bump cloud.google.com/go/storage from 1.57.0 to 1.57.1
- [#17970](https://github.com/influxdata/telegraf/pull/17970) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azcore from 1.19.1 to 1.20.0
- [#17973](https://github.com/influxdata/telegraf/pull/17973) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azidentity from 1.13.0 to 1.13.1
- [#17901](https://github.com/influxdata/telegraf/pull/17901) `deps` Bump github.com/IBM/sarama from 1.46.2 to 1.46.3
- [#17889](https://github.com/influxdata/telegraf/pull/17889) `deps` Bump github.com/SAP/go-hdb from 1.14.7 to 1.14.9
- [#17977](https://github.com/influxdata/telegraf/pull/17977) `deps` Bump github.com/SAP/go-hdb from 1.14.9 to 1.14.12
- [#17981](https://github.com/influxdata/telegraf/pull/17981) `deps` Bump github.com/apache/iotdb-client-go from 1.3.4 to 1.3.5
- [#17900](https://github.com/influxdata/telegraf/pull/17900) `deps` Bump github.com/aws/aws-sdk-go-v2 from 1.39.3 to 1.39.4
- [#17899](https://github.com/influxdata/telegraf/pull/17899) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.31.13 to 1.31.15
- [#17898](https://github.com/influxdata/telegraf/pull/17898) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.51.2 to 1.51.4
- [#17858](https://github.com/influxdata/telegraf/pull/17858) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.58.2 to 1.58.3
- [#17892](https://github.com/influxdata/telegraf/pull/17892) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.58.3 to 1.58.5
- [#17854](https://github.com/influxdata/telegraf/pull/17854) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.51.0 to 1.51.1
- [#17890](https://github.com/influxdata/telegraf/pull/17890) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.51.1 to 1.52.2
- [#17855](https://github.com/influxdata/telegraf/pull/17855) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.255.0 to 1.257.2
- [#17886](https://github.com/influxdata/telegraf/pull/17886) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.257.2 to 1.258.1
- [#17883](https://github.com/influxdata/telegraf/pull/17883) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.40.6 to 1.41.0
- [#17847](https://github.com/influxdata/telegraf/pull/17847) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.35.5 to 1.35.6
- [#17891](https://github.com/influxdata/telegraf/pull/17891) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.35.6 to 1.35.7
- [#17944](https://github.com/influxdata/telegraf/pull/17944) `deps` Bump github.com/aws/smithy-go from 1.23.1 to 1.23.2
- [#17978](https://github.com/influxdata/telegraf/pull/17978) `deps` Bump github.com/docker/docker from 28.5.1+incompatible to 28.5.2+incompatible
- [#18009](https://github.com/influxdata/telegraf/pull/18009) `deps` Bump github.com/dvsekhvalnov/jose2go from 1.6.0 to 1.7.0
- [#17941](https://github.com/influxdata/telegraf/pull/17941) `deps` Bump github.com/gofrs/uuid/v5 from 5.3.2 to 5.4.0
- [#17927](https://github.com/influxdata/telegraf/pull/17927) `deps` Bump github.com/gopacket/gopacket from 1.4.0 to 1.5.0
- [#17988](https://github.com/influxdata/telegraf/pull/17988) `deps` Bump github.com/influxdata/toml from v0.0.0-20190415235208-270119a8ce65 to v0.0.0-20251106153700-c381e153d076
- [#17932](https://github.com/influxdata/telegraf/pull/17932) `deps` Bump github.com/jedib0t/go-pretty/v6 from 6.6.8 to 6.6.9
- [#17979](https://github.com/influxdata/telegraf/pull/17979) `deps` Bump github.com/jedib0t/go-pretty/v6 from 6.6.9 to 6.7.1
- [#17896](https://github.com/influxdata/telegraf/pull/17896) `deps` Bump github.com/linkedin/goavro/v2 from 2.14.0 to 2.14.1
- [#17942](https://github.com/influxdata/telegraf/pull/17942) `deps` Bump github.com/lxc/incus/v6 from 6.17.0 to 6.18.0
- [#17937](https://github.com/influxdata/telegraf/pull/17937) `deps` Bump github.com/prometheus/common from 0.67.1 to 0.67.2
- [#17885](https://github.com/influxdata/telegraf/pull/17885) `deps` Bump github.com/prometheus/procfs from 0.17.0 to 0.19.1
- [#17930](https://github.com/influxdata/telegraf/pull/17930) `deps` Bump github.com/prometheus/procfs from 0.19.1 to 0.19.2
- [#17894](https://github.com/influxdata/telegraf/pull/17894) `deps` Bump github.com/prometheus/prometheus from 0.307.1 to 0.307.2
- [#17928](https://github.com/influxdata/telegraf/pull/17928) `deps` Bump github.com/prometheus/prometheus from 0.307.2 to 0.307.3
- [#17895](https://github.com/influxdata/telegraf/pull/17895) `deps` Bump github.com/redis/go-redis/v9 from 9.14.1 to 9.16.0
- [#17939](https://github.com/influxdata/telegraf/pull/17939) `deps` Bump github.com/shirou/gopsutil/v4 from 4.25.9 to 4.25.10
- [#17976](https://github.com/influxdata/telegraf/pull/17976) `deps` Bump github.com/testcontainers/testcontainers-go from 0.39.0 to 0.40.0
- [#17983](https://github.com/influxdata/telegraf/pull/17983) `deps` Bump github.com/testcontainers/testcontainers-go/modules/azure from 0.39.0 to 0.40.0
- [#17972](https://github.com/influxdata/telegraf/pull/17972) `deps` Bump github.com/testcontainers/testcontainers-go/modules/kafka from 0.39.0 to 0.40.0
- [#17893](https://github.com/influxdata/telegraf/pull/17893) `deps` Bump github.com/tinylib/msgp from 1.4.0 to 1.5.0
- [#17934](https://github.com/influxdata/telegraf/pull/17934) `deps` Bump go.mongodb.org/mongo-driver from 1.17.4 to 1.17.6
- [#17865](https://github.com/influxdata/telegraf/pull/17865) `deps` Bump go.opentelemetry.io/collector/pdata from 1.43.0 to 1.44.0
- [#17945](https://github.com/influxdata/telegraf/pull/17945) `deps` Bump go.opentelemetry.io/collector/pdata from 1.44.0 to 1.45.0
- [#17933](https://github.com/influxdata/telegraf/pull/17933) `deps` Bump go.opentelemetry.io/proto/otlp from 1.8.0 to 1.9.0
- [#17938](https://github.com/influxdata/telegraf/pull/17938) `deps` Bump go.opentelemetry.io/proto/otlp/collector/profiles/v1development from 0.1.0 to 0.2.0
- [#17936](https://github.com/influxdata/telegraf/pull/17936) `deps` Bump go.step.sm/crypto from 0.72.0 to 0.73.0
- [#17974](https://github.com/influxdata/telegraf/pull/17974) `deps` Bump go.step.sm/crypto from 0.73.0 to 0.74.0
- [#17984](https://github.com/influxdata/telegraf/pull/17984) `deps` Bump golang.org/x/oauth2 from 0.32.0 to 0.33.0
- [#17980](https://github.com/influxdata/telegraf/pull/17980) `deps` Bump golang.org/x/sync from 0.17.0 to 0.18.0
- [#17971](https://github.com/influxdata/telegraf/pull/17971) `deps` Bump golang.org/x/sys from 0.37.0 to 0.38.0
- [#17884](https://github.com/influxdata/telegraf/pull/17884) `deps` Bump google.golang.org/api from 0.252.0 to 0.253.0
- [#17929](https://github.com/influxdata/telegraf/pull/17929) `deps` Bump google.golang.org/api from 0.253.0 to 0.254.0
- [#17975](https://github.com/influxdata/telegraf/pull/17975) `deps` Bump google.golang.org/api from 0.254.0 to 0.255.0
- [#17931](https://github.com/influxdata/telegraf/pull/17931) `deps` Bump modernc.org/sqlite from 1.39.1 to 1.40.0
- [#17926](https://github.com/influxdata/telegraf/pull/17926) `deps` Bump the aws-sdk-go-v2 group with 11 updates
- [#17969](https://github.com/influxdata/telegraf/pull/17969) `deps` Bump the aws-sdk-go-v2 group with 11 updates

## v1.36.3 [2025-10-21]

### Bugfixes

- [#17765](https://github.com/influxdata/telegraf/pull/17765) `inputs.chrony` Prevent race condition in concurrent gather calls
- [#17634](https://github.com/influxdata/telegraf/pull/17634) `inputs.docker` Fix incorrect CPU usage_percent for Podman containers
- [#17740](https://github.com/influxdata/telegraf/pull/17740) `inputs.kube_inventory` Prevent panic in endpoints' ready flag
- [#17483](https://github.com/influxdata/telegraf/pull/17483) `inputs.smart` Correct exit_status for active vs standby drives
- [#17617](https://github.com/influxdata/telegraf/pull/17617) `inputs.zfs` Parse field values according to provided type
- [#17787](https://github.com/influxdata/telegraf/pull/17787) `outputs.nats` Unwrap wrapped metrics to avoid panic on missing Field method
- [#17573](https://github.com/influxdata/telegraf/pull/17573) `parsers.csv` Support concurrent usage
- [#17738](https://github.com/influxdata/telegraf/pull/17738) `secretstores.systemd` Handle dash version separator correctly

### Dependency Updates

- [#17770](https://github.com/influxdata/telegraf/pull/17770) `deps` Bump cloud.google.com/go/bigquery from 1.70.0 to 1.71.0
- [#17821](https://github.com/influxdata/telegraf/pull/17821) `deps` Bump cloud.google.com/go/monitoring from 1.24.2 to 1.24.3
- [#17777](https://github.com/influxdata/telegraf/pull/17777) `deps` Bump cloud.google.com/go/pubsub/v2 from 2.0.0 to 2.2.0
- [#17846](https://github.com/influxdata/telegraf/pull/17846) `deps` Bump cloud.google.com/go/pubsub/v2 from 2.2.0 to 2.2.1
- [#17718](https://github.com/influxdata/telegraf/pull/17718) `deps` Bump cloud.google.com/go/storage from 1.56.2 to 1.57.0
- [#17805](https://github.com/influxdata/telegraf/pull/17805) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azidentity from 1.12.0 to 1.13.0
- [#17784](https://github.com/influxdata/telegraf/pull/17784) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs from 1.4.0 to 2.0.0
- [#17810](https://github.com/influxdata/telegraf/pull/17810) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2 from 2.0.0 to 2.0.1
- [#17804](https://github.com/influxdata/telegraf/pull/17804) `deps` Bump github.com/IBM/sarama from 1.46.1 to 1.46.2
- [#17724](https://github.com/influxdata/telegraf/pull/17724) `deps` Bump github.com/SAP/go-hdb from 1.14.4 to 1.14.5
- [#17808](https://github.com/influxdata/telegraf/pull/17808) `deps` Bump github.com/SAP/go-hdb from 1.14.5 to 1.14.6
- [#17866](https://github.com/influxdata/telegraf/pull/17866) `deps` Bump github.com/SAP/go-hdb from 1.14.6 to 1.14.7
- [#17822](https://github.com/influxdata/telegraf/pull/17822) `deps` Bump github.com/antchfx/xmlquery from 1.4.4 to 1.5.0
- [#17868](https://github.com/influxdata/telegraf/pull/17868) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.31.12 to 1.31.13
- [#17730](https://github.com/influxdata/telegraf/pull/17730) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.31.9 to 1.31.12
- [#17719](https://github.com/influxdata/telegraf/pull/17719) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.50.1 to 1.51.1
- [#17863](https://github.com/influxdata/telegraf/pull/17863) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.51.1 to 1.51.2
- [#17716](https://github.com/influxdata/telegraf/pull/17716) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.58.0 to 1.58.2
- [#17715](https://github.com/influxdata/telegraf/pull/17715) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.50.3 to 1.50.5
- [#17772](https://github.com/influxdata/telegraf/pull/17772) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.50.5 to 1.51.0
- [#17714](https://github.com/influxdata/telegraf/pull/17714) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.253.0 to 1.254.1
- [#17814](https://github.com/influxdata/telegraf/pull/17814) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.254.1 to 1.255.0
- [#17728](https://github.com/influxdata/telegraf/pull/17728) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.40.3 to 1.40.5
- [#17848](https://github.com/influxdata/telegraf/pull/17848) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.40.5 to 1.40.6
- [#17723](https://github.com/influxdata/telegraf/pull/17723) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.35.3 to 1.35.5
- [#17864](https://github.com/influxdata/telegraf/pull/17864) `deps` Bump github.com/aws/smithy-go from 1.23.0 to 1.23.1
- [#17849](https://github.com/influxdata/telegraf/pull/17849) `deps` Bump github.com/bluenviron/gomavlib/v3 from 3.2.1 to 3.3.0
- [#17774](https://github.com/influxdata/telegraf/pull/17774) `deps` Bump github.com/docker/docker from 28.4.0+incompatible to 28.5.0+incompatible
- [#17816](https://github.com/influxdata/telegraf/pull/17816) `deps` Bump github.com/docker/docker from 28.5.0+incompatible to 28.5.1+incompatible
- [#17769](https://github.com/influxdata/telegraf/pull/17769) `deps` Bump github.com/go-ldap/ldap/v3 from 3.4.11 to 3.4.12
- [#17775](https://github.com/influxdata/telegraf/pull/17775) `deps` Bump github.com/go-logfmt/logfmt from 0.6.0 to 0.6.1
- [#17727](https://github.com/influxdata/telegraf/pull/17727) `deps` Bump github.com/hashicorp/consul/api from 1.32.3 to 1.32.4
- [#17862](https://github.com/influxdata/telegraf/pull/17862) `deps` Bump github.com/klauspost/compress from 1.18.0 to 1.18.1
- [#17773](https://github.com/influxdata/telegraf/pull/17773) `deps` Bump github.com/leodido/go-syslog/v4 from 4.2.1-0.20250421191238-de2e76af1251 to 4.3.0
- [#17729](https://github.com/influxdata/telegraf/pull/17729) `deps` Bump github.com/lxc/incus/v6 from 6.16.0 to 6.17.0
- [#17860](https://github.com/influxdata/telegraf/pull/17860) `deps` Bump github.com/nats-io/nats-server/v2 from 2.12.0 to 2.12.1
- [#17766](https://github.com/influxdata/telegraf/pull/17766) `deps` Bump github.com/nats-io/nats.go from 1.46.0 to 1.46.1
- [#17851](https://github.com/influxdata/telegraf/pull/17851) `deps` Bump github.com/nats-io/nats.go from 1.46.1 to 1.47.0
- [#17813](https://github.com/influxdata/telegraf/pull/17813) `deps` Bump github.com/prometheus/common from 0.66.1 to 0.67.1
- [#17867](https://github.com/influxdata/telegraf/pull/17867) `deps` Bump github.com/prometheus/prometheus from 0.306.0 to 0.307.1
- [#17861](https://github.com/influxdata/telegraf/pull/17861) `deps` Bump github.com/redis/go-redis/v9 from 9.14.0 to 9.14.1
- [#17767](https://github.com/influxdata/telegraf/pull/17767) `deps` Bump github.com/shirou/gopsutil/v4 from 4.25.8 to 4.25.9
- [#17725](https://github.com/influxdata/telegraf/pull/17725) `deps` Bump github.com/snowflakedb/gosnowflake from 0.0.0-20250911095445-20c4d105d9a0 to 1.17.0
- [#17776](https://github.com/influxdata/telegraf/pull/17776) `deps` Bump go.opentelemetry.io/collector/pdata from 1.42.0 to 1.43.0
- [#17817](https://github.com/influxdata/telegraf/pull/17817) `deps` Bump go.step.sm/crypto from 0.70.0 to 0.71.0
- [#17857](https://github.com/influxdata/telegraf/pull/17857) `deps` Bump go.step.sm/crypto from 0.71.0 to 0.72.0
- [#17820](https://github.com/influxdata/telegraf/pull/17820) `deps` Bump golang.org/x/crypto from 0.42.0 to 0.43.0
- [#17806](https://github.com/influxdata/telegraf/pull/17806) `deps` Bump golang.org/x/mod from 0.28.0 to 0.29.0
- [#17819](https://github.com/influxdata/telegraf/pull/17819) `deps` Bump golang.org/x/net from 0.44.0 to 0.46.0
- [#17818](https://github.com/influxdata/telegraf/pull/17818) `deps` Bump golang.org/x/oauth2 from 0.31.0 to 0.32.0
- [#17823](https://github.com/influxdata/telegraf/pull/17823) `deps` Bump golang.org/x/sys from 0.36.0 to 0.37.0
- [#17717](https://github.com/influxdata/telegraf/pull/17717) `deps` Bump google.golang.org/api from 0.249.0 to 0.250.0
- [#17778](https://github.com/influxdata/telegraf/pull/17778) `deps` Bump google.golang.org/api from 0.250.0 to 0.251.0
- [#17807](https://github.com/influxdata/telegraf/pull/17807) `deps` Bump google.golang.org/api from 0.251.0 to 0.252.0
- [#17771](https://github.com/influxdata/telegraf/pull/17771) `deps` Bump google.golang.org/grpc from 1.75.1 to 1.76.0
- [#17768](https://github.com/influxdata/telegraf/pull/17768) `deps` Bump google.golang.org/protobuf from 1.36.9 to 1.36.10
- [#17811](https://github.com/influxdata/telegraf/pull/17811) `deps` Bump modernc.org/sqlite from 1.39.0 to 1.39.1
- [#17779](https://github.com/influxdata/telegraf/pull/17779) `deps` Bump super-linter/super-linter from 8.1.0 to 8.2.0
- [#17853](https://github.com/influxdata/telegraf/pull/17853) `deps` Bump super-linter/super-linter from 8.2.0 to 8.2.1
- [#17610](https://github.com/influxdata/telegraf/pull/17610) `deps` Switch to maintained yaml library
- [#17794](https://github.com/influxdata/telegraf/pull/17794) `deps` Update golangci-lint to 2.5.0

## v1.36.2 [2025-09-29]

### Bugfixes

- [#17609](https://github.com/influxdata/telegraf/pull/17609) `filter` Handle multiple conditions correctly
- [#17552](https://github.com/influxdata/telegraf/pull/17552) `inputs.procstat` Use correct values for disk_read_bytes, disk_write_bytes on Linux
- [#17613](https://github.com/influxdata/telegraf/pull/17613) `inputs.tail` Fix data race when cleaning up unused tailers

### Dependency Updates

- [#17599](https://github.com/influxdata/telegraf/pull/17599) `deps` Bump actions/setup-go from 5 to 6
- [#17650](https://github.com/influxdata/telegraf/pull/17650) `deps` Bump cloud.google.com/go/bigquery from 1.69.0 to 1.70.0
- [#17654](https://github.com/influxdata/telegraf/pull/17654) `deps` Bump cloud.google.com/go/storage from 1.56.1 to 1.56.2
- [#17688](https://github.com/influxdata/telegraf/pull/17688) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azcore from 1.19.0 to 1.19.1
- [#17683](https://github.com/influxdata/telegraf/pull/17683) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azidentity from 1.11.0 to 1.12.0
- [#17644](https://github.com/influxdata/telegraf/pull/17644) `deps` Bump github.com/ClickHouse/clickhouse-go/v2 from 2.40.1 to 2.40.3
- [#17522](https://github.com/influxdata/telegraf/pull/17522) `deps` Bump github.com/IBM/sarama from 1.45.2 to 1.46.0
- [#17682](https://github.com/influxdata/telegraf/pull/17682) `deps` Bump github.com/IBM/sarama from 1.46.0 to 1.46.1
- [#17636](https://github.com/influxdata/telegraf/pull/17636) `deps` Bump github.com/SAP/go-hdb from 1.14.0 to 1.14.3
- [#17677](https://github.com/influxdata/telegraf/pull/17677) `deps` Bump github.com/SAP/go-hdb from 1.14.3 to 1.14.4
- [#17647](https://github.com/influxdata/telegraf/pull/17647) `deps` Bump github.com/apache/arrow-go/v18 from 18.4.0 to 18.4.1
- [#17587](https://github.com/influxdata/telegraf/pull/17587) `deps` Bump github.com/apache/inlong/inlong-sdk/dataproxy-sdk-twins/dataproxy-sdk-golang from 1.0.5 to 1.0.6
- [#17642](https://github.com/influxdata/telegraf/pull/17642) `deps` Bump github.com/awnumar/memguard from 0.22.5 to 0.23.0
- [#17693](https://github.com/influxdata/telegraf/pull/17693) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.31.4 to 1.31.9
- [#17588](https://github.com/influxdata/telegraf/pull/17588) `deps` Bump github.com/aws/aws-sdk-go-v2/feature/ec2/imds from 1.18.5 to 1.18.7
- [#17641](https://github.com/influxdata/telegraf/pull/17641) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.48.2 to 1.50.1
- [#17656](https://github.com/influxdata/telegraf/pull/17656) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.57.0 to 1.57.4
- [#17690](https://github.com/influxdata/telegraf/pull/17690) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.57.4 to 1.58.0
- [#17596](https://github.com/influxdata/telegraf/pull/17596) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.49.1 to 1.50.2
- [#17649](https://github.com/influxdata/telegraf/pull/17649) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.50.2 to 1.50.3
- [#17583](https://github.com/influxdata/telegraf/pull/17583) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.246.0 to 1.251.1
- [#17640](https://github.com/influxdata/telegraf/pull/17640) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.251.1 to 1.251.2
- [#17681](https://github.com/influxdata/telegraf/pull/17681) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.251.2 to 1.253.0
- [#17595](https://github.com/influxdata/telegraf/pull/17595) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.39.1 to 1.40.2
- [#17646](https://github.com/influxdata/telegraf/pull/17646) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.40.2 to 1.40.3
- [#17638](https://github.com/influxdata/telegraf/pull/17638) `deps` Bump github.com/aws/aws-sdk-go-v2/service/sts from 1.38.1 to 1.38.4
- [#17582](https://github.com/influxdata/telegraf/pull/17582) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.34.2 to 1.35.2
- [#17658](https://github.com/influxdata/telegraf/pull/17658) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.35.2 to 1.35.3
- [#17673](https://github.com/influxdata/telegraf/pull/17673) `deps` Bump github.com/cloudevents/sdk-go/v2 from 2.16.1 to 2.16.2
- [#17601](https://github.com/influxdata/telegraf/pull/17601) `deps` Bump github.com/docker/docker from 28.3.3+incompatible to 28.4.0+incompatible
- [#17653](https://github.com/influxdata/telegraf/pull/17653) `deps` Bump github.com/eclipse/paho.golang from 0.22.0 to 0.23.0
- [#17680](https://github.com/influxdata/telegraf/pull/17680) `deps` Bump github.com/eclipse/paho.mqtt.golang from 1.5.0 to 1.5.1
- [#17597](https://github.com/influxdata/telegraf/pull/17597) `deps` Bump github.com/google/cel-go from 0.26.0 to 0.26.1
- [#17689](https://github.com/influxdata/telegraf/pull/17689) `deps` Bump github.com/hashicorp/consul/api from 1.32.1 to 1.32.3
- [#17651](https://github.com/influxdata/telegraf/pull/17651) `deps` Bump github.com/lxc/incus/v6 from 6.15.0 to 6.16.0
- [#17635](https://github.com/influxdata/telegraf/pull/17635) `deps` Bump github.com/nats-io/nats-server/v2 from 2.11.8 to 2.11.9
- [#17670](https://github.com/influxdata/telegraf/pull/17670) `deps` Bump github.com/nats-io/nats-server/v2 from 2.11.9 to 2.12.0
- [#17675](https://github.com/influxdata/telegraf/pull/17675) `deps` Bump github.com/nats-io/nats.go from 1.45.0 to 1.46.0
- [#17674](https://github.com/influxdata/telegraf/pull/17674) `deps` Bump github.com/peterbourgon/unixtransport from 0.0.6 to 0.0.7
- [#17593](https://github.com/influxdata/telegraf/pull/17593) `deps` Bump github.com/prometheus/client_golang from 1.23.0 to 1.23.2
- [#17585](https://github.com/influxdata/telegraf/pull/17585) `deps` Bump github.com/prometheus/common from 0.65.0 to 0.66.1
- [#17685](https://github.com/influxdata/telegraf/pull/17685) `deps` Bump github.com/prometheus/prometheus from 0.305.0 to 0.306.0
- [#17329](https://github.com/influxdata/telegraf/pull/17329) `deps` Bump github.com/prometheus/prometheus from 0.54.1 to 0.305.0
- [#17645](https://github.com/influxdata/telegraf/pull/17645) `deps` Bump github.com/redis/go-redis/v9 from 9.12.1 to 9.14.0
- [#17567](https://github.com/influxdata/telegraf/pull/17567) `deps` Bump github.com/shirou/gopsutil/v4 from 4.25.7 to 4.25.8
- [#17699](https://github.com/influxdata/telegraf/pull/17699) `deps` Bump github.com/snowflakedb/gosnowflake from 1.16.0 to 0.0.0-20250911095445-20c4d105d9a0
- [#17590](https://github.com/influxdata/telegraf/pull/17590) `deps` Bump github.com/stretchr/testify from 1.10.0 to 1.11.1
- [#17687](https://github.com/influxdata/telegraf/pull/17687) `deps` Bump github.com/testcontainers/testcontainers-go from 0.38.0 to 0.39.0
- [#17676](https://github.com/influxdata/telegraf/pull/17676) `deps` Bump github.com/testcontainers/testcontainers-go/modules/azure from 0.38.0 to 0.39.0
- [#17671](https://github.com/influxdata/telegraf/pull/17671) `deps` Bump github.com/testcontainers/testcontainers-go/modules/kafka from 0.38.0 to 0.39.0
- [#17584](https://github.com/influxdata/telegraf/pull/17584) `deps` Bump github.com/tidwall/wal from 1.2.0 to 1.2.1
- [#17581](https://github.com/influxdata/telegraf/pull/17581) `deps` Bump github.com/tinylib/msgp from 1.3.0 to 1.4.0
- [#17591](https://github.com/influxdata/telegraf/pull/17591) `deps` Bump go.opentelemetry.io/collector/pdata from 1.39.0 to 1.41.0
- [#17686](https://github.com/influxdata/telegraf/pull/17686) `deps` Bump go.opentelemetry.io/collector/pdata from 1.41.0 to 1.42.0
- [#17602](https://github.com/influxdata/telegraf/pull/17602) `deps` Bump go.opentelemetry.io/proto/otlp from 1.7.0 to 1.8.0
- [#17652](https://github.com/influxdata/telegraf/pull/17652) `deps` Bump golang.org/x/crypto from 0.41.0 to 0.42.0
- [#17691](https://github.com/influxdata/telegraf/pull/17691) `deps` Bump golang.org/x/mod from 0.27.0 to 0.28.0
- [#17655](https://github.com/influxdata/telegraf/pull/17655) `deps` Bump golang.org/x/oauth2 from 0.30.0 to 0.31.0
- [#17589](https://github.com/influxdata/telegraf/pull/17589) `deps` Bump golang.org/x/sync from 0.16.0 to 0.17.0
- [#17580](https://github.com/influxdata/telegraf/pull/17580) `deps` Bump golang.org/x/term from 0.34.0 to 0.35.0
- [#17679](https://github.com/influxdata/telegraf/pull/17679) `deps` Bump google.golang.org/api from 0.248.0 to 0.249.0
- [#17639](https://github.com/influxdata/telegraf/pull/17639) `deps` Bump google.golang.org/grpc from 1.75.0 to 1.75.1
- [#17643](https://github.com/influxdata/telegraf/pull/17643) `deps` Bump google.golang.org/protobuf from 1.36.8 to 1.36.9
- [#17598](https://github.com/influxdata/telegraf/pull/17598) `deps` Bump k8s.io/api from 0.33.4 to 0.34.0
- [#17692](https://github.com/influxdata/telegraf/pull/17692) `deps` Bump k8s.io/client-go from 0.34.0 to 0.34.1
- [#17657](https://github.com/influxdata/telegraf/pull/17657) `deps` Bump modernc.org/sqlite from 1.38.2 to 1.39.0
- [#17648](https://github.com/influxdata/telegraf/pull/17648) `deps` Bump tj-actions/changed-files from 46.0.5 to 47.0.0
- [#17707](https://github.com/influxdata/telegraf/pull/17707) `deps` Remove collectd replacement

## v1.36.1 [2025-09-08]

### Bugfixes

- [#17605](https://github.com/influxdata/telegraf/pull/17605) `outputs.influxdb` Fix crash on init

## v1.36.0 [2025-09-08]

### Important Changes

- PR [#17355](https://github.com/influxdata/telegraf/pull/17355) changes the `profiles` support
  of `inputs.opentelemetry` from the `v1 experimental` to the `v1 development` as this experimental API
  is updated upstream. This will change the metric by for example removing the no-longer reported
  `frame_type`, `stack_trace_id`, `build_id`, and `build_id_type` fields. Also, the value format of other fields
  or tags might have changed. Please refer to the
  [OpenTelemetry documentation](https://opentelemetry.io/docs/) for more details.

### New Plugins

- [#17368](https://github.com/influxdata/telegraf/pull/17368) `inputs.turbostat` Add plugin
- [#17078](https://github.com/influxdata/telegraf/pull/17078) `processors.round` Add plugin

### Features

- [#16705](https://github.com/influxdata/telegraf/pull/16705) `agent` Introduce labels and selectors to enable and disable plugins
- [#17547](https://github.com/influxdata/telegraf/pull/17547) `inputs.influxdb_v2_listener` Add `/health` route
- [#17312](https://github.com/influxdata/telegraf/pull/17312) `inputs.internal` Allow to collect statistics per plugin instance
- [#17024](https://github.com/influxdata/telegraf/pull/17024) `inputs.lvm` Add sync_percent for lvm_logical_vol
- [#17355](https://github.com/influxdata/telegraf/pull/17355) `inputs.opentelemetry` Upgrade otlp proto module
- [#17156](https://github.com/influxdata/telegraf/pull/17156) `inputs.syslog` Add support for RFC3164 over TCP
- [#17543](https://github.com/influxdata/telegraf/pull/17543) `inputs.syslog` Allow limiting message size in octet counting mode
- [#17539](https://github.com/influxdata/telegraf/pull/17539) `inputs.x509_cert` Add support for Windows certificate stores
- [#17244](https://github.com/influxdata/telegraf/pull/17244) `output.nats` Allow disabling stream creation for externally managed streams
- [#17474](https://github.com/influxdata/telegraf/pull/17474) `outputs.elasticsearch` Support array headers and preserve commas in values
- [#17548](https://github.com/influxdata/telegraf/pull/17548) `outputs.influxdb` Add internal statistics for written bytes
- [#17213](https://github.com/influxdata/telegraf/pull/17213) `outputs.nats` Allow providing a subject layout
- [#17346](https://github.com/influxdata/telegraf/pull/17346) `outputs.nats` Enable batch serialization with use_batch_format
- [#17249](https://github.com/influxdata/telegraf/pull/17249) `outputs.sql` Allow sending batches of metrics in transactions
- [#17510](https://github.com/influxdata/telegraf/pull/17510) `parsers.avro` Support record arrays at root level
- [#17365](https://github.com/influxdata/telegraf/pull/17365) `plugins.snmp` Allow debug logging in gosnmp
- [#17345](https://github.com/influxdata/telegraf/pull/17345) `selfstat` Implement collection of plugin-internal statistics

### Bugfixes

- [#17411](https://github.com/influxdata/telegraf/pull/17411) `inputs.diskio` Handle counter wrapping in io fields
- [#17551](https://github.com/influxdata/telegraf/pull/17551) `inputs.s7comm` Use correct value for string length with 'extra' parameter
- [#17579](https://github.com/influxdata/telegraf/pull/17579) `internal` Extract go version more robustly
- [#17566](https://github.com/influxdata/telegraf/pull/17566) `outputs` Retrigger batch-available-events only if at least one metric was written successfully
- [#17381](https://github.com/influxdata/telegraf/pull/17381) `packaging` Rename rpm from loong64 to loongarch64

### Dependency Updates

- [#17519](https://github.com/influxdata/telegraf/pull/17519) `deps` Bump cloud.google.com/go/storage from 1.56.0 to 1.56.1
- [#17532](https://github.com/influxdata/telegraf/pull/17532) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azcore from 1.18.2 to 1.19.0
- [#17494](https://github.com/influxdata/telegraf/pull/17494) `deps` Bump github.com/SAP/go-hdb from 1.13.12 to 1.14.0
- [#17488](https://github.com/influxdata/telegraf/pull/17488) `deps` Bump github.com/antchfx/xpath from 1.3.4 to 1.3.5
- [#17540](https://github.com/influxdata/telegraf/pull/17540) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.31.0 to 1.31.2
- [#17538](https://github.com/influxdata/telegraf/pull/17538) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.18.4 to 1.18.6
- [#17517](https://github.com/influxdata/telegraf/pull/17517) `deps` Bump github.com/aws/aws-sdk-go-v2/feature/ec2/imds from 1.18.3 to 1.18.4
- [#17528](https://github.com/influxdata/telegraf/pull/17528) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.48.0 to 1.48.2
- [#17536](https://github.com/influxdata/telegraf/pull/17536) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.56.0 to 1.57.0
- [#17524](https://github.com/influxdata/telegraf/pull/17524) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.46.0 to 1.49.1
- [#17493](https://github.com/influxdata/telegraf/pull/17493) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.242.0 to 1.244.0
- [#17527](https://github.com/influxdata/telegraf/pull/17527) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.244.0 to 1.246.0
- [#17530](https://github.com/influxdata/telegraf/pull/17530) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.38.0 to 1.39.1
- [#17534](https://github.com/influxdata/telegraf/pull/17534) `deps` Bump github.com/aws/aws-sdk-go-v2/service/sts from 1.37.0 to 1.38.0
- [#17513](https://github.com/influxdata/telegraf/pull/17513) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.34.0 to 1.34.2
- [#17514](https://github.com/influxdata/telegraf/pull/17514) `deps` Bump github.com/coreos/go-systemd/v22 from 22.5.0 to 22.6.0
- [#17563](https://github.com/influxdata/telegraf/pull/17563) `deps` Bump github.com/facebook/time from 0.0.0-20240626113945-18207c5d8ddc to 0.0.0-20250903103710-a5911c32cdb9
- [#17526](https://github.com/influxdata/telegraf/pull/17526) `deps` Bump github.com/gophercloud/gophercloud/v2 from 2.7.0 to 2.8.0
- [#17537](https://github.com/influxdata/telegraf/pull/17537) `deps` Bump github.com/microsoft/go-mssqldb from 1.9.2 to 1.9.3
- [#17490](https://github.com/influxdata/telegraf/pull/17490) `deps` Bump github.com/nats-io/nats-server/v2 from 2.11.7 to 2.11.8
- [#17523](https://github.com/influxdata/telegraf/pull/17523) `deps` Bump github.com/nats-io/nats.go from 1.44.0 to 1.45.0
- [#17492](https://github.com/influxdata/telegraf/pull/17492) `deps` Bump github.com/safchain/ethtool from 0.5.10 to 0.6.2
- [#17486](https://github.com/influxdata/telegraf/pull/17486) `deps` Bump github.com/snowflakedb/gosnowflake from 1.15.0 to 1.16.0
- [#17541](https://github.com/influxdata/telegraf/pull/17541) `deps` Bump github.com/tidwall/wal from 1.1.8 to 1.2.0
- [#17529](https://github.com/influxdata/telegraf/pull/17529) `deps` Bump github.com/vmware/govmomi from 0.51.0 to 0.52.0
- [#17496](https://github.com/influxdata/telegraf/pull/17496) `deps` Bump go.opentelemetry.io/collector/pdata from 1.36.1 to 1.38.0
- [#17533](https://github.com/influxdata/telegraf/pull/17533) `deps` Bump go.opentelemetry.io/collector/pdata from 1.38.0 to 1.39.0
- [#17516](https://github.com/influxdata/telegraf/pull/17516) `deps` Bump go.step.sm/crypto from 0.69.0 to 0.70.0
- [#17499](https://github.com/influxdata/telegraf/pull/17499) `deps` Bump golang.org/x/mod from 0.26.0 to 0.27.0
- [#17497](https://github.com/influxdata/telegraf/pull/17497) `deps` Bump golang.org/x/net from 0.42.0 to 0.43.0
- [#17487](https://github.com/influxdata/telegraf/pull/17487) `deps` Bump google.golang.org/api from 0.246.0 to 0.247.0
- [#17531](https://github.com/influxdata/telegraf/pull/17531) `deps` Bump google.golang.org/api from 0.247.0 to 0.248.0
- [#17520](https://github.com/influxdata/telegraf/pull/17520) `deps` Bump google.golang.org/grpc from 1.74.2 to 1.75.0
- [#17518](https://github.com/influxdata/telegraf/pull/17518) `deps` Bump google.golang.org/protobuf from 1.36.7 to 1.36.8
- [#17498](https://github.com/influxdata/telegraf/pull/17498) `deps` Bump k8s.io/client-go from 0.33.3 to 0.33.4
- [#17515](https://github.com/influxdata/telegraf/pull/17515) `deps` Bump super-linter/super-linter from 8.0.0 to 8.1.0

## v1.35.4 [2025-08-18]

### Bugfixes

- [#17451](https://github.com/influxdata/telegraf/pull/17451) `agent` Update help message for CLI flag --test
- [#17413](https://github.com/influxdata/telegraf/pull/17413) `inputs.gnmi` Handle empty updates in gnmi notification response
- [#17445](https://github.com/influxdata/telegraf/pull/17445) `inputs.redfish` Log correct address on HTTP error

### Dependency Updates

- [#17454](https://github.com/influxdata/telegraf/pull/17454) `deps` Bump actions/checkout from 4 to 5
- [#17404](https://github.com/influxdata/telegraf/pull/17404) `deps` Bump cloud.google.com/go/storage from 1.55.0 to 1.56.0
- [#17428](https://github.com/influxdata/telegraf/pull/17428) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azcore from 1.18.1 to 1.18.2
- [#17455](https://github.com/influxdata/telegraf/pull/17455) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azidentity from 1.10.1 to 1.11.0
- [#17383](https://github.com/influxdata/telegraf/pull/17383) `deps` Bump github.com/ClickHouse/clickhouse-go/v2 from 2.37.2 to 2.39.0
- [#17435](https://github.com/influxdata/telegraf/pull/17435) `deps` Bump github.com/ClickHouse/clickhouse-go/v2 from 2.39.0 to 2.40.1
- [#17393](https://github.com/influxdata/telegraf/pull/17393) `deps` Bump github.com/apache/arrow-go/v18 from 18.3.1 to 18.4.0
- [#17439](https://github.com/influxdata/telegraf/pull/17439) `deps` Bump github.com/apache/inlong/inlong-sdk/dataproxy-sdk-twins/dataproxy-sdk-golang from 1.0.3 to 1.0.5
- [#17437](https://github.com/influxdata/telegraf/pull/17437) `deps` Bump github.com/aws/aws-sdk-go-v2 from 1.37.0 to 1.37.2
- [#17402](https://github.com/influxdata/telegraf/pull/17402) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.29.17 to 1.30.0
- [#17458](https://github.com/influxdata/telegraf/pull/17458) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.30.1 to 1.31.0
- [#17391](https://github.com/influxdata/telegraf/pull/17391) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.17.70 to 1.18.0
- [#17436](https://github.com/influxdata/telegraf/pull/17436) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.18.1 to 1.18.3
- [#17434](https://github.com/influxdata/telegraf/pull/17434) `deps` Bump github.com/aws/aws-sdk-go-v2/feature/ec2/imds from 1.18.0 to 1.18.2
- [#17461](https://github.com/influxdata/telegraf/pull/17461) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.45.3 to 1.48.0
- [#17392](https://github.com/influxdata/telegraf/pull/17392) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.51.0 to 1.54.0
- [#17440](https://github.com/influxdata/telegraf/pull/17440) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.54.0 to 1.55.0
- [#17473](https://github.com/influxdata/telegraf/pull/17473) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.55.0 to 1.56.0
- [#17431](https://github.com/influxdata/telegraf/pull/17431) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.44.0 to 1.46.0
- [#17470](https://github.com/influxdata/telegraf/pull/17470) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.231.0 to 1.242.0
- [#17397](https://github.com/influxdata/telegraf/pull/17397) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.35.3 to 1.36.0
- [#17430](https://github.com/influxdata/telegraf/pull/17430) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.36.0 to 1.37.0
- [#17469](https://github.com/influxdata/telegraf/pull/17469) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.37.0 to 1.38.0
- [#17432](https://github.com/influxdata/telegraf/pull/17432) `deps` Bump github.com/aws/aws-sdk-go-v2/service/sts from 1.35.0 to 1.36.0
- [#17401](https://github.com/influxdata/telegraf/pull/17401) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.31.2 to 1.32.0
- [#17421](https://github.com/influxdata/telegraf/pull/17421) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.32.0 to 1.33.0
- [#17464](https://github.com/influxdata/telegraf/pull/17464) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.33.0 to 1.34.0
- [#17457](https://github.com/influxdata/telegraf/pull/17457) `deps` Bump github.com/clarify/clarify-go from 0.4.0 to 0.4.1
- [#17407](https://github.com/influxdata/telegraf/pull/17407) `deps` Bump github.com/docker/docker from 28.3.2+incompatible to 28.3.3+incompatible
- [#17463](https://github.com/influxdata/telegraf/pull/17463) `deps` Bump github.com/docker/go-connections from 0.5.0 to 0.6.0
- [#17394](https://github.com/influxdata/telegraf/pull/17394) `deps` Bump github.com/golang-jwt/jwt/v5 from 5.2.2 to 5.2.3
- [#17423](https://github.com/influxdata/telegraf/pull/17423) `deps` Bump github.com/gopacket/gopacket from 1.3.1 to 1.4.0
- [#17399](https://github.com/influxdata/telegraf/pull/17399) `deps` Bump github.com/jedib0t/go-pretty/v6 from 6.6.7 to 6.6.8
- [#17422](https://github.com/influxdata/telegraf/pull/17422) `deps` Bump github.com/lxc/incus/v6 from 6.14.0 to 6.15.0
- [#17429](https://github.com/influxdata/telegraf/pull/17429) `deps` Bump github.com/miekg/dns from 1.1.67 to 1.1.68
- [#17433](https://github.com/influxdata/telegraf/pull/17433) `deps` Bump github.com/nats-io/nats-server/v2 from 2.11.6 to 2.11.7
- [#17426](https://github.com/influxdata/telegraf/pull/17426) `deps` Bump github.com/nats-io/nats.go from 1.43.0 to 1.44.0
- [#17456](https://github.com/influxdata/telegraf/pull/17456) `deps` Bump github.com/redis/go-redis/v9 from 9.11.0 to 9.12.1
- [#17420](https://github.com/influxdata/telegraf/pull/17420) `deps` Bump github.com/shirou/gopsutil/v4 from 4.25.6 to 4.25.7
- [#17388](https://github.com/influxdata/telegraf/pull/17388) `deps` Bump github.com/testcontainers/testcontainers-go/modules/azure from 0.37.0 to 0.38.0
- [#17382](https://github.com/influxdata/telegraf/pull/17382) `deps` Bump github.com/testcontainers/testcontainers-go/modules/kafka from 0.37.0 to 0.38.0
- [#17427](https://github.com/influxdata/telegraf/pull/17427) `deps` Bump github.com/yuin/goldmark from 1.7.12 to 1.7.13
- [#17386](https://github.com/influxdata/telegraf/pull/17386) `deps` Bump go.opentelemetry.io/collector/pdata from 1.36.0 to 1.36.1
- [#17425](https://github.com/influxdata/telegraf/pull/17425) `deps` Bump go.step.sm/crypto from 0.67.0 to 0.68.0
- [#17462](https://github.com/influxdata/telegraf/pull/17462) `deps` Bump go.step.sm/crypto from 0.68.0 to 0.69.0
- [#17460](https://github.com/influxdata/telegraf/pull/17460) `deps` Bump golang.org/x/crypto from 0.40.0 to 0.41.0
- [#17424](https://github.com/influxdata/telegraf/pull/17424) `deps` Bump google.golang.org/api from 0.243.0 to 0.244.0
- [#17459](https://github.com/influxdata/telegraf/pull/17459) `deps` Bump google.golang.org/api from 0.244.0 to 0.246.0
- [#17465](https://github.com/influxdata/telegraf/pull/17465) `deps` Bump google.golang.org/protobuf from 1.36.6 to 1.36.7
- [#17384](https://github.com/influxdata/telegraf/pull/17384) `deps` Bump k8s.io/apimachinery from 0.33.2 to 0.33.3
- [#17389](https://github.com/influxdata/telegraf/pull/17389) `deps` Bump k8s.io/client-go from 0.33.2 to 0.33.3
- [#17396](https://github.com/influxdata/telegraf/pull/17396) `deps` Bump modernc.org/sqlite from 1.38.0 to 1.38.1
- [#17385](https://github.com/influxdata/telegraf/pull/17385) `deps` Bump software.sslmate.com/src/go-pkcs12 from 0.5.0 to 0.6.0
- [#17390](https://github.com/influxdata/telegraf/pull/17390) `deps` Bump super-linter/super-linter from 7.4.0 to 8.0.0
- [#17448](https://github.com/influxdata/telegraf/pull/17448) `deps` Fix collectd dependency not resolving
- [#17410](https://github.com/influxdata/telegraf/pull/17410) `deps` Migrate from cloud.google.com/go/pubsub to v2

## v1.35.3 [2025-07-28]

### Bugfixes

- [#17373](https://github.com/influxdata/telegraf/pull/17373) `agent` Handle nil timer on telegraf reload when no debounce is specified
- [#17340](https://github.com/influxdata/telegraf/pull/17340) `agent` Make Windows service install more robust
- [#17310](https://github.com/influxdata/telegraf/pull/17310) `outputs.sql` Add timestamp to derived datatypes
- [#17349](https://github.com/influxdata/telegraf/pull/17349) `outputs` Retrigger batch-available-events only for non-failing writes
- [#17293](https://github.com/influxdata/telegraf/pull/17293) `parsers.json_v2` Respect string type for objects and arrays
- [#17367](https://github.com/influxdata/telegraf/pull/17367) `plugins.snmp` Update gosnmp to prevent panic in snmp agents
- [#17292](https://github.com/influxdata/telegraf/pull/17292) `processors.snmp_lookup` Avoid re-enqueing updates after plugin stopped
- [#17369](https://github.com/influxdata/telegraf/pull/17369) `processors.snmp_lookup` Prevent deadlock during plugin shutdown

### Dependency Updates

- [#17320](https://github.com/influxdata/telegraf/pull/17320) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azcore from 1.18.0 to 1.18.1
- [#17328](https://github.com/influxdata/telegraf/pull/17328) `deps` Bump github.com/SAP/go-hdb from 1.13.11 to 1.13.12
- [#17301](https://github.com/influxdata/telegraf/pull/17301) `deps` Bump github.com/SAP/go-hdb from 1.13.9 to 1.13.11
- [#17326](https://github.com/influxdata/telegraf/pull/17326) `deps` Bump github.com/alitto/pond/v2 from 2.4.0 to 2.5.0
- [#17295](https://github.com/influxdata/telegraf/pull/17295) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.227.0 to 1.230.0
- [#17332](https://github.com/influxdata/telegraf/pull/17332) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.230.0 to 1.231.0
- [#17300](https://github.com/influxdata/telegraf/pull/17300) `deps` Bump github.com/docker/docker from 28.3.0+incompatible to 28.3.1+incompatible
- [#17334](https://github.com/influxdata/telegraf/pull/17334) `deps` Bump github.com/docker/docker from 28.3.1+incompatible to 28.3.2+incompatible
- [#17327](https://github.com/influxdata/telegraf/pull/17327) `deps` Bump github.com/google/cel-go from 0.25.0 to 0.26.0
- [#17331](https://github.com/influxdata/telegraf/pull/17331) `deps` Bump github.com/miekg/dns from 1.1.66 to 1.1.67
- [#17297](https://github.com/influxdata/telegraf/pull/17297) `deps` Bump github.com/nats-io/nats-server/v2 from 2.11.5 to 2.11.6
- [#17321](https://github.com/influxdata/telegraf/pull/17321) `deps` Bump github.com/openconfig/goyang from 1.6.2 to 1.6.3
- [#17298](https://github.com/influxdata/telegraf/pull/17298) `deps` Bump github.com/prometheus/procfs from 0.16.1 to 0.17.0
- [#17296](https://github.com/influxdata/telegraf/pull/17296) `deps` Bump github.com/shirou/gopsutil/v4 from 4.25.5 to 4.25.6
- [#17299](https://github.com/influxdata/telegraf/pull/17299) `deps` Bump github.com/snowflakedb/gosnowflake from 1.14.1 to 1.15.0
- [#17323](https://github.com/influxdata/telegraf/pull/17323) `deps` Bump go.opentelemetry.io/collector/pdata from 1.35.0 to 1.36.0
- [#17091](https://github.com/influxdata/telegraf/pull/17091) `deps` Bump go.step.sm/crypto from 0.64.0 to 0.67.0
- [#17330](https://github.com/influxdata/telegraf/pull/17330) `deps` Bump golang.org/x/crypto from 0.39.0 to 0.40.0
- [#17322](https://github.com/influxdata/telegraf/pull/17322) `deps` Bump golang.org/x/mod from 0.25.0 to 0.26.0
- [#17336](https://github.com/influxdata/telegraf/pull/17336) `deps` Bump golang.org/x/net from 0.41.0 to 0.42.0
- [#17337](https://github.com/influxdata/telegraf/pull/17337) `deps` Bump golang.org/x/sys from 0.33.0 to 0.34.0
- [#17335](https://github.com/influxdata/telegraf/pull/17335) `deps` Bump golang.org/x/term from 0.32.0 to 0.33.0
- [#17294](https://github.com/influxdata/telegraf/pull/17294) `deps` Bump google.golang.org/api from 0.239.0 to 0.240.0
- [#17325](https://github.com/influxdata/telegraf/pull/17325) `deps` Bump google.golang.org/api from 0.240.0 to 0.241.0
- [#17138](https://github.com/influxdata/telegraf/pull/17138) `deps` Bump modernc.org/sqlite from 1.37.0 to 1.38.0

## v1.35.2 [2025-07-07]

### Bugfixes

- [#17248](https://github.com/influxdata/telegraf/pull/17248) `agent` Add missing config flags for migrate command
- [#17240](https://github.com/influxdata/telegraf/pull/17240) `disk-buffer` Correctly reset the mask after adding to an empty buffer
- [#17284](https://github.com/influxdata/telegraf/pull/17284) `disk-buffer` Expire metric tracking information in the right place
- [#17257](https://github.com/influxdata/telegraf/pull/17257) `disk-buffer` Mask old tracking metrics on restart
- [#17247](https://github.com/influxdata/telegraf/pull/17247) `disk-buffer` Remove empty buffer on close
- [#17285](https://github.com/influxdata/telegraf/pull/17285) `inputs.gnmi` Avoid interpreting path elements with multiple colons as namespace
- [#17278](https://github.com/influxdata/telegraf/pull/17278) `inputs.gnmi` Handle base64 encoded IEEE-754 floats correctly
- [#17258](https://github.com/influxdata/telegraf/pull/17258) `inputs.kibana` Support Kibana 8.x status API format change
- [#17214](https://github.com/influxdata/telegraf/pull/17214) `inputs.ntpq` Fix ntpq field misalignment parsing errors
- [#17234](https://github.com/influxdata/telegraf/pull/17234) `outputs.microsoft_fabric` Correct app name
- [#17291](https://github.com/influxdata/telegraf/pull/17291) `outputs.nats` Avoid initializing Jetstream unconditionally
- [#17246](https://github.com/influxdata/telegraf/pull/17246) `outputs` Retrigger batch-available-events correctly

### Dependency Updates

- [#17217](https://github.com/influxdata/telegraf/pull/17217) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs from 1.3.2 to 1.4.0
- [#17226](https://github.com/influxdata/telegraf/pull/17226) `deps` Bump github.com/ClickHouse/clickhouse-go/v2 from 2.37.0 to 2.37.1
- [#17265](https://github.com/influxdata/telegraf/pull/17265) `deps` Bump github.com/ClickHouse/clickhouse-go/v2 from 2.37.1 to 2.37.2
- [#17268](https://github.com/influxdata/telegraf/pull/17268) `deps` Bump github.com/Masterminds/semver/v3 from 3.3.1 to 3.4.0
- [#17271](https://github.com/influxdata/telegraf/pull/17271) `deps` Bump github.com/SAP/go-hdb from 1.13.7 to 1.13.9
- [#17232](https://github.com/influxdata/telegraf/pull/17232) `deps` Bump github.com/alitto/pond/v2 from 2.3.4 to 2.4.0
- [#17231](https://github.com/influxdata/telegraf/pull/17231) `deps` Bump github.com/apache/arrow-go/v18 from 18.3.0 to 18.3.1
- [#17223](https://github.com/influxdata/telegraf/pull/17223) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.29.15 to 1.29.17
- [#17220](https://github.com/influxdata/telegraf/pull/17220) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.17.69 to 1.17.70
- [#17227](https://github.com/influxdata/telegraf/pull/17227) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.50.3 to 1.51.0
- [#17262](https://github.com/influxdata/telegraf/pull/17262) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.43.4 to 1.44.0
- [#17224](https://github.com/influxdata/telegraf/pull/17224) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.225.1 to 1.225.2
- [#17260](https://github.com/influxdata/telegraf/pull/17260) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.226.0 to 1.227.0
- [#17264](https://github.com/influxdata/telegraf/pull/17264) `deps` Bump github.com/docker/docker from 28.2.2+incompatible to 28.3.0+incompatible
- [#17256](https://github.com/influxdata/telegraf/pull/17256) `deps` Bump github.com/lxc/incus/v6 from 6.13.0 to 6.14.0
- [#17272](https://github.com/influxdata/telegraf/pull/17272) `deps` Bump github.com/microsoft/go-mssqldb from 1.8.2 to 1.9.2
- [#17261](https://github.com/influxdata/telegraf/pull/17261) `deps` Bump github.com/nats-io/nats-server/v2 from 2.11.4 to 2.11.5
- [#17266](https://github.com/influxdata/telegraf/pull/17266) `deps` Bump github.com/peterbourgon/unixtransport from 0.0.5 to 0.0.6
- [#17229](https://github.com/influxdata/telegraf/pull/17229) `deps` Bump github.com/prometheus/common from 0.64.0 to 0.65.0
- [#17267](https://github.com/influxdata/telegraf/pull/17267) `deps` Bump github.com/redis/go-redis/v9 from 9.10.0 to 9.11.0
- [#17273](https://github.com/influxdata/telegraf/pull/17273) `deps` Bump go.opentelemetry.io/collector/pdata from 1.34.0 to 1.35.0
- [#17219](https://github.com/influxdata/telegraf/pull/17219) `deps` Bump google.golang.org/api from 0.237.0 to 0.238.0
- [#17263](https://github.com/influxdata/telegraf/pull/17263) `deps` Bump google.golang.org/api from 0.238.0 to 0.239.0
- [#17218](https://github.com/influxdata/telegraf/pull/17218) `deps` Bump k8s.io/api from 0.33.1 to 0.33.2
- [#17228](https://github.com/influxdata/telegraf/pull/17228) `deps` Bump k8s.io/client-go from 0.33.1 to 0.33.2

## v1.35.1 [2025-06-23]

### Bugfixes

- [#17178](https://github.com/influxdata/telegraf/pull/17178) `inputs.procstat` Fix user filter conditional logic
- [#17210](https://github.com/influxdata/telegraf/pull/17210) `processors.strings` Add explicit TOML tags on struct fields

### Dependency Updates

- [#17194](https://github.com/influxdata/telegraf/pull/17194) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azidentity from 1.10.0 to 1.10.1
- [#17189](https://github.com/influxdata/telegraf/pull/17189) `deps` Bump github.com/ClickHouse/clickhouse-go/v2 from 2.36.0 to 2.37.0
- [#17186](https://github.com/influxdata/telegraf/pull/17186) `deps` Bump github.com/SAP/go-hdb from 1.13.6 to 1.13.7
- [#17188](https://github.com/influxdata/telegraf/pull/17188) `deps` Bump github.com/alitto/pond/v2 from 2.3.2 to 2.3.4
- [#17180](https://github.com/influxdata/telegraf/pull/17180) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.17.68 to 1.17.69
- [#17185](https://github.com/influxdata/telegraf/pull/17185) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.45.1 to 1.45.2
- [#17187](https://github.com/influxdata/telegraf/pull/17187) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.50.1 to 1.50.2
- [#17183](https://github.com/influxdata/telegraf/pull/17183) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.43.2 to 1.43.3
- [#17182](https://github.com/influxdata/telegraf/pull/17182) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.225.0 to 1.225.1
- [#17190](https://github.com/influxdata/telegraf/pull/17190) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.35.1 to 1.35.2
- [#17193](https://github.com/influxdata/telegraf/pull/17193) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.31.0 to 1.31.1
- [#17195](https://github.com/influxdata/telegraf/pull/17195) `deps` Bump github.com/aws/smithy-go from 1.22.3 to 1.22.4
- [#17196](https://github.com/influxdata/telegraf/pull/17196) `deps` Bump github.com/cloudevents/sdk-go/v2 from 2.16.0 to 2.16.1
- [#17212](https://github.com/influxdata/telegraf/pull/17212) `deps` Bump github.com/go-chi/chi/v5 from 5.2.1 to 5.2.2
- [#17191](https://github.com/influxdata/telegraf/pull/17191) `deps` Bump github.com/go-sql-driver/mysql from 1.9.2 to 1.9.3
- [#17192](https://github.com/influxdata/telegraf/pull/17192) `deps` Bump github.com/peterbourgon/unixtransport from 0.0.4 to 0.0.5
- [#17181](https://github.com/influxdata/telegraf/pull/17181) `deps` Bump github.com/redis/go-redis/v9 from 9.9.0 to 9.10.0
- [#17197](https://github.com/influxdata/telegraf/pull/17197) `deps` Bump github.com/urfave/cli/v2 from 2.27.6 to 2.27.7
- [#17198](https://github.com/influxdata/telegraf/pull/17198) `deps` Bump go.opentelemetry.io/collector/pdata from 1.33.0 to 1.34.0
- [#17184](https://github.com/influxdata/telegraf/pull/17184) `deps` Bump google.golang.org/api from 0.236.0 to 0.237.0

## v1.35.0 [2025-06-16]

### Deprecation Removals

This release removes the following deprecated plugin aliases:

- `inputs.cisco_telemetry_gnmi` in [#17101](https://github.com/influxdata/telegraf/pull/17101)
- `inputs.http_listener` in [#17102](https://github.com/influxdata/telegraf/pull/17102)
- `inputs.KNXListener` in [#17168](https://github.com/influxdata/telegraf/pull/17168)
- `inputs.logparser` in [#17170](https://github.com/influxdata/telegraf/pull/17170)

Furthermore, the following deprecated plugin options are removed:

- `ssl_ca`, `ssl_cert` and `ssl_key` of common TLS settings in [#17119](https://github.com/influxdata/telegraf/pull/17119)
- `url` of `inputs.amqp_consumer` in [#17149](https://github.com/influxdata/telegraf/pull/17149)
- `namespace` of `inputs.cloudwatch` in [#17123](https://github.com/influxdata/telegraf/pull/17123)
- `datacentre` of `inputs.consul` in [#17150](https://github.com/influxdata/telegraf/pull/17150)
- `container_names`, `perdevice` and `total` of `inputs.docker` in [#17148](https://github.com/influxdata/telegraf/pull/17148)
- `http_timeout` of `inputs.elasticsearch` in [#17124](https://github.com/influxdata/telegraf/pull/17124)
- `directory` of `inputs.filecount` in [#17152](https://github.com/influxdata/telegraf/pull/17152)
- `guess_path_tag` and `enable_tls` of `inputs.gnmi` in [#17151](https://github.com/influxdata/telegraf/pull/17151)
- `bearer_token` of `inputs.http` in [#17153](https://github.com/influxdata/telegraf/pull/17153)
- `path` and `port` of `inputs.http_listener_v2` in [#17158](https://github.com/influxdata/telegraf/pull/17158)
- `address` of `inputs.http_response` in [#17157](https://github.com/influxdata/telegraf/pull/17157)
- `object_type` of `inputs.icinga2` in [#17163](https://github.com/influxdata/telegraf/pull/17163)
- `max_line_size` of `inputs.influxdb_listener` in [#17162](https://github.com/influxdata/telegraf/pull/17162)
- `enable_file_download` of `inputs.internet_speed` in [#17165](https://github.com/influxdata/telegraf/pull/17165)
- `bearer_token_string` of `inputs.kube_inventory` in [#17110](https://github.com/influxdata/telegraf/pull/17110)
- `bearer_token_string` of `inputs.kubernetes` in [#17109](https://github.com/influxdata/telegraf/pull/17109)
- `server` of `inputs.nsq_consumer` in [#17166](https://github.com/influxdata/telegraf/pull/17166)
- `dns_lookup` of `inputs.ntpq` in [#17159](https://github.com/influxdata/telegraf/pull/17159)
- `ssl` of `inputs.openldap` in [#17103](https://github.com/influxdata/telegraf/pull/17103)
- `name` and `queues` of `inputs.rabbitmq` in [#17105](https://github.com/influxdata/telegraf/pull/17105)
- `path` of `inputs.smart` in [#17113](https://github.com/influxdata/telegraf/pull/17113)
- `azuredb` and `query_version` of `inputs.sqlserver` in [#17112](https://github.com/influxdata/telegraf/pull/17112)
- `parse_data_dog_tags` and `udp_packet_size` of `inputs.statsd` in [#17171](https://github.com/influxdata/telegraf/pull/17171)
- `force_discover_on_init` of `inputs.vsphere` in [#17169](https://github.com/influxdata/telegraf/pull/17169)
- `database`, `precision`, `retention_policy` and `url` of `outputs.amqp` in [#16950](https://github.com/influxdata/telegraf/pull/16950)
- `precision` of `outputs.influxdb` in [#17160](https://github.com/influxdata/telegraf/pull/17160)
- `partitionkey` and `use_random_partitionkey` of `outputs.kinesis` in [#17167](https://github.com/influxdata/telegraf/pull/17167)
- `source_tag` of `outputs.librato` in [#17174](https://github.com/influxdata/telegraf/pull/17174)
- `batch` and `topic_prefix` of `outputs.mqtt` in [#17176](https://github.com/influxdata/telegraf/pull/17176)
- `trace` of `outputs.remotefile` in [#17173](https://github.com/influxdata/telegraf/pull/17173)
- `host`, `port` and `string_to_number` of `outputs.wavefront` in [#17172](https://github.com/influxdata/telegraf/pull/17172)

Replacements do exist, so please migrate your configuration in case you are
still using one of those plugins or options. The `telegraf config migrate`
command might be able to assist with the procedure.

### New Plugins

- [#16390](https://github.com/influxdata/telegraf/pull/16390) `inputs.fritzbox` Add plugin
- [#16780](https://github.com/influxdata/telegraf/pull/16780) `inputs.mavlink` Add plugin
- [#16509](https://github.com/influxdata/telegraf/pull/16509) `inputs.whois` Add plugin
- [#16211](https://github.com/influxdata/telegraf/pull/16211) `outputs.inlong` Add plugin
- [#16827](https://github.com/influxdata/telegraf/pull/16827) `outputs.microsoft_fabric` Add plugin
- [#16629](https://github.com/influxdata/telegraf/pull/16629) `processors.cumulative_sum` Add plugin

### Features

- [#17048](https://github.com/influxdata/telegraf/pull/17048) `agent` Add debounce for watch events
- [#16524](https://github.com/influxdata/telegraf/pull/16524) `common.kafka` Add AWS-MSK-IAM SASL authentication
- [#16867](https://github.com/influxdata/telegraf/pull/16867) `common.ratelimiter` Implement means to reserve memory for concurrent use
- [#16148](https://github.com/influxdata/telegraf/pull/16148) `common.shim` Add batch to shim
- [#17121](https://github.com/influxdata/telegraf/pull/17121) `inputs.amqp_consumer` Allow string values in queue arguments
- [#17051](https://github.com/influxdata/telegraf/pull/17051) `inputs.opcua` Allow forcing reconnection on every gather cycle
- [#16532](https://github.com/influxdata/telegraf/pull/16532) `inputs.opcua_listener` Allow to subscribe to OPCUA events
- [#16882](https://github.com/influxdata/telegraf/pull/16882) `inputs.prometheus` Add HTTP service discovery support
- [#16999](https://github.com/influxdata/telegraf/pull/16999) `inputs.s7comm` Add support for LREAL and LINT data types
- [#16452](https://github.com/influxdata/telegraf/pull/16452) `inputs.unbound` Collect histogram statistics
- [#16700](https://github.com/influxdata/telegraf/pull/16700) `inputs.whois` Support IDN domains
- [#17119](https://github.com/influxdata/telegraf/pull/17119) `migrations` Add migration for common.tls ssl options
- [#17101](https://github.com/influxdata/telegraf/pull/17101) `migrations` Add migration for inputs.cisco_telemetry_gnmi
- [#17123](https://github.com/influxdata/telegraf/pull/17123) `migrations` Add migration for inputs.cloudwatch
- [#17148](https://github.com/influxdata/telegraf/pull/17148) `migrations` Add migration for inputs.docker
- [#17124](https://github.com/influxdata/telegraf/pull/17124) `migrations` Add migration for inputs.elasticsearch
- [#17102](https://github.com/influxdata/telegraf/pull/17102) `migrations` Add migration for inputs.http_listener
- [#17162](https://github.com/influxdata/telegraf/pull/17162) `migrations` Add migration for inputs.influxdb_listener
- [#17110](https://github.com/influxdata/telegraf/pull/17110) `migrations` Add migration for inputs.kube_inventory
- [#17109](https://github.com/influxdata/telegraf/pull/17109) `migrations` Add migration for inputs.kubernetes
- [#17103](https://github.com/influxdata/telegraf/pull/17103) `migrations` Add migration for inputs.openldap
- [#17105](https://github.com/influxdata/telegraf/pull/17105) `migrations` Add migration for inputs.rabbitmq
- [#17113](https://github.com/influxdata/telegraf/pull/17113) `migrations` Add migration for inputs.smart
- [#17112](https://github.com/influxdata/telegraf/pull/17112) `migrations` Add migration for inputs.sqlserver
- [#16950](https://github.com/influxdata/telegraf/pull/16950) `migrations` Add migration for outputs.amqp
- [#17160](https://github.com/influxdata/telegraf/pull/17160) `migrations` Add migration for outputs.influxdb
- [#17149](https://github.com/influxdata/telegraf/pull/17149) `migrations` Add migration for inputs.amqp_consumer
- [#17150](https://github.com/influxdata/telegraf/pull/17150) `migrations` Add migration for inputs.consul
- [#17152](https://github.com/influxdata/telegraf/pull/17152) `migrations` Add migration for inputs.filecount
- [#17151](https://github.com/influxdata/telegraf/pull/17151) `migrations` Add migration for inputs.gnmi
- [#17153](https://github.com/influxdata/telegraf/pull/17153) `migrations` Add migration for inputs.http
- [#17158](https://github.com/influxdata/telegraf/pull/17158) `migrations` Add migration for inputs.http_listener_v2
- [#17157](https://github.com/influxdata/telegraf/pull/17157) `migrations` Add migration for inputs.http_response
- [#17163](https://github.com/influxdata/telegraf/pull/17163) `migrations` Add migration for inputs.icinga2
- [#17165](https://github.com/influxdata/telegraf/pull/17165) `migrations` Add migration for inputs.internet_speed
- [#17166](https://github.com/influxdata/telegraf/pull/17166) `migrations` Add migration for inputs.nsq_consumer
- [#17159](https://github.com/influxdata/telegraf/pull/17159) `migrations` Add migration for inputs.ntpq
- [#17171](https://github.com/influxdata/telegraf/pull/17171) `migrations` Add migration for inputs.statsd
- [#17169](https://github.com/influxdata/telegraf/pull/17169) `migrations` Add migration for inputs.vsphere
- [#17167](https://github.com/influxdata/telegraf/pull/17167) `migrations` Add migration for outputs.kinesis
- [#17174](https://github.com/influxdata/telegraf/pull/17174) `migrations` Add migration for outputs.librato
- [#17176](https://github.com/influxdata/telegraf/pull/17176) `migrations` Add migration for outputs.mqtt
- [#17173](https://github.com/influxdata/telegraf/pull/17173) `migrations` Add migration for outputs.remotefile
- [#17172](https://github.com/influxdata/telegraf/pull/17172) `migrations` Add migration for outputs.wavefront
- [#17168](https://github.com/influxdata/telegraf/pull/17168) `migrations` Add migration for inputs.KNXListener
- [#17170](https://github.com/influxdata/telegraf/pull/17170) `migrations` Add migration for inputs.logparser
- [#16646](https://github.com/influxdata/telegraf/pull/16646) `outputs.health` Add max time between metrics check
- [#16597](https://github.com/influxdata/telegraf/pull/16597) `outputs.http` Include body sample in non-retryable error logs
- [#16741](https://github.com/influxdata/telegraf/pull/16741) `outputs.influxdb_v2` Implement concurrent writes
- [#16746](https://github.com/influxdata/telegraf/pull/16746) `outputs.influxdb_v2` Support secrets in http_headers values
- [#16582](https://github.com/influxdata/telegraf/pull/16582) `outputs.nats` Allow asynchronous publishing for Jetstream
- [#16544](https://github.com/influxdata/telegraf/pull/16544) `outputs.sql` Add option to automate table schema updates
- [#16678](https://github.com/influxdata/telegraf/pull/16678) `outputs.sql` Support secret for dsn
- [#16583](https://github.com/influxdata/telegraf/pull/16583) `outputs.stackdriver` Ensure quota is charged to configured project
- [#16717](https://github.com/influxdata/telegraf/pull/16717) `processors.defaults` Add support for specifying default tags
- [#16701](https://github.com/influxdata/telegraf/pull/16701) `processors.enum` Add multiple tag mapping
- [#16030](https://github.com/influxdata/telegraf/pull/16030) `processors.enum` Allow mapping to be applied to multiple fields
- [#16494](https://github.com/influxdata/telegraf/pull/16494) `serializer.prometheusremotewrite` Allow sending native histograms

### Bugfixes

- [#17044](https://github.com/influxdata/telegraf/pull/17044) `inputs.opcua` Fix integration test
- [#16986](https://github.com/influxdata/telegraf/pull/16986) `inputs.procstat` Resolve remote usernames on Posix systems
- [#16699](https://github.com/influxdata/telegraf/pull/16699) `inputs.win_wmi` Free resources to avoid leaks
- [#17118](https://github.com/influxdata/telegraf/pull/17118) `migrations` Update table content for general plugin migrations

### Dependency Updates

- [#17089](https://github.com/influxdata/telegraf/pull/17089) `deps` Bump cloud.google.com/go/bigquery from 1.68.0 to 1.69.0
- [#17026](https://github.com/influxdata/telegraf/pull/17026) `deps` Bump cloud.google.com/go/storage from 1.53.0 to 1.54.0
- [#17095](https://github.com/influxdata/telegraf/pull/17095) `deps` Bump cloud.google.com/go/storage from 1.54.0 to 1.55.0
- [#17034](https://github.com/influxdata/telegraf/pull/17034) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azidentity from 1.9.0 to 1.10.0
- [#17065](https://github.com/influxdata/telegraf/pull/17065) `deps` Bump github.com/ClickHouse/clickhouse-go/v2 from 2.34.0 to 2.35.0
- [#17145](https://github.com/influxdata/telegraf/pull/17145) `deps` Bump github.com/ClickHouse/clickhouse-go/v2 from 2.35.0 to 2.36.0
- [#17062](https://github.com/influxdata/telegraf/pull/17062) `deps` Bump github.com/IBM/nzgo/v12 from 12.0.9 to 12.0.10
- [#17083](https://github.com/influxdata/telegraf/pull/17083) `deps` Bump github.com/IBM/sarama from 1.45.1 to 1.45.2
- [#17040](https://github.com/influxdata/telegraf/pull/17040) `deps` Bump github.com/apache/inlong/inlong-sdk/dataproxy-sdk-twins/dataproxy-sdk-golang from 1.0.0 to 1.0.1
- [#17060](https://github.com/influxdata/telegraf/pull/17060) `deps` Bump github.com/apache/inlong/inlong-sdk/dataproxy-sdk-twins/dataproxy-sdk-golang from 1.0.1 to 1.0.2
- [#17127](https://github.com/influxdata/telegraf/pull/17127) `deps` Bump github.com/apache/inlong/inlong-sdk/dataproxy-sdk-twins/dataproxy-sdk-golang from 1.0.2 to 1.0.3
- [#17061](https://github.com/influxdata/telegraf/pull/17061) `deps` Bump github.com/apache/thrift from 0.21.0 to 0.22.0
- [#16954](https://github.com/influxdata/telegraf/pull/16954) `deps` Bump github.com/aws/aws-msk-iam-sasl-signer-go from 1.0.1 to 1.0.3
- [#17041](https://github.com/influxdata/telegraf/pull/17041) `deps` Bump github.com/aws/aws-msk-iam-sasl-signer-go from 1.0.3 to 1.0.4
- [#17128](https://github.com/influxdata/telegraf/pull/17128) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.29.14 to 1.29.15
- [#17129](https://github.com/influxdata/telegraf/pull/17129) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.17.67 to 1.17.68
- [#17057](https://github.com/influxdata/telegraf/pull/17057) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.44.3 to 1.45.0
- [#17132](https://github.com/influxdata/telegraf/pull/17132) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.45.0 to 1.45.1
- [#17029](https://github.com/influxdata/telegraf/pull/17029) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.49.0 to 1.50.0
- [#17131](https://github.com/influxdata/telegraf/pull/17131) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.50.0 to 1.50.1
- [#17143](https://github.com/influxdata/telegraf/pull/17143) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.43.1 to 1.43.2
- [#17037](https://github.com/influxdata/telegraf/pull/17037) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.218.0 to 1.219.0
- [#17067](https://github.com/influxdata/telegraf/pull/17067) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.220.0 to 1.222.0
- [#17093](https://github.com/influxdata/telegraf/pull/17093) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.222.0 to 1.224.0
- [#17136](https://github.com/influxdata/telegraf/pull/17136) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.224.0 to 1.225.0
- [#17139](https://github.com/influxdata/telegraf/pull/17139) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.35.0 to 1.35.1
- [#16996](https://github.com/influxdata/telegraf/pull/16996) `deps` Bump github.com/bluenviron/gomavlib/v3 from 3.1.0 to 3.2.1
- [#16987](https://github.com/influxdata/telegraf/pull/16987) `deps` Bump github.com/creack/goselect from 0.1.2 to 0.1.3
- [#17097](https://github.com/influxdata/telegraf/pull/17097) `deps` Bump github.com/docker/docker from 28.1.1+incompatible to 28.2.2+incompatible
- [#17133](https://github.com/influxdata/telegraf/pull/17133) `deps` Bump github.com/gosnmp/gosnmp from 1.40.0 to 1.41.0
- [#17126](https://github.com/influxdata/telegraf/pull/17126) `deps` Bump github.com/linkedin/goavro/v2 from 2.13.1 to 2.14.0
- [#17087](https://github.com/influxdata/telegraf/pull/17087) `deps` Bump github.com/lxc/incus/v6 from 6.12.0 to 6.13.0
- [#17085](https://github.com/influxdata/telegraf/pull/17085) `deps` Bump github.com/microsoft/go-mssqldb from 1.8.1 to 1.8.2
- [#17064](https://github.com/influxdata/telegraf/pull/17064) `deps` Bump github.com/nats-io/nats-server/v2 from 2.11.3 to 2.11.4
- [#17140](https://github.com/influxdata/telegraf/pull/17140) `deps` Bump github.com/nats-io/nats.go from 1.42.0 to 1.43.0
- [#17134](https://github.com/influxdata/telegraf/pull/17134) `deps` Bump github.com/netsampler/goflow2/v2 from 2.2.2 to 2.2.3
- [#17028](https://github.com/influxdata/telegraf/pull/17028) `deps` Bump github.com/prometheus/common from 0.63.0 to 0.64.0
- [#17066](https://github.com/influxdata/telegraf/pull/17066) `deps` Bump github.com/rclone/rclone from 1.69.2 to 1.69.3
- [#17096](https://github.com/influxdata/telegraf/pull/17096) `deps` Bump github.com/redis/go-redis/v9 from 9.8.0 to 9.9.0
- [#17088](https://github.com/influxdata/telegraf/pull/17088) `deps` Bump github.com/shirou/gopsutil/v4 from 4.25.4 to 4.25.5
- [#17135](https://github.com/influxdata/telegraf/pull/17135) `deps` Bump github.com/sijms/go-ora/v2 from 2.8.24 to 2.9.0
- [#17094](https://github.com/influxdata/telegraf/pull/17094) `deps` Bump github.com/snowflakedb/gosnowflake from 1.14.0 to 1.14.1
- [#17035](https://github.com/influxdata/telegraf/pull/17035) `deps` Bump github.com/tinylib/msgp from 1.2.5 to 1.3.0
- [#17054](https://github.com/influxdata/telegraf/pull/17054) `deps` Bump github.com/vmware/govmomi from 0.50.0 to 0.51.0
- [#17039](https://github.com/influxdata/telegraf/pull/17039) `deps` Bump github.com/yuin/goldmark from 1.7.11 to 1.7.12
- [#17130](https://github.com/influxdata/telegraf/pull/17130) `deps` Bump go.mongodb.org/mongo-driver from 1.17.3 to 1.17.4
- [#17056](https://github.com/influxdata/telegraf/pull/17056) `deps` Bump go.opentelemetry.io/collector/pdata from 1.31.0 to 1.33.0
- [#17058](https://github.com/influxdata/telegraf/pull/17058) `deps` Bump go.step.sm/crypto from 0.63.0 to 0.64.0
- [#17141](https://github.com/influxdata/telegraf/pull/17141) `deps` Bump golang.org/x/crypto from 0.38.0 to 0.39.0
- [#17144](https://github.com/influxdata/telegraf/pull/17144) `deps` Bump golang.org/x/mod from 0.24.0 to 0.25.0
- [#17033](https://github.com/influxdata/telegraf/pull/17033) `deps` Bump google.golang.org/api from 0.232.0 to 0.233.0
- [#17055](https://github.com/influxdata/telegraf/pull/17055) `deps` Bump google.golang.org/api from 0.233.0 to 0.234.0
- [#17086](https://github.com/influxdata/telegraf/pull/17086) `deps` Bump google.golang.org/api from 0.234.0 to 0.235.0
- [#17036](https://github.com/influxdata/telegraf/pull/17036) `deps` Bump google.golang.org/grpc from 1.72.0 to 1.72.1
- [#17059](https://github.com/influxdata/telegraf/pull/17059) `deps` Bump google.golang.org/grpc from 1.72.1 to 1.72.2
- [#17137](https://github.com/influxdata/telegraf/pull/17137) `deps` Bump google.golang.org/grpc from 1.72.2 to 1.73.0
- [#17031](https://github.com/influxdata/telegraf/pull/17031) `deps` Bump k8s.io/api from 0.33.0 to 0.33.1
- [#17038](https://github.com/influxdata/telegraf/pull/17038) `deps` Bump k8s.io/apimachinery from 0.33.0 to 0.33.1
- [#17030](https://github.com/influxdata/telegraf/pull/17030) `deps` Bump k8s.io/client-go from 0.33.0 to 0.33.1
- [#17025](https://github.com/influxdata/telegraf/pull/17025) `deps` Bump super-linter/super-linter from 7.3.0 to 7.4.0

## v1.34.4 [2025-05-19]

### Bugfixes

- [#17009](https://github.com/influxdata/telegraf/pull/17009) `inputs.cloudwatch` Restore filtering to match all dimensions
- [#16978](https://github.com/influxdata/telegraf/pull/16978) `inputs.nfsclient` Handle errors during mountpoint filtering
- [#17021](https://github.com/influxdata/telegraf/pull/17021) `inputs.opcua` Fix type mismatch in unit test
- [#16854](https://github.com/influxdata/telegraf/pull/16854) `inputs.opcua` Handle session invalidation between gather cycles
- [#16879](https://github.com/influxdata/telegraf/pull/16879) `inputs.tail` Prevent leaking file descriptors
- [#16815](https://github.com/influxdata/telegraf/pull/16815) `inputs.win_eventlog` Handle large events to avoid they get dropped silently
- [#16878](https://github.com/influxdata/telegraf/pull/16878) `parsers.json_v2` Handle measurements with multiple objects correctly

### Dependency Updates

- [#16991](https://github.com/influxdata/telegraf/pull/16991) `deps` Bump cloud.google.com/go/bigquery from 1.67.0 to 1.68.0
- [#16963](https://github.com/influxdata/telegraf/pull/16963) `deps` Bump cloud.google.com/go/storage from 1.52.0 to 1.53.0
- [#16955](https://github.com/influxdata/telegraf/pull/16955) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue from 1.0.0 to 1.0.1
- [#16989](https://github.com/influxdata/telegraf/pull/16989) `deps` Bump github.com/SAP/go-hdb from 1.13.5 to 1.13.6
- [#16998](https://github.com/influxdata/telegraf/pull/16998) `deps` Bump github.com/apache/arrow-go/v18 from 18.2.0 to 18.3.0
- [#16952](https://github.com/influxdata/telegraf/pull/16952) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.47.3 to 1.48.0
- [#16995](https://github.com/influxdata/telegraf/pull/16995) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.48.0 to 1.49.0
- [#16974](https://github.com/influxdata/telegraf/pull/16974) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.212.0 to 1.214.0
- [#16993](https://github.com/influxdata/telegraf/pull/16993) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.215.0 to 1.218.0
- [#16968](https://github.com/influxdata/telegraf/pull/16968) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.33.3 to 1.35.0
- [#16988](https://github.com/influxdata/telegraf/pull/16988) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.30.2 to 1.31.0
- [#17013](https://github.com/influxdata/telegraf/pull/17013) `deps` Bump github.com/ebitengine/purego from 0.8.2 to 0.8.3
- [#16972](https://github.com/influxdata/telegraf/pull/16972) `deps` Bump github.com/hashicorp/consul/api from 1.32.0 to 1.32.1
- [#16992](https://github.com/influxdata/telegraf/pull/16992) `deps` Bump github.com/microsoft/go-mssqldb from 1.8.0 to 1.8.1
- [#16990](https://github.com/influxdata/telegraf/pull/16990) `deps` Bump github.com/miekg/dns from 1.1.65 to 1.1.66
- [#16975](https://github.com/influxdata/telegraf/pull/16975) `deps` Bump github.com/nats-io/nats-server/v2 from 2.11.2 to 2.11.3
- [#16967](https://github.com/influxdata/telegraf/pull/16967) `deps` Bump github.com/nats-io/nats.go from 1.41.2 to 1.42.0
- [#16964](https://github.com/influxdata/telegraf/pull/16964) `deps` Bump github.com/rclone/rclone from 1.69.1 to 1.69.2
- [#16973](https://github.com/influxdata/telegraf/pull/16973) `deps` Bump github.com/redis/go-redis/v9 from 9.7.3 to 9.8.0
- [#16962](https://github.com/influxdata/telegraf/pull/16962) `deps` Bump github.com/shirou/gopsutil/v4 from 4.25.3 to 4.25.4
- [#16969](https://github.com/influxdata/telegraf/pull/16969) `deps` Bump github.com/snowflakedb/gosnowflake from 1.13.3 to 1.14.0
- [#16994](https://github.com/influxdata/telegraf/pull/16994) `deps` Bump github.com/vishvananda/netlink from 1.3.1-0.20250221194427-0af32151e72b to 1.3.1
- [#16958](https://github.com/influxdata/telegraf/pull/16958) `deps` Bump go.step.sm/crypto from 0.62.0 to 0.63.0
- [#16960](https://github.com/influxdata/telegraf/pull/16960) `deps` Bump golang.org/x/crypto from 0.37.0 to 0.38.0
- [#16966](https://github.com/influxdata/telegraf/pull/16966) `deps` Bump golang.org/x/net from 0.39.0 to 0.40.0
- [#16957](https://github.com/influxdata/telegraf/pull/16957) `deps` Bump google.golang.org/api from 0.230.0 to 0.231.0
- [#16853](https://github.com/influxdata/telegraf/pull/16853) `deps` Switch to maintained azure testcontainer module

## v1.34.3 [2025-05-05]

### Bugfixes

- [#16697](https://github.com/influxdata/telegraf/pull/16697) `agent` Correctly truncate the disk buffer
- [#16868](https://github.com/influxdata/telegraf/pull/16868) `common.ratelimiter` Only grow the buffer but never shrink
- [#16812](https://github.com/influxdata/telegraf/pull/16812) `inputs.cloudwatch` Handle metric includes/excludes correctly to prevent panic
- [#16911](https://github.com/influxdata/telegraf/pull/16911) `inputs.lustre2` Skip empty files
- [#16594](https://github.com/influxdata/telegraf/pull/16594) `inputs.opcua` Handle node array values
- [#16782](https://github.com/influxdata/telegraf/pull/16782) `inputs.win_wmi` Replace hard-coded class-name with correct config setting
- [#16781](https://github.com/influxdata/telegraf/pull/16781) `inputs.win_wmi` Restrict threading model to APARTMENTTHREADED
- [#16857](https://github.com/influxdata/telegraf/pull/16857) `outputs.quix` Allow empty certificate for new cloud managed instances

### Dependency Updates

- [#16804](https://github.com/influxdata/telegraf/pull/16804) `deps` Bump cloud.google.com/go/bigquery from 1.66.2 to 1.67.0
- [#16835](https://github.com/influxdata/telegraf/pull/16835) `deps` Bump cloud.google.com/go/monitoring from 1.24.0 to 1.24.2
- [#16785](https://github.com/influxdata/telegraf/pull/16785) `deps` Bump cloud.google.com/go/pubsub from 1.48.0 to 1.49.0
- [#16897](https://github.com/influxdata/telegraf/pull/16897) `deps` Bump cloud.google.com/go/storage from 1.51.0 to 1.52.0
- [#16840](https://github.com/influxdata/telegraf/pull/16840) `deps` Bump github.com/BurntSushi/toml from 1.4.0 to 1.5.0
- [#16838](https://github.com/influxdata/telegraf/pull/16838) `deps` Bump github.com/aliyun/alibaba-cloud-sdk-go from 1.63.104 to 1.63.106
- [#16908](https://github.com/influxdata/telegraf/pull/16908) `deps` Bump github.com/aliyun/alibaba-cloud-sdk-go from 1.63.106 to 1.63.107
- [#16789](https://github.com/influxdata/telegraf/pull/16789) `deps` Bump github.com/antchfx/xpath from 1.3.3 to 1.3.4
- [#16807](https://github.com/influxdata/telegraf/pull/16807) `deps` Bump github.com/apache/arrow-go/v18 from 18.1.0 to 18.2.0
- [#16844](https://github.com/influxdata/telegraf/pull/16844) `deps` Bump github.com/apache/iotdb-client-go from 1.3.3 to 1.3.4
- [#16839](https://github.com/influxdata/telegraf/pull/16839) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.44.1 to 1.44.3
- [#16836](https://github.com/influxdata/telegraf/pull/16836) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.45.3 to 1.47.3
- [#16846](https://github.com/influxdata/telegraf/pull/16846) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.42.2 to 1.42.4
- [#16905](https://github.com/influxdata/telegraf/pull/16905) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.42.4 to 1.43.1
- [#16842](https://github.com/influxdata/telegraf/pull/16842) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.210.1 to 1.211.3
- [#16900](https://github.com/influxdata/telegraf/pull/16900) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.211.3 to 1.212.0
- [#16903](https://github.com/influxdata/telegraf/pull/16903) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.33.2 to 1.33.3
- [#16793](https://github.com/influxdata/telegraf/pull/16793) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.27.4 to 1.30.2
- [#16802](https://github.com/influxdata/telegraf/pull/16802) `deps` Bump github.com/clarify/clarify-go from 0.3.1 to 0.4.0
- [#16849](https://github.com/influxdata/telegraf/pull/16849) `deps` Bump github.com/docker/docker from 28.0.4+incompatible to 28.1.1+incompatible
- [#16830](https://github.com/influxdata/telegraf/pull/16830) `deps` Bump github.com/go-ldap/ldap/v3 from 3.4.10 to 3.4.11
- [#16801](https://github.com/influxdata/telegraf/pull/16801) `deps` Bump github.com/go-sql-driver/mysql from 1.8.1 to 1.9.2
- [#16806](https://github.com/influxdata/telegraf/pull/16806) `deps` Bump github.com/gofrs/uuid/v5 from 5.3.0 to 5.3.2
- [#16895](https://github.com/influxdata/telegraf/pull/16895) `deps` Bump github.com/google/cel-go from 0.24.1 to 0.25.0
- [#16797](https://github.com/influxdata/telegraf/pull/16797) `deps` Bump github.com/gopcua/opcua from 0.7.1 to 0.7.4
- [#16894](https://github.com/influxdata/telegraf/pull/16894) `deps` Bump github.com/gopcua/opcua from 0.7.4 to 0.8.0
- [#16660](https://github.com/influxdata/telegraf/pull/16660) `deps` Bump github.com/gosmnp/gosnmp from 1.39.0 to 1.40.0
- [#16902](https://github.com/influxdata/telegraf/pull/16902) `deps` Bump github.com/gosnmp/gosnmp from 1.39.0 to 1.40.0
- [#16841](https://github.com/influxdata/telegraf/pull/16841) `deps` Bump github.com/hashicorp/consul/api from 1.31.2 to 1.32.0
- [#16891](https://github.com/influxdata/telegraf/pull/16891) `deps` Bump github.com/jedib0t/go-pretty/v6 from 6.6.5 to 6.6.7
- [#16892](https://github.com/influxdata/telegraf/pull/16892) `deps` Bump github.com/lxc/incus/v6 from 6.11.0 to 6.12.0
- [#16786](https://github.com/influxdata/telegraf/pull/16786) `deps` Bump github.com/microsoft/go-mssqldb from 1.7.2 to 1.8.0
- [#16851](https://github.com/influxdata/telegraf/pull/16851) `deps` Bump github.com/miekg/dns from 1.1.64 to 1.1.65
- [#16808](https://github.com/influxdata/telegraf/pull/16808) `deps` Bump github.com/nats-io/nats-server/v2 from 2.10.25 to 2.10.27
- [#16888](https://github.com/influxdata/telegraf/pull/16888) `deps` Bump github.com/nats-io/nats-server/v2 from 2.10.27 to 2.11.2
- [#16909](https://github.com/influxdata/telegraf/pull/16909) `deps` Bump github.com/nats-io/nats.go from 1.41.1 to 1.41.2
- [#16790](https://github.com/influxdata/telegraf/pull/16790) `deps` Bump github.com/openconfig/gnmi from 0.11.0 to 0.14.1
- [#16799](https://github.com/influxdata/telegraf/pull/16799) `deps` Bump github.com/openconfig/goyang from 1.6.0 to 1.6.2
- [#16848](https://github.com/influxdata/telegraf/pull/16848) `deps` Bump github.com/prometheus-community/pro-bing from 0.4.1 to 0.7.0
- [#16795](https://github.com/influxdata/telegraf/pull/16795) `deps` Bump github.com/prometheus/client_golang from 1.21.1 to 1.22.0
- [#16845](https://github.com/influxdata/telegraf/pull/16845) `deps` Bump github.com/prometheus/client_model from 0.6.1 to 0.6.2
- [#16901](https://github.com/influxdata/telegraf/pull/16901) `deps` Bump github.com/prometheus/procfs from 0.16.0 to 0.16.1
- [#16792](https://github.com/influxdata/telegraf/pull/16792) `deps` Bump github.com/safchain/ethtool from 0.3.0 to 0.5.10
- [#16791](https://github.com/influxdata/telegraf/pull/16791) `deps` Bump github.com/seancfoley/ipaddress-go from 1.7.0 to 1.7.1
- [#16794](https://github.com/influxdata/telegraf/pull/16794) `deps` Bump github.com/shirou/gopsutil/v4 from 4.25.1 to 4.25.3
- [#16828](https://github.com/influxdata/telegraf/pull/16828) `deps` Bump github.com/snowflakedb/gosnowflake from 1.11.2 to 1.13.1
- [#16904](https://github.com/influxdata/telegraf/pull/16904) `deps` Bump github.com/snowflakedb/gosnowflake from 1.13.1 to 1.13.3
- [#16787](https://github.com/influxdata/telegraf/pull/16787) `deps` Bump github.com/srebhan/cborquery from 1.0.3 to 1.0.4
- [#16837](https://github.com/influxdata/telegraf/pull/16837) `deps` Bump github.com/srebhan/protobufquery from 1.0.1 to 1.0.4
- [#16893](https://github.com/influxdata/telegraf/pull/16893) `deps` Bump github.com/testcontainers/testcontainers-go from 0.36.0 to 0.37.0
- [#16803](https://github.com/influxdata/telegraf/pull/16803) `deps` Bump github.com/testcontainers/testcontainers-go/modules/kafka from 0.34.0 to 0.36.0
- [#16890](https://github.com/influxdata/telegraf/pull/16890) `deps` Bump github.com/testcontainers/testcontainers-go/modules/kafka from 0.36.0 to 0.37.0
- [#16850](https://github.com/influxdata/telegraf/pull/16850) `deps` Bump github.com/vmware/govmomi from 0.49.0 to 0.50.0
- [#16784](https://github.com/influxdata/telegraf/pull/16784) `deps` Bump github.com/yuin/goldmark from 1.7.8 to 1.7.9
- [#16896](https://github.com/influxdata/telegraf/pull/16896) `deps` Bump github.com/yuin/goldmark from 1.7.9 to 1.7.11
- [#16832](https://github.com/influxdata/telegraf/pull/16832) `deps` Bump go.mongodb.org/mongo-driver from 1.17.0 to 1.17.3
- [#16800](https://github.com/influxdata/telegraf/pull/16800) `deps` Bump go.opentelemetry.io/collector/pdata from 1.29.0 to 1.30.0
- [#16907](https://github.com/influxdata/telegraf/pull/16907) `deps` Bump go.opentelemetry.io/collector/pdata from 1.30.0 to 1.31.0
- [#16831](https://github.com/influxdata/telegraf/pull/16831) `deps` Bump go.step.sm/crypto from 0.60.0 to 0.61.0
- [#16886](https://github.com/influxdata/telegraf/pull/16886) `deps` Bump go.step.sm/crypto from 0.61.0 to 0.62.0
- [#16816](https://github.com/influxdata/telegraf/pull/16816) `deps` Bump golangci-lint from v2.0.2 to v2.1.2
- [#16852](https://github.com/influxdata/telegraf/pull/16852) `deps` Bump gonum.org/v1/gonum from 0.15.1 to 0.16.0
- [#16805](https://github.com/influxdata/telegraf/pull/16805) `deps` Bump google.golang.org/api from 0.228.0 to 0.229.0
- [#16898](https://github.com/influxdata/telegraf/pull/16898) `deps` Bump google.golang.org/api from 0.229.0 to 0.230.0
- [#16834](https://github.com/influxdata/telegraf/pull/16834) `deps` Bump google.golang.org/grpc from 1.71.1 to 1.72.0
- [#16889](https://github.com/influxdata/telegraf/pull/16889) `deps` Bump k8s.io/client-go from 0.32.3 to 0.33.0
- [#16843](https://github.com/influxdata/telegraf/pull/16843) `deps` Bump modernc.org/sqlite from 1.36.2 to 1.37.0

## v1.34.2 [2025-04-14]

### Bugfixes

- [#16375](https://github.com/influxdata/telegraf/pull/16375) `aggregators` Handle time drift when calculating aggregation windows

### Dependency Updates

- [#16689](https://github.com/influxdata/telegraf/pull/16689) `deps` Bump cloud.google.com/go/pubsub from 1.45.3 to 1.48.0
- [#16769](https://github.com/influxdata/telegraf/pull/16769) `deps` Bump cloud.google.com/go/storage from 1.50.0 to 1.51.0
- [#16771](https://github.com/influxdata/telegraf/pull/16771) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azcore from 1.17.0 to 1.18.0
- [#16708](https://github.com/influxdata/telegraf/pull/16708) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs from 1.2.3 to 1.3.1
- [#16764](https://github.com/influxdata/telegraf/pull/16764) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs from 1.3.1 to 1.3.2
- [#16777](https://github.com/influxdata/telegraf/pull/16777) `deps` Bump github.com/ClickHouse/clickhouse-go/v2 from 2.30.3 to 2.34.0
- [#16707](https://github.com/influxdata/telegraf/pull/16707) `deps` Bump github.com/IBM/sarama from v1.43.3 to v1.45.1
- [#16739](https://github.com/influxdata/telegraf/pull/16739) `deps` Bump github.com/SAP/go-hdb from 1.9.10 to 1.13.5
- [#16754](https://github.com/influxdata/telegraf/pull/16754) `deps` Bump github.com/aliyun/alibaba-cloud-sdk-go from 1.62.721 to 1.63.104
- [#16767](https://github.com/influxdata/telegraf/pull/16767) `deps` Bump github.com/antchfx/jsonquery from 1.3.3 to 1.3.6
- [#16758](https://github.com/influxdata/telegraf/pull/16758) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.29.6 to 1.29.13
- [#16710](https://github.com/influxdata/telegraf/pull/16710) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.17.59 to 1.17.65
- [#16685](https://github.com/influxdata/telegraf/pull/16685) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.43.14 to 1.44.1
- [#16773](https://github.com/influxdata/telegraf/pull/16773) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.40.0 to 1.42.2
- [#16688](https://github.com/influxdata/telegraf/pull/16688) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.203.1 to 1.210.1
- [#16772](https://github.com/influxdata/telegraf/pull/16772) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.32.6 to 1.33.2
- [#16711](https://github.com/influxdata/telegraf/pull/16711) `deps` Bump github.com/cloudevents/sdk-go/v2 from 2.15.2 to 2.16.0
- [#16687](https://github.com/influxdata/telegraf/pull/16687) `deps` Bump github.com/google/cel-go from 0.23.0 to 0.24.1
- [#16712](https://github.com/influxdata/telegraf/pull/16712) `deps` Bump github.com/gophercloud/gophercloud/v2 from 2.0.0-rc.3 to 2.6.0
- [#16738](https://github.com/influxdata/telegraf/pull/16738) `deps` Bump github.com/gorcon/rcon from 1.3.5 to 1.4.0
- [#16737](https://github.com/influxdata/telegraf/pull/16737) `deps` Bump github.com/gosnmp/gosnmp from 1.38.0 to 1.39.0
- [#16752](https://github.com/influxdata/telegraf/pull/16752) `deps` Bump github.com/lxc/incus/v6 from 6.9.0 to 6.11.0
- [#16761](https://github.com/influxdata/telegraf/pull/16761) `deps` Bump github.com/nats-io/nats.go from 1.39.1 to 1.41.1
- [#16753](https://github.com/influxdata/telegraf/pull/16753) `deps` Bump github.com/netsampler/goflow2/v2 from 2.2.1 to 2.2.2
- [#16760](https://github.com/influxdata/telegraf/pull/16760) `deps` Bump github.com/p4lang/p4runtime from 1.4.0 to 1.4.1
- [#16766](https://github.com/influxdata/telegraf/pull/16766) `deps` Bump github.com/prometheus/common from 0.62.0 to 0.63.0
- [#16686](https://github.com/influxdata/telegraf/pull/16686) `deps` Bump github.com/rclone/rclone from 1.68.2 to 1.69.1
- [#16770](https://github.com/influxdata/telegraf/pull/16770) `deps` Bump github.com/sijms/go-ora/v2 from 2.8.22 to 2.8.24
- [#16709](https://github.com/influxdata/telegraf/pull/16709) `deps` Bump github.com/testcontainers/testcontainers-go from 0.35.0 to 0.36.0
- [#16763](https://github.com/influxdata/telegraf/pull/16763) `deps` Bump github.com/tinylib/msgp from 1.2.0 to 1.2.5
- [#16757](https://github.com/influxdata/telegraf/pull/16757) `deps` Bump github.com/urfave/cli/v2 from 2.27.2 to 2.27.6
- [#16724](https://github.com/influxdata/telegraf/pull/16724) `deps` Bump github.com/vmware/govmomi from v0.45.1 to v0.49.0
- [#16768](https://github.com/influxdata/telegraf/pull/16768) `deps` Bump go.opentelemetry.io/collector/pdata from 1.25.0 to 1.29.0
- [#16765](https://github.com/influxdata/telegraf/pull/16765) `deps` Bump go.step.sm/crypto from 0.59.1 to 0.60.0
- [#16756](https://github.com/influxdata/telegraf/pull/16756) `deps` Bump golang.org/x/crypto from 0.36.0 to 0.37.0
- [#16683](https://github.com/influxdata/telegraf/pull/16683) `deps` Bump golangci-lint from v1.64.5 to v2.0.2
- [#16759](https://github.com/influxdata/telegraf/pull/16759) `deps` Bump google.golang.org/api from 0.224.0 to 0.228.0
- [#16755](https://github.com/influxdata/telegraf/pull/16755) `deps` Bump k8s.io/client-go from 0.32.1 to 0.32.3
- [#16684](https://github.com/influxdata/telegraf/pull/16684) `deps` Bump tj-actions/changed-files from 46.0.1 to 46.0.3
- [#16736](https://github.com/influxdata/telegraf/pull/16736) `deps` Bump tj-actions/changed-files from 46.0.3 to 46.0.4
- [#16751](https://github.com/influxdata/telegraf/pull/16751) `deps` Bump tj-actions/changed-files from 46.0.4 to 46.0.5

## v1.34.1 [2025-03-24]

### Bugfixes

- [#16638](https://github.com/influxdata/telegraf/pull/16638) `agent` Condense plugin source information table when multiple plugins in same file
- [#16674](https://github.com/influxdata/telegraf/pull/16674) `inputs.tail` Do not seek on pipes
- [#16643](https://github.com/influxdata/telegraf/pull/16643) `inputs.tail` Use correct initial_read_offset persistent offset naming in the code
- [#16628](https://github.com/influxdata/telegraf/pull/16628) `outputs.influxdb_v2` Use dynamic token secret
- [#16625](https://github.com/influxdata/telegraf/pull/16625) `outputs.sql` Allow to disable timestamp column
- [#16682](https://github.com/influxdata/telegraf/pull/16682) `secrets` Make 'insufficient lockable memory' warning work on BSDs

### Dependency Updates

- [#16612](https://github.com/influxdata/telegraf/pull/16612) `deps` Bump github.com/PaesslerAG/gval from 1.2.2 to 1.2.4
- [#16650](https://github.com/influxdata/telegraf/pull/16650) `deps` Bump github.com/aws/smithy-go from 1.22.2 to 1.22.3
- [#16680](https://github.com/influxdata/telegraf/pull/16680) `deps` Bump github.com/golang-jwt/jwt/v4 from 4.5.1 to 4.5.2
- [#16679](https://github.com/influxdata/telegraf/pull/16679) `deps` Bump github.com/golang-jwt/jwt/v5 from 5.2.1 to 5.2.2
- [#16610](https://github.com/influxdata/telegraf/pull/16610) `deps` Bump github.com/golang/snappy from 0.0.4 to 1.0.0
- [#16652](https://github.com/influxdata/telegraf/pull/16652) `deps` Bump github.com/hashicorp/consul/api from 1.29.2 to 1.31.2
- [#16651](https://github.com/influxdata/telegraf/pull/16651) `deps` Bump github.com/leodido/go-syslog/v4 from 4.1.0 to 4.2.0
- [#16613](https://github.com/influxdata/telegraf/pull/16613) `deps` Bump github.com/linkedin/goavro/v2 from 2.13.0 to 2.13.1
- [#16671](https://github.com/influxdata/telegraf/pull/16671) `deps` Bump github.com/redis/go-redis/v9 from 9.7.0 to 9.7.3
- [#16611](https://github.com/influxdata/telegraf/pull/16611) `deps` Bump go.step.sm/crypto from 0.54.0 to 0.59.1
- [#16640](https://github.com/influxdata/telegraf/pull/16640) `deps` Bump golang.org/x/crypto from 0.35.0 to 0.36.0
- [#16620](https://github.com/influxdata/telegraf/pull/16620) `deps` Bump golang.org/x/net from 0.35.0 to 0.36.0
- [#16639](https://github.com/influxdata/telegraf/pull/16639) `deps` Bump golang.org/x/oauth2 from 0.26.0 to 0.28.0
- [#16653](https://github.com/influxdata/telegraf/pull/16653) `deps` Bump k8s.io/api from 0.32.1 to 0.32.3
- [#16659](https://github.com/influxdata/telegraf/pull/16659) `deps` Bump tj-actions/changed-files from v45 to v46.0.1

## v1.34.0 [2025-03-10]

### New Plugins

- [#15988](https://github.com/influxdata/telegraf/pull/15988) `inputs.firehose` Add new plugin
- [#16352](https://github.com/influxdata/telegraf/pull/16352) `inputs.huebridge` Add plugin
- [#16392](https://github.com/influxdata/telegraf/pull/16392) `inputs.nsdp` Add plugin

### Features

- [#16333](https://github.com/influxdata/telegraf/pull/16333) `agent` Add support for input probing
- [#16270](https://github.com/influxdata/telegraf/pull/16270) `agent` Print plugins source information
- [#16474](https://github.com/influxdata/telegraf/pull/16474) `inputs.cgroup` Support more cgroup v2 formats
- [#16337](https://github.com/influxdata/telegraf/pull/16337) `inputs.cloudwatch` Allow wildcards for namespaces
- [#16292](https://github.com/influxdata/telegraf/pull/16292) `inputs.docker` Support swarm jobs
- [#16501](https://github.com/influxdata/telegraf/pull/16501) `inputs.exec` Allow to get untruncated errors in debug mode
- [#16480](https://github.com/influxdata/telegraf/pull/16480) `inputs.gnmi` Add support for `depth` extension
- [#16336](https://github.com/influxdata/telegraf/pull/16336) `inputs.infiniband` Add support for RDMA counters
- [#16124](https://github.com/influxdata/telegraf/pull/16124) `inputs.ipset` Add metric for number of entries and individual IPs
- [#16579](https://github.com/influxdata/telegraf/pull/16579) `inputs.nvidia_smi` Add new power-draw fields for v12 scheme
- [#16305](https://github.com/influxdata/telegraf/pull/16305) `inputs.nvidia_smi` Implement probing
- [#16105](https://github.com/influxdata/telegraf/pull/16105) `inputs.procstat` Add child level tag
- [#16066](https://github.com/influxdata/telegraf/pull/16066) `inputs.proxmox` Allow to add VM-id and status as tag
- [#16287](https://github.com/influxdata/telegraf/pull/16287) `inputs.systemd_units` Add active_enter_timestamp_us field
- [#16342](https://github.com/influxdata/telegraf/pull/16342) `inputs.tail` Add `initial_read_offset` config for controlling read behavior
- [#16355](https://github.com/influxdata/telegraf/pull/16355) `inputs.webhooks` Add support for GitHub workflow events
- [#16508](https://github.com/influxdata/telegraf/pull/16508) `inputs.x509_cert` Add support for JKS and PKCS#12 keystores
- [#16491](https://github.com/influxdata/telegraf/pull/16491) `outputs.mqtt` Add sprig for topic name generator for homie layout
- [#16570](https://github.com/influxdata/telegraf/pull/16570) `outputs.nats` Use Jetstream publisher when using Jetstream
- [#16566](https://github.com/influxdata/telegraf/pull/16566) `outputs.prometheus_client` Allow adding custom headers
- [#16272](https://github.com/influxdata/telegraf/pull/16272) `parsers.avro` Allow union fields to be specified as tags
- [#16493](https://github.com/influxdata/telegraf/pull/16493) `parsers.prometheusremotewrite` Add dense metric version to better support histograms
- [#16214](https://github.com/influxdata/telegraf/pull/16214) `processors.converter` Add support for base64 encoded IEEE floats
- [#16497](https://github.com/influxdata/telegraf/pull/16497) `processors.template` Add sprig function for templates

### Bugfixes

- [#16542](https://github.com/influxdata/telegraf/pull/16542) `inputs.gnmi` Handle path elements without name but with keys correctly
- [#16606](https://github.com/influxdata/telegraf/pull/16606) `inputs.huebridge` Cleanup and fix linter issues
- [#16580](https://github.com/influxdata/telegraf/pull/16580) `inputs.net` Skip checks in containerized environments
- [#16555](https://github.com/influxdata/telegraf/pull/16555) `outputs.opensearch` Use correct pipeline name while creating bulk-indexers
- [#16557](https://github.com/influxdata/telegraf/pull/16557) `serializers.prometheus` Use legacy validation for metric name

### Dependency Updates

- [#16576](https://github.com/influxdata/telegraf/pull/16576) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azidentity from 1.8.1 to 1.8.2
- [#16553](https://github.com/influxdata/telegraf/pull/16553) `deps` Bump github.com/Azure/go-autorest/autorest from 0.11.29 to 0.11.30
- [#16552](https://github.com/influxdata/telegraf/pull/16552) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.198.1 to 1.203.1
- [#16554](https://github.com/influxdata/telegraf/pull/16554) `deps` Bump github.com/go-jose/go-jose/v4 from 4.0.4 to 4.0.5
- [#16574](https://github.com/influxdata/telegraf/pull/16574) `deps` Bump github.com/gopcua/opcua from 0.5.3 to 0.7.1
- [#16551](https://github.com/influxdata/telegraf/pull/16551) `deps` Bump github.com/nats-io/nats.go from 1.39.0 to 1.39.1
- [#16575](https://github.com/influxdata/telegraf/pull/16575) `deps` Bump github.com/tidwall/wal from 1.1.7 to 1.1.8
- [#16578](https://github.com/influxdata/telegraf/pull/16578) `deps` Bump super-linter/super-linter from 7.2.1 to 7.3.0

## v1.33.3 [2025-02-25]

### Important Changes

- PR [#16507](https://github.com/influxdata/telegraf/pull/16507) adds the
  `enforce_first_namespace_as_origin` to the GNMI input plugin. This option
  allows to disable mangling of the response `path` tag by _not_ using namespaces
  as origin. It is highly recommended to disable the option.
  However, disabling the behavior might change the `path` tag and
  thus might break existing queries. Furthermore, the tag modification might
  increase cardinality in your database.

### Bugfixes

- [#16546](https://github.com/influxdata/telegraf/pull/16546) `agent` Add authorization and user-agent when watching remote configs
- [#16507](https://github.com/influxdata/telegraf/pull/16507) `inputs.gnmi` Allow to disable using first namespace as origin
- [#16511](https://github.com/influxdata/telegraf/pull/16511) `inputs.proxmox` Allow search domain to be empty
- [#16530](https://github.com/influxdata/telegraf/pull/16530) `internal` Fix plural acronyms in SnakeCase function
- [#16539](https://github.com/influxdata/telegraf/pull/16539) `logging` Handle closing correctly and fix tests
- [#16535](https://github.com/influxdata/telegraf/pull/16535) `processors.execd` Detect line-protocol parser correctly

### Dependency Updates

- [#16506](https://github.com/influxdata/telegraf/pull/16506) `deps` Bump github.com/ClickHouse/clickhouse-go/v2 from 2.30.1 to 2.30.3
- [#16502](https://github.com/influxdata/telegraf/pull/16502) `deps` Bump github.com/antchfx/xmlquery from 1.4.1 to 1.4.4
- [#16519](https://github.com/influxdata/telegraf/pull/16519) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.43.1 to 1.43.14
- [#16503](https://github.com/influxdata/telegraf/pull/16503) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.36.2 to 1.40.0
- [#16522](https://github.com/influxdata/telegraf/pull/16522) `deps` Bump github.com/nats-io/nats.go from 1.37.0 to 1.39.0
- [#16505](https://github.com/influxdata/telegraf/pull/16505) `deps` Bump github.com/srebhan/cborquery from 1.0.1 to 1.0.3
- [#16534](https://github.com/influxdata/telegraf/pull/16534) `deps` Bump github.com/vishvananda/netlink from 1.3.0 to 1.3.1-0.20250221194427-0af32151e72b
- [#16521](https://github.com/influxdata/telegraf/pull/16521) `deps` Bump go.opentelemetry.io/collector/pdata from 1.12.0 to 1.25.0
- [#16504](https://github.com/influxdata/telegraf/pull/16504) `deps` Bump golang.org/x/net from 0.34.0 to 0.35.0
- [#16512](https://github.com/influxdata/telegraf/pull/16512) `deps` Bump golangci-lint from v1.63.4 to v1.64.5

## v1.33.2 [2025-02-10]

### Important Changes

- PR [#16423](https://github.com/influxdata/telegraf/pull/16423) converts the ClickHouse drivers to the v2 version.
  This new version also requires a
  [new format for the DSN](https://github.com/ClickHouse/clickhouse-go/tree/v2.30.2?tab=readme-ov-file#dsn). The plugin
  tries its best to convert the old DSN to the new format but might not be able to do so. Please check for warnings in
  your log file and convert to the new format as soon as possible.
- PR [#16403](https://github.com/influxdata/telegraf/pull/16403) ensures consistency of the NetFlow plugin's
  `ip_version` field type by enforcing "IPv4", "IPv6", or "unknown" string values. Previously the `ip_version` could
  become an (unsigned) integer when parsing raw-packets' headers especially with SFlow v5 input. Please watch
  out for type-conflicts on the output side!

### Bugfixes

- [#16477](https://github.com/influxdata/telegraf/pull/16477) `agent` Avoid panic by checking for skip_processors_after_aggregators
- [#16489](https://github.com/influxdata/telegraf/pull/16489) `agent` Set `godebug x509negativeserial=1` as a workaround
- [#16403](https://github.com/influxdata/telegraf/pull/16403) `inputs.netflow` Ensure type consistency for sFlow&#39;s IP version field
- [#16447](https://github.com/influxdata/telegraf/pull/16447) `inputs.x509_cert` Add config to left-pad serial number to 128-bits
- [#16448](https://github.com/influxdata/telegraf/pull/16448) `outputs.azure_monitor` Prevent infinite send loop for outdated metrics
- [#16472](https://github.com/influxdata/telegraf/pull/16472) `outputs.sql` Fix insert into ClickHouse
- [#16454](https://github.com/influxdata/telegraf/pull/16454) `service` Set address to prevent orphaned dbus-session processes

### Dependency Updates

- [#16442](https://github.com/influxdata/telegraf/pull/16442) `deps` Bump cloud.google.com/go/storage from 1.47.0 to 1.50.0
- [#16414](https://github.com/influxdata/telegraf/pull/16414) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azidentity from 1.7.0 to 1.8.1
- [#16416](https://github.com/influxdata/telegraf/pull/16416) `deps` Bump github.com/apache/iotdb-client-go from 1.3.2 to 1.3.3
- [#16415](https://github.com/influxdata/telegraf/pull/16415) `deps` Bump github.com/aws/aws-sdk-go-v2 from 1.32.8 to 1.33.0
- [#16394](https://github.com/influxdata/telegraf/pull/16394) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.38.0 to 1.45.3
- [#16468](https://github.com/influxdata/telegraf/pull/16468) `deps` Bump github.com/aws/aws-sdk-go-v2/service/sts from 1.33.10 to 1.33.12
- [#16439](https://github.com/influxdata/telegraf/pull/16439) `deps` Bump github.com/aws/aws-sdk-go-v2/service/sts from 1.33.2 to 1.33.10
- [#16395](https://github.com/influxdata/telegraf/pull/16395) `deps` Bump github.com/eclipse/paho.golang from 0.21.0 to 0.22.0
- [#16470](https://github.com/influxdata/telegraf/pull/16470) `deps` Bump github.com/go-ldap/ldap/v3 from 3.4.8 to 3.4.10
- [#16440](https://github.com/influxdata/telegraf/pull/16440) `deps` Bump github.com/google/cel-go from 0.21.0 to 0.23.0
- [#16445](https://github.com/influxdata/telegraf/pull/16445) `deps` Bump github.com/lxc/incus/v6 from 6.6.0 to 6.9.0
- [#16466](https://github.com/influxdata/telegraf/pull/16466) `deps` Bump github.com/nats-io/nats-server/v2 from 2.10.17 to 2.10.25
- [#16453](https://github.com/influxdata/telegraf/pull/16453) `deps` Bump github.com/prometheus/common from 0.61.0 to 0.62.0
- [#16417](https://github.com/influxdata/telegraf/pull/16417) `deps` Bump github.com/shirou/gopsutil/v4 from 4.24.10 to 4.24.12
- [#16369](https://github.com/influxdata/telegraf/pull/16369) `deps` Bump github.com/shirou/gopsutil/v4 from v4.24.10 to v4.24.12
- [#16397](https://github.com/influxdata/telegraf/pull/16397) `deps` Bump github.com/showwin/speedtest-go from 1.7.9 to 1.7.10
- [#16467](https://github.com/influxdata/telegraf/pull/16467) `deps` Bump github.com/yuin/goldmark from 1.6.0 to 1.7.8
- [#16360](https://github.com/influxdata/telegraf/pull/16360) `deps` Bump golangci-lint from v1.62.2 to v1.63.4
- [#16469](https://github.com/influxdata/telegraf/pull/16469) `deps` Bump google.golang.org/api from 0.214.0 to 0.219.0
- [#16396](https://github.com/influxdata/telegraf/pull/16396) `deps` Bump k8s.io/api from 0.31.3 to 0.32.1
- [#16482](https://github.com/influxdata/telegraf/pull/16482) `deps` Update Apache arrow from 0.0-20240716144821-cf5d7c7ec3cf to 18.1.0
- [#16423](https://github.com/influxdata/telegraf/pull/16423) `deps` Update ClickHouse SQL driver from 1.5.4 to to 2.30.1

## v1.33.1 [2025-01-10]

### Important Changes

- The default value of `skip_processors_after_aggregators` will change to `true`
  with Telegraf `v1.40.0`, skip running the processors again after aggregators!
  If you need the current default behavior, please explicitly set the option to
  `false`! To silence the warning and use the future default behavior, please
  explicitly set the option to `true`.

### Bugfixes

- [#16290](https://github.com/influxdata/telegraf/pull/16290) `agent` Skip initialization of second processor state if requested
- [#16377](https://github.com/influxdata/telegraf/pull/16377) `inputs.intel_powerstat` Fix option removal version
- [#16310](https://github.com/influxdata/telegraf/pull/16310) `inputs.mongodb` Do not dereference nil pointer if gathering database stats fails
- [#16383](https://github.com/influxdata/telegraf/pull/16383) `outputs.influxdb_v2` Allow overriding auth and agent headers
- [#16388](https://github.com/influxdata/telegraf/pull/16388) `outputs.influxdb_v2` Fix panic and API error handling
- [#16289](https://github.com/influxdata/telegraf/pull/16289) `outputs.remotefile` Handle tracking metrics correctly

### Dependency Updates

- [#16344](https://github.com/influxdata/telegraf/pull/16344) `deps` Bump cloud.google.com/go/bigquery from 1.64.0 to 1.65.0
- [#16283](https://github.com/influxdata/telegraf/pull/16283) `deps` Bump cloud.google.com/go/monitoring from 1.21.1 to 1.22.0
- [#16315](https://github.com/influxdata/telegraf/pull/16315) `deps` Bump github.com/Azure/go-autorest/autorest/adal from 0.9.23 to 0.9.24
- [#16319](https://github.com/influxdata/telegraf/pull/16319) `deps` Bump github.com/IBM/nzgo/v12 from 12.0.9-0.20231115043259-49c27f2dfe48 to 12.0.9
- [#16346](https://github.com/influxdata/telegraf/pull/16346) `deps` Bump github.com/Masterminds/semver/v3 from 3.3.0 to 3.3.1
- [#16280](https://github.com/influxdata/telegraf/pull/16280) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.27.39 to 1.28.6
- [#16343](https://github.com/influxdata/telegraf/pull/16343) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.162.1 to 1.198.1
- [#16317](https://github.com/influxdata/telegraf/pull/16317) `deps` Bump github.com/fatih/color from 1.17.0 to 1.18.0
- [#16345](https://github.com/influxdata/telegraf/pull/16345) `deps` Bump github.com/gopacket/gopacket from 1.3.0 to 1.3.1
- [#16282](https://github.com/influxdata/telegraf/pull/16282) `deps` Bump github.com/nats-io/nats.go from 1.36.0 to 1.37.0
- [#16318](https://github.com/influxdata/telegraf/pull/16318) `deps` Bump github.com/prometheus/common from 0.60.0 to 0.61.0
- [#16324](https://github.com/influxdata/telegraf/pull/16324) `deps` Bump github.com/vapourismo/knx-go from v0.0.0-20240217175130-922a0d50c241 to v0.0.0-20240915133544-a6ab43471c11
- [#16297](https://github.com/influxdata/telegraf/pull/16297) `deps` Bump golang.org/x/crypto from 0.29.0 to 0.31.0
- [#16281](https://github.com/influxdata/telegraf/pull/16281) `deps` Bump k8s.io/client-go from 0.30.1 to 0.31.3
- [#16313](https://github.com/influxdata/telegraf/pull/16313) `deps` Bump super-linter/super-linter from 7.2.0 to 7.2.1

## v1.33.0 [2024-12-09]

### New Plugins

- [#15754](https://github.com/influxdata/telegraf/pull/15754) `inputs.neoom_beaam` Add new plugin
- [#15869](https://github.com/influxdata/telegraf/pull/15869) `processors.batch` Add batch processor
- [#16144](https://github.com/influxdata/telegraf/pull/16144) `outputs.quix` Add plugin

### Features

- [#16010](https://github.com/influxdata/telegraf/pull/16010) `agent` Add --watch-interval option for polling config changes
- [#15948](https://github.com/influxdata/telegraf/pull/15948) `aggregators.basicstats` Add first field
- [#15891](https://github.com/influxdata/telegraf/pull/15891) `common.socket` Allow parallel parsing with a pool of workers
- [#16141](https://github.com/influxdata/telegraf/pull/16141) `inputs.amqp_consumer` Allow specification of queue arguments
- [#15950](https://github.com/influxdata/telegraf/pull/15950) `inputs.diskio` Add field io await and util
- [#15919](https://github.com/influxdata/telegraf/pull/15919) `inputs.kafka_consumer` Implement startup error behavior options
- [#15910](https://github.com/influxdata/telegraf/pull/15910) `inputs.memcached` Add support for external-store metrics
- [#15990](https://github.com/influxdata/telegraf/pull/15990) `inputs.mock` Add sine phase
- [#16040](https://github.com/influxdata/telegraf/pull/16040) `inputs.modbus` Allow grouping across register types
- [#15865](https://github.com/influxdata/telegraf/pull/15865) `inputs.prometheus` Allow to use secrets for credentials
- [#16230](https://github.com/influxdata/telegraf/pull/16230) `inputs.smart` Add Power on Hours and Cycle Count
- [#15935](https://github.com/influxdata/telegraf/pull/15935) `inputs.snmp` Add displayhint conversion
- [#16027](https://github.com/influxdata/telegraf/pull/16027) `inputs.snmp` Convert uneven bytes to int
- [#15976](https://github.com/influxdata/telegraf/pull/15976) `inputs.socket_listener` Use reception time as timestamp
- [#15853](https://github.com/influxdata/telegraf/pull/15853) `inputs.statsd` Allow reporting sets and timings count as floats
- [#11591](https://github.com/influxdata/telegraf/pull/11591) `inputs.vsphere` Add VM memory configuration
- [#16109](https://github.com/influxdata/telegraf/pull/16109) `inputs.vsphere` Add cpu temperature field
- [#15917](https://github.com/influxdata/telegraf/pull/15917) `inputs` Add option to choose the metric time source
- [#16242](https://github.com/influxdata/telegraf/pull/16242) `logging` Allow overriding message key for structured logging
- [#15742](https://github.com/influxdata/telegraf/pull/15742) `outputs.influxdb_v2` Add rate limit implementation
- [#15943](https://github.com/influxdata/telegraf/pull/15943) `outputs.mqtt` Add sprig functions for topic name generator
- [#16041](https://github.com/influxdata/telegraf/pull/16041) `outputs.postgresql` Allow limiting of column name length
- [#16258](https://github.com/influxdata/telegraf/pull/16258) `outputs` Add rate-limiting infrastructure
- [#16146](https://github.com/influxdata/telegraf/pull/16146) `outputs` Implement partial write errors
- [#15883](https://github.com/influxdata/telegraf/pull/15883) `outputs` Only copy metric if its not filtered out
- [#15893](https://github.com/influxdata/telegraf/pull/15893) `serializers.prometheusremotewrite` Log metric conversion errors

### Bugfixes

- [#16248](https://github.com/influxdata/telegraf/pull/16248) `inputs.netflow` Decode flags in TCP and IP headers correctly
- [#16257](https://github.com/influxdata/telegraf/pull/16257) `inputs.procstat` Handle running processes correctly across multiple filters
- [#16219](https://github.com/influxdata/telegraf/pull/16219) `logging` Add Close() func for redirectLogger
- [#16255](https://github.com/influxdata/telegraf/pull/16255) `logging` Clean up extra empty spaces when redirectLogger is used
- [#16274](https://github.com/influxdata/telegraf/pull/16274) `logging` Fix duplicated prefix and attrMsg in log message when redirectLogger is used

### Dependency Updates

- [#16232](https://github.com/influxdata/telegraf/pull/16232) `deps` Bump cloud.google.com/go/bigquery from 1.63.1 to 1.64.0
- [#16235](https://github.com/influxdata/telegraf/pull/16235) `deps` Bump cloud.google.com/go/storage from 1.43.0 to 1.47.0
- [#16198](https://github.com/influxdata/telegraf/pull/16198) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.42.2 to 1.43.1
- [#16234](https://github.com/influxdata/telegraf/pull/16234) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.29.3 to 1.32.6
- [#16201](https://github.com/influxdata/telegraf/pull/16201) `deps` Bump github.com/intel/powertelemetry from 1.0.1 to 1.0.2
- [#16200](https://github.com/influxdata/telegraf/pull/16200) `deps` Bump github.com/rclone/rclone from 1.68.1 to 1.68.2
- [#16199](https://github.com/influxdata/telegraf/pull/16199) `deps` Bump github.com/vishvananda/netns from 0.0.4 to 0.0.5
- [#16236](https://github.com/influxdata/telegraf/pull/16236) `deps` Bump golang.org/x/net from 0.30.0 to 0.31.0
- [#16250](https://github.com/influxdata/telegraf/pull/16250) `deps` Bump golangci-lint from v1.62.0 to v1.62.2
- [#16233](https://github.com/influxdata/telegraf/pull/16233) `deps` Bump google.golang.org/grpc from 1.67.1 to 1.68.0
- [#16202](https://github.com/influxdata/telegraf/pull/16202) `deps` Bump modernc.org/sqlite from 1.33.1 to 1.34.1
- [#16203](https://github.com/influxdata/telegraf/pull/16203) `deps` Bump super-linter/super-linter from 7.1.0 to 7.2.0

## v1.32.3 [2024-11-18]

### Important Changes

- PR [#16015](https://github.com/influxdata/telegraf/pull/16015) changes the
  internal counters of the Bind plugin to unsigned integers matching the server
  implementation. We keep backward compatibility by setting
  `report_counters_as_int` to `true` by default to avoid type conflicts on the
  output side. However, you should change this setting to `false` as soon as
  possible to avoid invalid values and parsing errors with the v3 XML
  statistics.

### Bugfixes

- [#16123](https://github.com/influxdata/telegraf/pull/16123) `agent` Restore setup order of stateful plugins to Init() then SetState()
- [#16111](https://github.com/influxdata/telegraf/pull/16111) `common.socket` Make sure the scanner buffer matches the read-buffer size
- [#16156](https://github.com/influxdata/telegraf/pull/16156) `common.socket` Use read buffer size config setting as a datagram reader buffer size
- [#16015](https://github.com/influxdata/telegraf/pull/16015) `inputs.bind` Convert counters to uint64
- [#16171](https://github.com/influxdata/telegraf/pull/16171) `inputs.gnmi` Register connection statistics before creating client
- [#16197](https://github.com/influxdata/telegraf/pull/16197) `inputs.netflow` Cast TCP ports to uint16
- [#16110](https://github.com/influxdata/telegraf/pull/16110) `inputs.ntpq` Avoid panic on empty lines and make sure -p is present
- [#16155](https://github.com/influxdata/telegraf/pull/16155) `inputs.snmp` Fix crash when trying to format fields from unknown OIDs
- [#16145](https://github.com/influxdata/telegraf/pull/16145) `inputs.snmp_trap` Remove timeout deprecation
- [#16108](https://github.com/influxdata/telegraf/pull/16108) `logger` Avoid setting the log-format default too early

### Dependency Updates

- [#16093](https://github.com/influxdata/telegraf/pull/16093) `deps` Bump cloud.google.com/go/pubsub from 1.42.0 to 1.45.1
- [#16175](https://github.com/influxdata/telegraf/pull/16175) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.17.37 to 1.17.44
- [#16096](https://github.com/influxdata/telegraf/pull/16096) `deps` Bump github.com/gofrs/uuid/v5 from 5.2.0 to 5.3.0
- [#16136](https://github.com/influxdata/telegraf/pull/16136) `deps` Bump github.com/golang-jwt/jwt/v4 from 4.5.0 to 4.5.1
- [#16094](https://github.com/influxdata/telegraf/pull/16094) `deps` Bump github.com/gopacket/gopacket from 1.2.0 to 1.3.0
- [#16133](https://github.com/influxdata/telegraf/pull/16133) `deps` Bump github.com/jackc/pgtype from 1.14.3 to 1.14.4
- [#16131](https://github.com/influxdata/telegraf/pull/16131) `deps` Bump github.com/openconfig/gnmi from 0.10.0 to 0.11.0
- [#16092](https://github.com/influxdata/telegraf/pull/16092) `deps` Bump github.com/prometheus/client_golang from 1.20.4 to 1.20.5
- [#16178](https://github.com/influxdata/telegraf/pull/16178) `deps` Bump github.com/rclone/rclone from 1.67.0 to 1.68.1
- [#16132](https://github.com/influxdata/telegraf/pull/16132) `deps` Bump github.com/shirou/gopsutil/v4 from 4.24.9 to 4.24.10
- [#16176](https://github.com/influxdata/telegraf/pull/16176) `deps` Bump github.com/sijms/go-ora/v2 from 2.8.19 to 2.8.22
- [#16134](https://github.com/influxdata/telegraf/pull/16134) `deps` Bump github.com/testcontainers/testcontainers-go/modules/kafka from 0.33.0 to 0.34.0
- [#16174](https://github.com/influxdata/telegraf/pull/16174) `deps` Bump github.com/tidwall/gjson from 1.17.1 to 1.18.0
- [#16135](https://github.com/influxdata/telegraf/pull/16135) `deps` Bump github.com/vmware/govmomi from 0.39.0 to 0.45.1
- [#16095](https://github.com/influxdata/telegraf/pull/16095) `deps` Bump golang.org/x/sys from 0.25.0 to 0.26.0
- [#16177](https://github.com/influxdata/telegraf/pull/16177) `deps` Bump golang.org/x/text from 0.19.0 to 0.20.0
- [#16172](https://github.com/influxdata/telegraf/pull/16172) `deps` Bump golangci-lint from v1.61.0 to v1.62.0

## v1.32.2 [2024-10-28]

### Bugfixes

- [#15966](https://github.com/influxdata/telegraf/pull/15966) `agent` Use a unique WAL file for plugin instances of the same type
- [#16074](https://github.com/influxdata/telegraf/pull/16074) `inputs.kafka_consumer` Fix deadlock
- [#16009](https://github.com/influxdata/telegraf/pull/16009) `inputs.netflow` Cast complex types to field compatible ones
- [#16026](https://github.com/influxdata/telegraf/pull/16026) `inputs.opcua` Allow to retry reads on invalid sessions
- [#16060](https://github.com/influxdata/telegraf/pull/16060) `inputs.procstat` Correctly use systemd-unit setting for finding them
- [#16008](https://github.com/influxdata/telegraf/pull/16008) `inputs.win_eventlog` Handle XML data fields' filtering the same way as event fields
- [#15968](https://github.com/influxdata/telegraf/pull/15968) `outputs.remotefile` Create a new serializer instance per output file
- [#16014](https://github.com/influxdata/telegraf/pull/16014) `outputs.syslog` Trim field-names belonging to explicit SDIDs correctly

### Dependency Updates

- [#15992](https://github.com/influxdata/telegraf/pull/15992) `deps` Bump cloud.google.com/go/bigquery from 1.62.0 to 1.63.1
- [#16056](https://github.com/influxdata/telegraf/pull/16056) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azcore from 1.14.0 to 1.16.0
- [#16021](https://github.com/influxdata/telegraf/pull/16021) `deps` Bump github.com/IBM/sarama from 1.43.2 to 1.43.3
- [#16019](https://github.com/influxdata/telegraf/pull/16019) `deps` Bump github.com/alitto/pond from 1.9.0 to 1.9.2
- [#16018](https://github.com/influxdata/telegraf/pull/16018) `deps` Bump github.com/apache/thrift from 0.20.0 to 0.21.0
- [#16054](https://github.com/influxdata/telegraf/pull/16054) `deps` Bump github.com/aws/aws-sdk-go-v2 from 1.32.1 to 1.32.2
- [#15996](https://github.com/influxdata/telegraf/pull/15996) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.40.4 to 1.42.1
- [#16055](https://github.com/influxdata/telegraf/pull/16055) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.42.1 to 1.42.2
- [#16057](https://github.com/influxdata/telegraf/pull/16057) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.34.9 to 1.36.2
- [#16022](https://github.com/influxdata/telegraf/pull/16022) `deps` Bump github.com/docker/docker from 27.1.1+incompatible to 27.3.1+incompatible
- [#15993](https://github.com/influxdata/telegraf/pull/15993) `deps` Bump github.com/gosnmp/gosnmp from 1.37.0 to 1.38.0
- [#15947](https://github.com/influxdata/telegraf/pull/15947) `deps` Bump github.com/gwos/tcg/sdk from v8.7.2 to v8.8.0
- [#16053](https://github.com/influxdata/telegraf/pull/16053) `deps` Bump github.com/lxc/incus/v6 from 6.2.0 to 6.6.0
- [#15994](https://github.com/influxdata/telegraf/pull/15994) `deps` Bump github.com/signalfx/golib/v3 from 3.3.53 to 3.3.54
- [#15995](https://github.com/influxdata/telegraf/pull/15995) `deps` Bump github.com/snowflakedb/gosnowflake from 1.11.1 to 1.11.2
- [#16020](https://github.com/influxdata/telegraf/pull/16020) `deps` Bump go.step.sm/crypto from 0.51.1 to 0.54.0
- [#16023](https://github.com/influxdata/telegraf/pull/16023) `deps` Bump github.com/shirou/gopsutil from v3.24.4 to v4.24.9

## v1.32.1 [2024-10-07]

### Important Changes

- PR [#15796](https://github.com/influxdata/telegraf/pull/15796) changes the
  delivery state update of un-parseable messages from `ACK` to `NACK` without
  requeueing. This way, those messages are not lost and can optionally be
  handled using a dead-letter exchange by other means.
- Removal of old-style serializer creation. This should not directly affect
  users as it is an API change. All serializers in Telegraf are already ported
  to the new framework. If you experience any issues with not being able to
  create serializers let us know!

### Bugfixes

- [#15969](https://github.com/influxdata/telegraf/pull/15969) `agent` Fix buffer not flushing if all metrics are written
- [#15937](https://github.com/influxdata/telegraf/pull/15937) `config` Correctly print removal version info
- [#15900](https://github.com/influxdata/telegraf/pull/15900) `common.http` Keep timeout after creating oauth client
- [#15796](https://github.com/influxdata/telegraf/pull/15796) `inputs.amqp_consumer` NACKing messages on non-delivery related errors
- [#15923](https://github.com/influxdata/telegraf/pull/15923) `inputs.cisco_telemetry_mdt` Handle NXOS DME subtree telemetry format
- [#15907](https://github.com/influxdata/telegraf/pull/15907) `inputs.consul` Move config checking to Init method
- [#15982](https://github.com/influxdata/telegraf/pull/15982) `inputs.influxdb_v2_listener` Fix concurrent read/write dict
- [#15960](https://github.com/influxdata/telegraf/pull/15960) `inputs.vsphere` Add tags to VSAN ESA disks
- [#15921](https://github.com/influxdata/telegraf/pull/15921) `parsers.avro` Add mutex to cache access
- [#15965](https://github.com/influxdata/telegraf/pull/15965) `processors.aws_ec2` Remove leading slash and cancel worker only if it exists

### Dependency Updates

- [#15932](https://github.com/influxdata/telegraf/pull/15932) `deps` Bump cloud.google.com/go/monitoring from 1.20.2 to 1.21.1
- [#15863](https://github.com/influxdata/telegraf/pull/15863) `deps` Bump github.com/Azure/azure-kusto-go from 0.15.3 to 0.16.1
- [#15862](https://github.com/influxdata/telegraf/pull/15862) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azcore from 1.13.0 to 1.14.0
- [#15957](https://github.com/influxdata/telegraf/pull/15957) `deps` Bump github.com/aws/aws-sdk-go-v2/feature/ec2/imds from 1.16.12 to 1.16.14
- [#15859](https://github.com/influxdata/telegraf/pull/15859) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.34.4 to 1.34.9
- [#15931](https://github.com/influxdata/telegraf/pull/15931) `deps` Bump github.com/boschrexroth/ctrlx-datalayer-golang from 1.3.0 to 1.3.1
- [#15890](https://github.com/influxdata/telegraf/pull/15890) `deps` Bump github.com/harlow/kinesis-consumer from v0.3.6-0.20240606153816-553e2392fdf3 to v0.3.6-0.20240916192723-43900507c911
- [#15904](https://github.com/influxdata/telegraf/pull/15904) `deps` Bump github.com/netsampler/goflow2/v2 from 2.1.5 to 2.2.1
- [#15903](https://github.com/influxdata/telegraf/pull/15903) `deps` Bump github.com/p4lang/p4runtime from 1.3.0 to 1.4.0
- [#15905](https://github.com/influxdata/telegraf/pull/15905) `deps` Bump github.com/prometheus/client_golang from 1.20.2 to 1.20.3
- [#15930](https://github.com/influxdata/telegraf/pull/15930) `deps` Bump github.com/prometheus/client_golang from 1.20.3 to 1.20.4
- [#15962](https://github.com/influxdata/telegraf/pull/15962) `deps` Bump github.com/prometheus/common from 0.55.0 to 0.60.0
- [#15860](https://github.com/influxdata/telegraf/pull/15860) `deps` Bump github.com/snowflakedb/gosnowflake from 1.10.0 to 1.11.1
- [#15954](https://github.com/influxdata/telegraf/pull/15954) `deps` Bump github.com/srebhan/protobufquery from 0.0.0-20230803132024-ae4c0d878e55 to 1.0.1
- [#15929](https://github.com/influxdata/telegraf/pull/15929) `deps` Bump go.mongodb.org/mongo-driver from 1.16.0 to 1.17.0
- [#15902](https://github.com/influxdata/telegraf/pull/15902) `deps` Bump golang.org/x/mod from 0.19.0 to 0.21.0
- [#15955](https://github.com/influxdata/telegraf/pull/15955) `deps` Bump golang.org/x/oauth2 from 0.21.0 to 0.23.0
- [#15861](https://github.com/influxdata/telegraf/pull/15861) `deps` Bump golang.org/x/term from 0.23.0 to 0.24.0
- [#15856](https://github.com/influxdata/telegraf/pull/15856) `deps` Bump golangci-lint from v1.60.3 to v1.61.0
- [#15933](https://github.com/influxdata/telegraf/pull/15933) `deps` Bump k8s.io/apimachinery from 0.30.1 to 0.31.1
- [#15901](https://github.com/influxdata/telegraf/pull/15901) `deps` Bump modernc.org/sqlite from 1.32.0 to 1.33.1

## v1.32.0 [2024-09-09]

### Important Changes

- This release contains a logging overhaul as well as some new features for
  logging (see PRs [#15556](https://github.com/influxdata/telegraf/pull/15556),
  [#15629](https://github.com/influxdata/telegraf/pull/15629),
  [#15677](https://github.com/influxdata/telegraf/pull/15677),
  [#15695](https://github.com/influxdata/telegraf/pull/15695) and
  [#15751](https://github.com/influxdata/telegraf/pull/15751)).
  As a consequence the redunant `logtarget` setting is deprecated, `stderr` is
  used if no `logfile` is provided, otherwise messages are logged to the given
  file. For using the Windows `eventlog` set `logformat = "eventlog"`!
- This release contains a change in json_v2 parser config parsing -
  if the config is empty (not define any rules), initialization will fail
  (see PR [#15844](https://github.com/influxdata/telegraf/pull/15844)).
- This release contains a feature for a disk-backed metric buffer under the
  `buffer_strategy` agent config (see
  PR [#15564](https://github.com/influxdata/telegraf/pull/15564)).
  Please note, this feature is **experimental**, please give it a test and
  report any issues you encounter.

### New Plugins

- [#15700](https://github.com/influxdata/telegraf/pull/15700) `inputs.slurm` SLURM workload manager
- [#15602](https://github.com/influxdata/telegraf/pull/15602) `outputs.parquet` Parquet file writer
- [#15569](https://github.com/influxdata/telegraf/pull/15569) `outputs.remotefile` Output to remote location like S3

### Features

- [#15732](https://github.com/influxdata/telegraf/pull/15732) `agent` Add config check sub-command
- [#15564](https://github.com/influxdata/telegraf/pull/15564) `agent` Add metric disk buffer
- [#15645](https://github.com/influxdata/telegraf/pull/15645) `agent` Enable watching for new configuration files
- [#15644](https://github.com/influxdata/telegraf/pull/15644) `agent` Watch for deleted files
- [#15695](https://github.com/influxdata/telegraf/pull/15695) `logging` Add 'trace' log-level
- [#15677](https://github.com/influxdata/telegraf/pull/15677) `logging` Allow to override log-level per plugin
- [#15751](https://github.com/influxdata/telegraf/pull/15751) `logging` Implement structured logging
- [#15640](https://github.com/influxdata/telegraf/pull/15640) `common.cookie` Allow usage of secrets in headers
- [#15636](https://github.com/influxdata/telegraf/pull/15636) `common.shim` Enable metric tracking within external plugins
- [#15570](https://github.com/influxdata/telegraf/pull/15570) `common.tls` Allow group aliases for cipher-suites
- [#15628](https://github.com/influxdata/telegraf/pull/15628) `inputs.amd_rocm_smi` Parse newer ROCm versions
- [#15519](https://github.com/influxdata/telegraf/pull/15519) `inputs.azure_monitor` Add client options parameter
- [#15544](https://github.com/influxdata/telegraf/pull/15544) `inputs.elasticsearch` Add support for custom headers
- [#15688](https://github.com/influxdata/telegraf/pull/15688) `inputs.elasticsearch` Gather enrich stats
- [#15834](https://github.com/influxdata/telegraf/pull/15834) `inputs.execd` Allow to provide logging prefixes on stderr
- [#15764](https://github.com/influxdata/telegraf/pull/15764) `inputs.http_listener_v2` Add unix socket mode
- [#15495](https://github.com/influxdata/telegraf/pull/15495) `inputs.ipmi_sensor` Collect additional commands
- [#15790](https://github.com/influxdata/telegraf/pull/15790) `inputs.kafka_consumer` Allow to select the metric time source
- [#15648](https://github.com/influxdata/telegraf/pull/15648) `inputs.modbus` Allow reading single bits of input and holding registers
- [#15528](https://github.com/influxdata/telegraf/pull/15528) `inputs.mqtt_consumer` Add variable length topic parsing
- [#15486](https://github.com/influxdata/telegraf/pull/15486) `inputs.mqtt_consumer` Implement startup error behaviors
- [#15749](https://github.com/influxdata/telegraf/pull/15749) `inputs.mysql` Add support for replica status
- [#15521](https://github.com/influxdata/telegraf/pull/15521) `inputs.netflow` Add more fields for sFlow extended gateway packets
- [#15396](https://github.com/influxdata/telegraf/pull/15396) `inputs.netflow` Add support for sFlow drop notification packets
- [#15468](https://github.com/influxdata/telegraf/pull/15468) `inputs.openstack` Allow collection without admin privileges
- [#15637](https://github.com/influxdata/telegraf/pull/15637) `inputs.opentelemetry` Add profiles support
- [#15423](https://github.com/influxdata/telegraf/pull/15423) `inputs.procstat` Add ability to collect per-process socket statistics
- [#15655](https://github.com/influxdata/telegraf/pull/15655) `inputs.s7comm` Implement startup-error behavior settings
- [#15600](https://github.com/influxdata/telegraf/pull/15600) `inputs.sql` Add SAP HANA SQL driver
- [#15424](https://github.com/influxdata/telegraf/pull/15424) `inputs.sqlserver` Introduce user specified ID parameter for ADD logins
- [#15687](https://github.com/influxdata/telegraf/pull/15687) `inputs.statsd` Expose allowed_pending_messages as internal stat
- [#15458](https://github.com/influxdata/telegraf/pull/15458) `inputs.systemd_units` Support user scoped units
- [#15702](https://github.com/influxdata/telegraf/pull/15702) `outputs.datadog` Add support for submitting alongside dd-agent
- [#15668](https://github.com/influxdata/telegraf/pull/15668) `outputs.dynatrace` Report metrics as a delta counter using regular expression
- [#15471](https://github.com/influxdata/telegraf/pull/15471) `outputs.elasticsearch` Allow custom template index settings
- [#15613](https://github.com/influxdata/telegraf/pull/15613) `outputs.elasticsearch` Support data streams
- [#15722](https://github.com/influxdata/telegraf/pull/15722) `outputs.kafka` Add option to add metric name as record header
- [#15689](https://github.com/influxdata/telegraf/pull/15689) `outputs.kafka` Add option to set producer message timestamp
- [#15787](https://github.com/influxdata/telegraf/pull/15787) `outputs.syslog` Implement startup error behavior options
- [#15697](https://github.com/influxdata/telegraf/pull/15697) `parsers.value` Add base64 datatype
- [#15795](https://github.com/influxdata/telegraf/pull/15795) `processors.aws_ec2` Allow to use instance metadata

### Bugfixes

- [#15661](https://github.com/influxdata/telegraf/pull/15661) `agent` Fix buffer directory config and document
- [#15788](https://github.com/influxdata/telegraf/pull/15788) `inputs.kinesis_consumer` Honor the configured endpoint
- [#15791](https://github.com/influxdata/telegraf/pull/15791) `inputs.mysql` Enforce float for all known floating-point information
- [#15743](https://github.com/influxdata/telegraf/pull/15743) `inputs.snmp` Avoid sending a nil to gosmi's GetEnumBitsFormatted
- [#15815](https://github.com/influxdata/telegraf/pull/15815) `logger` Handle trace level for standard log
- [#15781](https://github.com/influxdata/telegraf/pull/15781) `outputs.kinesis` Honor the configured endpoint
- [#15615](https://github.com/influxdata/telegraf/pull/15615) `outputs.remotefile` Resolve linter not checking error
- [#15740](https://github.com/influxdata/telegraf/pull/15740) `serializers.template` Unwrap metrics if required

### Dependency Updates

- [#15829](https://github.com/influxdata/telegraf/pull/15829) `deps` Bump github.com/BurntSushi/toml from 1.3.2 to 1.4.0
- [#15775](https://github.com/influxdata/telegraf/pull/15775) `deps` Bump github.com/aws/aws-sdk-go-v2/feature/ec2/imds from 1.16.11 to 1.16.12
- [#15733](https://github.com/influxdata/telegraf/pull/15733) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.38.7 to 1.40.3
- [#15761](https://github.com/influxdata/telegraf/pull/15761) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.40.3 to 1.40.4
- [#15827](https://github.com/influxdata/telegraf/pull/15827) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.37.3 to 1.38.0
- [#15760](https://github.com/influxdata/telegraf/pull/15760) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.25.5 to 1.27.4
- [#15737](https://github.com/influxdata/telegraf/pull/15737) `deps` Bump github.com/eclipse/paho.mqtt.golang from 1.4.3 to 1.5.0
- [#15734](https://github.com/influxdata/telegraf/pull/15734) `deps` Bump github.com/google/cel-go from 0.20.1 to 0.21.0
- [#15777](https://github.com/influxdata/telegraf/pull/15777) `deps` Bump github.com/miekg/dns from 1.1.59 to 1.1.62
- [#15828](https://github.com/influxdata/telegraf/pull/15828) `deps` Bump github.com/openconfig/goyang from 1.5.0 to 1.6.0
- [#15735](https://github.com/influxdata/telegraf/pull/15735) `deps` Bump github.com/pion/dtls/v2 from 2.2.11 to 2.2.12
- [#15779](https://github.com/influxdata/telegraf/pull/15779) `deps` Bump github.com/prometheus/client_golang from 1.19.1 to 1.20.2
- [#15831](https://github.com/influxdata/telegraf/pull/15831) `deps` Bump github.com/prometheus/prometheus from 0.53.1 to 0.54.1
- [#15736](https://github.com/influxdata/telegraf/pull/15736) `deps` Bump github.com/redis/go-redis/v9 from 9.5.1 to 9.6.1
- [#15830](https://github.com/influxdata/telegraf/pull/15830) `deps` Bump github.com/seancfoley/ipaddress-go from 1.6.0 to 1.7.0
- [#15842](https://github.com/influxdata/telegraf/pull/15842) `deps` Bump github.com/showwin/speedtest-go from 1.7.7 to 1.7.9
- [#15778](https://github.com/influxdata/telegraf/pull/15778) `deps` Bump go.step.sm/crypto from 0.50.0 to 0.51.1
- [#15776](https://github.com/influxdata/telegraf/pull/15776) `deps` Bump golang.org/x/net from 0.27.0 to 0.28.0
- [#15757](https://github.com/influxdata/telegraf/pull/15757) `deps` Bump golang.org/x/sync from 0.7.0 to 0.8.0
- [#15759](https://github.com/influxdata/telegraf/pull/15759) `deps` Bump gonum.org/v1/gonum from 0.15.0 to 0.15.1
- [#15758](https://github.com/influxdata/telegraf/pull/15758) `deps` Bump modernc.org/sqlite from 1.30.0 to 1.32.0
- [#15756](https://github.com/influxdata/telegraf/pull/15756) `deps` Bump super-linter/super-linter from 6.8.0 to 7.0.0
- [#15826](https://github.com/influxdata/telegraf/pull/15826) `deps` Bump super-linter/super-linter from 7.0.0 to 7.1.0
- [#15780](https://github.com/influxdata/telegraf/pull/15780) `deps` Bump tj-actions/changed-files from 44 to 45

## v1.31.3 [2024-08-12]

### Bugfixes

- [#15552](https://github.com/influxdata/telegraf/pull/15552) `inputs.chrony` Use DGRAM for the unix socket
- [#15667](https://github.com/influxdata/telegraf/pull/15667) `inputs.diskio` Print warnings once, add details to messages
- [#15670](https://github.com/influxdata/telegraf/pull/15670) `inputs.mqtt_consumer` Restore trace logging option
- [#15696](https://github.com/influxdata/telegraf/pull/15696) `inputs.opcua` Reconnect if closed connection
- [#15724](https://github.com/influxdata/telegraf/pull/15724) `inputs.smartctl` Use --scan-open instead of --scan to provide correct device type info
- [#15649](https://github.com/influxdata/telegraf/pull/15649) `inputs.tail` Prevent deadlock when closing and max undelivered lines hit

### Dependency Updates

- [#15720](https://github.com/influxdata/telegraf/pull/15720) `deps` Bump Go from v1.22.5 to v1.22.6
- [#15683](https://github.com/influxdata/telegraf/pull/15683) `deps` Bump cloud.google.com/go/bigquery from 1.61.0 to 1.62.0
- [#15654](https://github.com/influxdata/telegraf/pull/15654) `deps` Bump cloud.google.com/go/monitoring from 1.19.0 to 1.20.2
- [#15679](https://github.com/influxdata/telegraf/pull/15679) `deps` Bump cloud.google.com/go/monitoring from 1.20.2 to 1.20.3
- [#15626](https://github.com/influxdata/telegraf/pull/15626) `deps` Bump github.com/antchfx/xmlquery from 1.4.0 to 1.4.1
- [#15706](https://github.com/influxdata/telegraf/pull/15706) `deps` Bump github.com/apache/iotdb-client-go from 1.2.0-tsbs to 1.3.2
- [#15651](https://github.com/influxdata/telegraf/pull/15651) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.17.17 to 1.17.27
- [#15703](https://github.com/influxdata/telegraf/pull/15703) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from v1.27.4 to v1.29.3
- [#15681](https://github.com/influxdata/telegraf/pull/15681) `deps` Bump github.com/docker/docker from 25.0.5-incompatible to 27.1.1-incompatible
- [#15650](https://github.com/influxdata/telegraf/pull/15650) `deps` Bump github.com/gofrs/uuid/v5 from 5.0.0 to 5.2.0
- [#15705](https://github.com/influxdata/telegraf/pull/15705) `deps` Bump github.com/gorilla/websocket from 1.5.1 to 1.5.3
- [#15708](https://github.com/influxdata/telegraf/pull/15708) `deps` Bump github.com/multiplay/go-ts3 from 1.1.0 to 1.2.0
- [#15707](https://github.com/influxdata/telegraf/pull/15707) `deps` Bump github.com/prometheus-community/pro-bing from 0.4.0 to 0.4.1
- [#15709](https://github.com/influxdata/telegraf/pull/15709) `deps` Bump github.com/prometheus/prometheus from 0.48.1 to 0.53.1
- [#15680](https://github.com/influxdata/telegraf/pull/15680) `deps` Bump github.com/vmware/govmomi from 0.37.2 to 0.39.0
- [#15682](https://github.com/influxdata/telegraf/pull/15682) `deps` Bump go.mongodb.org/mongo-driver from 1.14.0 to 1.16.0
- [#15652](https://github.com/influxdata/telegraf/pull/15652) `deps` Bump go.step.sm/crypto from 0.47.1 to 0.50.0
- [#15653](https://github.com/influxdata/telegraf/pull/15653) `deps` Bump google.golang.org/grpc from 1.64.1 to 1.65.0
- [#15704](https://github.com/influxdata/telegraf/pull/15704) `deps` Bump super-linter/super-linter from 6.7.0 to 6.8.0

## v1.31.2 [2024-07-22]

### Bugfixes

- [#15589](https://github.com/influxdata/telegraf/pull/15589) `common.socket` Switch to context to simplify closing
- [#15601](https://github.com/influxdata/telegraf/pull/15601) `inputs.ping` Check addr length to avoid crash
- [#15618](https://github.com/influxdata/telegraf/pull/15618) `inputs.snmp` Translate field correctly when not in table
- [#15586](https://github.com/influxdata/telegraf/pull/15586) `parsers.xpath` Allow resolving extensions
- [#15630](https://github.com/influxdata/telegraf/pull/15630) `tools.custom_builder` Handle multiple instances of the same plugin correctly

### Dependency Updates

- [#15582](https://github.com/influxdata/telegraf/pull/15582) `deps` Bump cloud.google.com/go/storage from 1.41.0 to 1.42.0
- [#15623](https://github.com/influxdata/telegraf/pull/15623) `deps` Bump cloud.google.com/go/storage from 1.42.0 to 1.43.0
- [#15607](https://github.com/influxdata/telegraf/pull/15607) `deps` Bump github.com/alitto/pond from 1.8.3 to 1.9.0
- [#15625](https://github.com/influxdata/telegraf/pull/15625) `deps` Bump github.com/antchfx/xpath from 1.3.0 to 1.3.1
- [#15622](https://github.com/influxdata/telegraf/pull/15622) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.34.3 to 1.37.3
- [#15606](https://github.com/influxdata/telegraf/pull/15606) `deps` Bump github.com/hashicorp/consul/api from 1.26.1 to 1.29.1
- [#15604](https://github.com/influxdata/telegraf/pull/15604) `deps` Bump github.com/jackc/pgx/v4 from 4.18.2 to 4.18.3
- [#15581](https://github.com/influxdata/telegraf/pull/15581) `deps` Bump github.com/nats-io/nats-server/v2 from 2.10.16 to 2.10.17
- [#15603](https://github.com/influxdata/telegraf/pull/15603) `deps` Bump github.com/openconfig/goyang from 1.0.0 to 1.5.0
- [#15624](https://github.com/influxdata/telegraf/pull/15624) `deps` Bump github.com/sijms/go-ora/v2 from 2.8.4 to 2.8.19
- [#15585](https://github.com/influxdata/telegraf/pull/15585) `deps` Bump github.com/testcontainers/testcontainers-go/modules/kafka from 0.30.0 to 0.31.0
- [#15605](https://github.com/influxdata/telegraf/pull/15605) `deps` Bump github.com/tinylib/msgp from 1.1.9 to 1.2.0
- [#15584](https://github.com/influxdata/telegraf/pull/15584) `deps` Bump github.com/urfave/cli/v2 from 2.27.1 to 2.27.2
- [#15614](https://github.com/influxdata/telegraf/pull/15614) `deps` Bump google.golang.org/grpc from 1.64.0 to 1.64.1
- [#15608](https://github.com/influxdata/telegraf/pull/15608) `deps` Bump super-linter/super-linter from 6.6.0 to 6.7.0

For versions earlier than v1.13 and earlier see
[CHANGELOG-1.13.md](CHANGELOG-1.13.md).

## v1.31.1 [2024-07-01]

### Bugfixes

- [#15488](https://github.com/influxdata/telegraf/pull/15488) `agent` Ignore startup-errors in test mode
- [#15568](https://github.com/influxdata/telegraf/pull/15568) `inputs.chrony` Handle ServerStats4 response
- [#15551](https://github.com/influxdata/telegraf/pull/15551) `inputs.chrony` Support local (reference) sources
- [#15565](https://github.com/influxdata/telegraf/pull/15565) `inputs.gnmi` Handle YANG namespaces in paths correctly
- [#15496](https://github.com/influxdata/telegraf/pull/15496) `inputs.http_response` Fix for IPv4 and IPv6 addresses when interface is set
- [#15493](https://github.com/influxdata/telegraf/pull/15493) `inputs.mysql` Handle custom TLS configs correctly
- [#15514](https://github.com/influxdata/telegraf/pull/15514) `logging` Add back constants for backward compatibility
- [#15531](https://github.com/influxdata/telegraf/pull/15531) `secretstores.oauth2` Ensure endpoint params is not nil

### Dependency Updates

- [#15483](https://github.com/influxdata/telegraf/pull/15483) `deps` Bump cloud.google.com/go/monitoring from 1.18.1 to 1.19.0
- [#15559](https://github.com/influxdata/telegraf/pull/15559) `deps` Bump github.com/Azure/azure-kusto-go from 0.15.2 to 0.15.3
- [#15489](https://github.com/influxdata/telegraf/pull/15489) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/azidentity from 1.5.1 to 1.6.0
- [#15560](https://github.com/influxdata/telegraf/pull/15560) `deps` Bump github.com/Azure/go-autorest/autorest/azure/auth from 0.5.12 to 0.5.13
- [#15480](https://github.com/influxdata/telegraf/pull/15480) `deps` Bump github.com/IBM/sarama from 1.43.1 to 1.43.2
- [#15526](https://github.com/influxdata/telegraf/pull/15526) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.37.0 to 1.38.7
- [#15527](https://github.com/influxdata/telegraf/pull/15527) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.30.2 to 1.32.9
- [#15558](https://github.com/influxdata/telegraf/pull/15558) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.32.9 to 1.33.2
- [#15448](https://github.com/influxdata/telegraf/pull/15448) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.161.1 to 1.162.1
- [#15557](https://github.com/influxdata/telegraf/pull/15557) `deps` Bump github.com/go-ldap/ldap/v3 from 3.4.6 to 3.4.8
- [#15523](https://github.com/influxdata/telegraf/pull/15523) `deps` Bump github.com/linkedin/goavro/v2 from 2.12.0 to 2.13.0
- [#15484](https://github.com/influxdata/telegraf/pull/15484) `deps` Bump github.com/microsoft/go-mssqldb from 1.7.0 to 1.7.2
- [#15561](https://github.com/influxdata/telegraf/pull/15561) `deps` Bump github.com/nats-io/nats-server/v2 from 2.10.14 to 2.10.16
- [#15524](https://github.com/influxdata/telegraf/pull/15524) `deps` Bump github.com/prometheus/common from 0.53.0 to 0.54.0
- [#15481](https://github.com/influxdata/telegraf/pull/15481) `deps` Bump github.com/prometheus/procfs from 0.15.0 to 0.15.1
- [#15482](https://github.com/influxdata/telegraf/pull/15482) `deps` Bump github.com/rabbitmq/amqp091-go from 1.9.0 to 1.10.0
- [#15525](https://github.com/influxdata/telegraf/pull/15525) `deps` Bump go.step.sm/crypto from 0.44.1 to 0.47.1
- [#15479](https://github.com/influxdata/telegraf/pull/15479) `deps` Bump super-linter/super-linter from 6.5.1 to 6.6.0

## v1.31.0 [2024-06-10]

### Important Changes

- [PR #15186](https://github.com/influxdata/telegraf/pull/15186) changes the
  meaning of `inputs.procstat` fields `read_bytes` and `write_bytes` on Linux
  to now contain _all_ I/O operations for consistency with other
  operating-systems. The previous values are output as `disk_read_bytes` and
  `disk_write_bytes` measuring _only_ the I/O on the storage layer.

### New Plugins

- [#15066](https://github.com/influxdata/telegraf/pull/15066) `inputs.smartctl` smartctl
- [#15298](https://github.com/influxdata/telegraf/pull/15298) `parsers.openmetrics` OpenMetrics
- [#15008](https://github.com/influxdata/telegraf/pull/15008) `parsers.parquet` Apache Parquet
- [#15094](https://github.com/influxdata/telegraf/pull/15094) `processors.timestamp` Timestamp

### Features

- [#15433](https://github.com/influxdata/telegraf/pull/15433) `agent` Add uint support in cli test output
- [#15377](https://github.com/influxdata/telegraf/pull/15377) `agent` Introduce CLI option to set config URL retry attempts
- [#15388](https://github.com/influxdata/telegraf/pull/15388) `agent` Introduce CLI option to reload remote URL configs on change
- [#15030](https://github.com/influxdata/telegraf/pull/15030) `aggregators.basicstats` Add last field
- [#15268](https://github.com/influxdata/telegraf/pull/15268) `aggregators.final` Add option to disable appending _final
- [#15319](https://github.com/influxdata/telegraf/pull/15319) `aggregators.merge` Allow to round metric timestamps
- [#15426](https://github.com/influxdata/telegraf/pull/15426) `cli` List available parsers and serializers
- [#15341](https://github.com/influxdata/telegraf/pull/15341) `common.opcua` Add session timeout as configuration option
- [#15395](https://github.com/influxdata/telegraf/pull/15395) `input.azure_monitor` Use default Azure credentials chain when no secret provided
- [#15233](https://github.com/influxdata/telegraf/pull/15233) `inputs.ceph` Use perf schema to determine metric type
- [#14992](https://github.com/influxdata/telegraf/pull/14992) `inputs.dns_query` Allow ignoring errors of specific types
- [#15400](https://github.com/influxdata/telegraf/pull/15400) `inputs.exec` Add option to ignore return code
- [#15271](https://github.com/influxdata/telegraf/pull/15271) `inputs.execd` Add option to not restart program on error
- [#15330](https://github.com/influxdata/telegraf/pull/15330) `inputs.file` Add tag with absolute path of file
- [#15171](https://github.com/influxdata/telegraf/pull/15171) `inputs.gnmi` Add keepalive settings
- [#15278](https://github.com/influxdata/telegraf/pull/15278) `inputs.gnmi` Add option to create more descriptive tags
- [#15173](https://github.com/influxdata/telegraf/pull/15173) `inputs.gnmi` Add secret store support for username and password
- [#15201](https://github.com/influxdata/telegraf/pull/15201) `inputs.gnmi` Add yang-model decoding of JSON IETF payloads
- [#15256](https://github.com/influxdata/telegraf/pull/15256) `inputs.gnmi` Allow to pass accepted cipher suites
- [#15454](https://github.com/influxdata/telegraf/pull/15454) `inputs.http_listener` Allow setting custom success return code
- [#15110](https://github.com/influxdata/telegraf/pull/15110) `inputs.http_response` Add cookie authentication
- [#15438](https://github.com/influxdata/telegraf/pull/15438) `inputs.influxdb` Add metrics for build, crypto and commandline
- [#15361](https://github.com/influxdata/telegraf/pull/15361) `inputs.influxdb_v2_listener` Add support for rate limiting
- [#15407](https://github.com/influxdata/telegraf/pull/15407) `inputs.influxdb_v2_listener` Support secret store for token
- [#15329](https://github.com/influxdata/telegraf/pull/15329) `inputs.internet_speed` Introduce packet loss field
- [#15368](https://github.com/influxdata/telegraf/pull/15368) `inputs.kafka_consumer` Add resolve canonical bootstrap server option
- [#15169](https://github.com/influxdata/telegraf/pull/15169) `inputs.knx_listener` Add support for string data type
- [#15069](https://github.com/influxdata/telegraf/pull/15069) `inputs.knx_listener` Allow usage of DPT string representation
- [#15049](https://github.com/influxdata/telegraf/pull/15049) `inputs.kubernetes` Add option to node metric name
- [#15044](https://github.com/influxdata/telegraf/pull/15044) `inputs.lustre2` Add eviction_count field
- [#15042](https://github.com/influxdata/telegraf/pull/15042) `inputs.lustre2` Add health-check metric
- [#14813](https://github.com/influxdata/telegraf/pull/14813) `inputs.lustre2` Add support for bulk read/write stats
- [#15045](https://github.com/influxdata/telegraf/pull/15045) `inputs.lustre2` Skip brw_stats in case of insufficient permissions
- [#15270](https://github.com/influxdata/telegraf/pull/15270) `inputs.mock` Add baseline option to sine
- [#15314](https://github.com/influxdata/telegraf/pull/15314) `inputs.netflow` Add support for IPFIX option packets
- [#15180](https://github.com/influxdata/telegraf/pull/15180) `inputs.netflow` Add support for netflow v9 option packets
- [#15282](https://github.com/influxdata/telegraf/pull/15282) `inputs.nvidia_smi` Add power-limit field for v12 scheme
- [#15460](https://github.com/influxdata/telegraf/pull/15460) `inputs.openstack` Use service catalog from v3 authentication if available
- [#15231](https://github.com/influxdata/telegraf/pull/15231) `inputs.opentelemetry` Add option to set max receive message size
- [#15299](https://github.com/influxdata/telegraf/pull/15299) `inputs.procstat` Add option to select properties to collect
- [#14948](https://github.com/influxdata/telegraf/pull/14948) `inputs.procstat` Allow multiple selection criteria
- [#15186](https://github.com/influxdata/telegraf/pull/15186) `inputs.procstat` Report consistent I/O on Linux
- [#14981](https://github.com/influxdata/telegraf/pull/14981) `inputs.radius` Provide setting to set request IP address
- [#15293](https://github.com/influxdata/telegraf/pull/15293) `inputs.redis` Add latency percentiles metric
- [#15000](https://github.com/influxdata/telegraf/pull/15000) `inputs.s7comm`  Add optional connection type setting
- [#15439](https://github.com/influxdata/telegraf/pull/15439) `inputs.snmp` Convert octet string with invalid data to hex
- [#15137](https://github.com/influxdata/telegraf/pull/15137) `inputs.sqlserver` Add persistent version store metrics
- [#15380](https://github.com/influxdata/telegraf/pull/15380) `inputs.statsd` Add support for DogStatsD v1.2
- [#15371](https://github.com/influxdata/telegraf/pull/15371) `inputs.statsd` Allow counters to report as float
- [#15306](https://github.com/influxdata/telegraf/pull/15306) `inputs.win_eventlog` Add option to define event batch-size
- [#14973](https://github.com/influxdata/telegraf/pull/14973) `inputs.win_wmi` Add support for remote queries
- [#15300](https://github.com/influxdata/telegraf/pull/15300) `inputs.win_wmi` Allow to invoke methods
- [#15145](https://github.com/influxdata/telegraf/pull/15145) `inputs` Add framework to retry on startup errors
- [#15065](https://github.com/influxdata/telegraf/pull/15065) `outputs.cratedb` Allow configuration of startup error handling
- [#15477](https://github.com/influxdata/telegraf/pull/15477) `outputs.elasticsearch` Allow settings extra headers for elasticsearch output
- [#15225](https://github.com/influxdata/telegraf/pull/15225) `outputs.influxdb` Add option to define local address
- [#15228](https://github.com/influxdata/telegraf/pull/15228) `outputs.influxdb_v2` Add option to set local address
- [#15475](https://github.com/influxdata/telegraf/pull/15475) `outputs.influxdb_v2` Preserve custom query parameters on write
- [#15429](https://github.com/influxdata/telegraf/pull/15429) `outputs.mqtt` Add client trace logging, resolve MQTT5 reconnect login
- [#15041](https://github.com/influxdata/telegraf/pull/15041) `outputs.postgresql` Add secret store support
- [#15073](https://github.com/influxdata/telegraf/pull/15073) `outputs.postgresql` Allow configuration of startup error handling
- [#14884](https://github.com/influxdata/telegraf/pull/14884) `outputs` Add framework to retry on startup errors
- [#14952](https://github.com/influxdata/telegraf/pull/14952) `parser.prometheusremotewrite` Parse and generate histogram buckets
- [#14961](https://github.com/influxdata/telegraf/pull/14961) `parsers.binary` Allow base64-encoded input data
- [#15328](https://github.com/influxdata/telegraf/pull/15328) `processors.parser` Add base64 decode for fields
- [#15434](https://github.com/influxdata/telegraf/pull/15434) `processors.printer` Embed Influx serializer options
- [#15170](https://github.com/influxdata/telegraf/pull/15170) `processors.starlark` Allow persistence of global state
- [#15220](https://github.com/influxdata/telegraf/pull/15220) `serializers.influx` Add option to omit timestamp
- [#14975](https://github.com/influxdata/telegraf/pull/14975) `snmp` Add secret support for auth_password and priv_password

### Bugfixes

- [#15402](https://github.com/influxdata/telegraf/pull/15402) `agent` Warn on multiple agent configuration tables seen
- [#15440](https://github.com/influxdata/telegraf/pull/15440) `inputs.cloudwatch` Add accounts when enabled
- [#15428](https://github.com/influxdata/telegraf/pull/15428) `inputs.cloudwatch` Ensure account list is larger than index
- [#15456](https://github.com/influxdata/telegraf/pull/15456) `inputs.ecs` Check for nil pointer before use
- [#15401](https://github.com/influxdata/telegraf/pull/15401) `inputs.postgresql_extensible` Use same timestamp for each gather
- [#15260](https://github.com/influxdata/telegraf/pull/15260) `inputs.procstat` Do not report dead processes as running for orphan PID files
- [#15332](https://github.com/influxdata/telegraf/pull/15332) `inputs.smartctl` Add additional fields
- [#15466](https://github.com/influxdata/telegraf/pull/15466) `processors.snmp_lookup` Return empty tag-map on error to avoid panic

### Dependency Updates

- [#15385](https://github.com/influxdata/telegraf/pull/15385) `deps` Bump cloud.google.com/go/storage from 1.40.0 to 1.41.0
- [#15446](https://github.com/influxdata/telegraf/pull/15446) `deps` Bump github.com/awnumar/memguard from 0.22.4 to 0.22.5
- [#15413](https://github.com/influxdata/telegraf/pull/15413) `deps` Bump github.com/fatih/color from 1.16.0 to 1.17.0
- [#15410](https://github.com/influxdata/telegraf/pull/15410) `deps` Bump github.com/jhump/protoreflect from 1.15.6 to 1.16.0
- [#15441](https://github.com/influxdata/telegraf/pull/15441) `deps` Bump github.com/lxc/incus v0.4.0 to v6.2.0
- [#15381](https://github.com/influxdata/telegraf/pull/15381) `deps` Bump github.com/miekg/dns from 1.1.58 to 1.1.59
- [#15444](https://github.com/influxdata/telegraf/pull/15444) `deps` Bump github.com/openzipkin/zipkin-go from 0.4.2 to 0.4.3
- [#15412](https://github.com/influxdata/telegraf/pull/15412) `deps` Bump github.com/prometheus/common from 0.52.2 to 0.53.0
- [#15362](https://github.com/influxdata/telegraf/pull/15362) `deps` Bump github.com/showwin/speedtest-go from 1.7.5 to 1.7.6
- [#15382](https://github.com/influxdata/telegraf/pull/15382) `deps` Bump github.com/showwin/speedtest-go from 1.7.6 to 1.7.7
- [#15384](https://github.com/influxdata/telegraf/pull/15384) `deps` Bump github.com/snowflakedb/gosnowflake from 1.7.2 to 1.10.0
- [#15470](https://github.com/influxdata/telegraf/pull/15470) `deps` Bump go from v1.22.3 to v1.22.4
- [#15411](https://github.com/influxdata/telegraf/pull/15411) `deps` Bump golang.org/x/crypto from 0.22.0 to 0.23.0
- [#15447](https://github.com/influxdata/telegraf/pull/15447) `deps` Bump golang.org/x/net from 0.24.0 to 0.25.0
- [#15383](https://github.com/influxdata/telegraf/pull/15383) `deps` Bump k8s.io/* from 0.29.3 to 0.30.1
- [#15445](https://github.com/influxdata/telegraf/pull/15445) `deps` Bump modernc.org/sqlite from 1.29.10 to 1.30.0
- [#15409](https://github.com/influxdata/telegraf/pull/15409) `deps` Bump modernc.org/sqlite from 1.29.5 to 1.29.10
- [#15386](https://github.com/influxdata/telegraf/pull/15386) `deps` Bump super-linter/super-linter from 6.4.1 to 6.5.0
- [#15408](https://github.com/influxdata/telegraf/pull/15408) `deps` Bump super-linter/super-linter from 6.5.0 to 6.5.1
- [#15393](https://github.com/influxdata/telegraf/pull/15393) `deps` Switch to github.com/leodido/go-syslog
- [#15403](https://github.com/influxdata/telegraf/pull/15403) `deps` Update OpenTelemetry dependencies

## v1.30.3 [2024-05-20]

### Bugfixes

- [#15213](https://github.com/influxdata/telegraf/pull/15213) `http` Stop plugins from leaking file descriptors on telegraf reload
- [#15312](https://github.com/influxdata/telegraf/pull/15312) `input.redis` Discard invalid errorstat lines
- [#15317](https://github.com/influxdata/telegraf/pull/15317) `inputs.cloudwatch` Option to produce dense metrics
- [#15259](https://github.com/influxdata/telegraf/pull/15259) `inputs.gnmi` Ensure path contains elements to avoid panic
- [#15239](https://github.com/influxdata/telegraf/pull/15239) `inputs.http_listener_v2` Wrap timestamp parsing error messages
- [#15323](https://github.com/influxdata/telegraf/pull/15323) `inputs.netflow` Log unknown fields only once
- [#15212](https://github.com/influxdata/telegraf/pull/15212) `inputs.sysstat` Prevent default sadc_interval from increasing on reload
- [#15223](https://github.com/influxdata/telegraf/pull/15223) `makefile` Use go's dependency checker for per platform builds
- [#15224](https://github.com/influxdata/telegraf/pull/15224) `outputs.graphite` Handle local address without port correctly
- [#15277](https://github.com/influxdata/telegraf/pull/15277) `outputs.loki` Option to sanitize label names
- [#15346](https://github.com/influxdata/telegraf/pull/15346) `windows` Make sure to log the final error message on exit

### Dependency Updates

- [#15262](https://github.com/influxdata/telegraf/pull/15262) `deps` Bump cloud.google.com/go/bigquery from 1.59.1 to 1.61.0
- [#15308](https://github.com/influxdata/telegraf/pull/15308) `deps` Bump github.com/Azure/azure-kusto-go from 0.15.0 to 0.15.2
- [#15203](https://github.com/influxdata/telegraf/pull/15203) `deps` Bump github.com/aliyun/alibaba-cloud-sdk-go from 1.62.713 to 1.62.721
- [#15349](https://github.com/influxdata/telegraf/pull/15349) `deps` Bump github.com/antchfx/xmlquery from 1.3.18 to 1.4.0
- [#15263](https://github.com/influxdata/telegraf/pull/15263) `deps` Bump github.com/antchfx/xpath from 1.2.5 to 1.3.0
- [#15348](https://github.com/influxdata/telegraf/pull/15348) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.27.9 to 1.27.13
- [#15202](https://github.com/influxdata/telegraf/pull/15202) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.17.9 to 1.17.11
- [#15350](https://github.com/influxdata/telegraf/pull/15350) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.151.1 to 1.161.1
- [#15307](https://github.com/influxdata/telegraf/pull/15307) `deps` Bump github.com/coocood/freecache from 1.2.3 to 1.2.4
- [#15205](https://github.com/influxdata/telegraf/pull/15205) `deps` Bump github.com/google/cel-go from 0.18.1 to 0.20.1
- [#15276](https://github.com/influxdata/telegraf/pull/15276) `deps` Bump github.com/grid-x/modbus from v0.0.0-20211113184042-7f2251c342c9 to v0.0.0-20240503115206-582f2ab60a18
- [#15347](https://github.com/influxdata/telegraf/pull/15347) `deps` Bump github.com/nats-io/nats-server/v2 from 2.10.9 to 2.10.14
- [#15310](https://github.com/influxdata/telegraf/pull/15310) `deps` Bump github.com/pion/dtls/v2 from 2.2.10 to 2.2.11
- [#15265](https://github.com/influxdata/telegraf/pull/15265) `deps` Bump github.com/prometheus/procfs from 0.13.0 to 0.14.0
- [#15272](https://github.com/influxdata/telegraf/pull/15272) `deps` Bump github.com/shirou/gopsutil/v3 from v3.24.3 to v3.24.4
- [#15264](https://github.com/influxdata/telegraf/pull/15264) `deps` Bump github.com/testcontainers/testcontainers-go/modules/kafka from 0.26.1-0.20231116140448-68d5f8983d09 to 0.30.0
- [#15351](https://github.com/influxdata/telegraf/pull/15351) `deps` Bump github.com/vmware/govmomi from 0.37.0 to 0.37.2
- [#15327](https://github.com/influxdata/telegraf/pull/15327) `deps` Bump go from v1.22.2 to v1.22.3
- [#15206](https://github.com/influxdata/telegraf/pull/15206) `deps` Bump golang.org/x/mod from 0.16.0 to 0.17.0
- [#15266](https://github.com/influxdata/telegraf/pull/15266) `deps` Bump golang.org/x/sync from 0.6.0 to 0.7.0
- [#15303](https://github.com/influxdata/telegraf/pull/15303) `deps` Bump golangci-lint from v1.57.2 to v1.58.0
- [#15309](https://github.com/influxdata/telegraf/pull/15309) `deps` Bump google.golang.org/api from 0.171.0 to 0.177.0
- [#15207](https://github.com/influxdata/telegraf/pull/15207) `deps` Bump super-linter/super-linter from 6.3.1 to 6.4.1
- [#15316](https://github.com/influxdata/telegraf/pull/15316) `deps` Migrate to maintained gopacket library

## v1.30.2 [2024-04-22]

### Important Changes

- [PR #15108](https://github.com/influxdata/telegraf/pull/15108) reverts the
  behavior of `inputs.systemd_units` back to pre-v1.30.0 to only collect units
  already loaded by systemd, i.e. not collecting disabled or static units. This
  was necessary because using unspecific filters will cause significant load on
  the system as systemd needs to read all unit-files matching the pattern in
  each gather cycle. If you use specific patterns and want to collect non-loaded
  units, please set the `collect_disabled_units` option to `true`.

### Bugfixes

- [#15054](https://github.com/influxdata/telegraf/pull/15054) `agent` Ensure import of required package for pprof support
- [#15155](https://github.com/influxdata/telegraf/pull/15155) `inputs.diskio` Update path from /sys/block to /sys/class/block
- [#15146](https://github.com/influxdata/telegraf/pull/15146) `inputs.modbus` Avoid overflow when calculating with uint16 addresses
- [#15144](https://github.com/influxdata/telegraf/pull/15144) `inputs.nvidia` Include power limit field for v11
- [#15178](https://github.com/influxdata/telegraf/pull/15178) `inputs.opcua` Make sure to always create a request
- [#15176](https://github.com/influxdata/telegraf/pull/15176) `inputs.phpfpm` Check for error before continue processing
- [#15195](https://github.com/influxdata/telegraf/pull/15195) `inputs.prometheus` Correctly handle host header
- [#15078](https://github.com/influxdata/telegraf/pull/15078) `inputs.prometheus` Remove duplicate response_timeout option
- [#15154](https://github.com/influxdata/telegraf/pull/15154) `inputs.sqlserver` Honor timezone on backup metrics
- [#15129](https://github.com/influxdata/telegraf/pull/15129) `inputs.systemd_units` Reconnect if connection is lost
- [#15108](https://github.com/influxdata/telegraf/pull/15108) `inputs.systemd_units` Revert to only gather loaded units by default
- [#15132](https://github.com/influxdata/telegraf/pull/15132) `inputs.win_eventlog` Handle empty query correctly
- [#15157](https://github.com/influxdata/telegraf/pull/15157) `outputs.opensearch` Correctly error during failures or disconnect
- [#15196](https://github.com/influxdata/telegraf/pull/15196) `outputs.sql` Enable the use of krb5 with mssql driver
- [#15168](https://github.com/influxdata/telegraf/pull/15168) `systemd` Remove 5 second timeout, use default (90 seconds)

### Dependency Updates

- [#15087](https://github.com/influxdata/telegraf/pull/15087) `deps` Bump github.com/aliyun/alibaba-cloud-sdk-go from 1.62.563 to 1.62.708
- [#15163](https://github.com/influxdata/telegraf/pull/15163) `deps` Bump github.com/aliyun/alibaba-cloud-sdk-go from 1.62.708 to 1.62.713
- [#15086](https://github.com/influxdata/telegraf/pull/15086) `deps` Bump github.com/apache/iotdb-client-go from 0.12.2-0.20220722111104-cd17da295b46 to 1.2.0-tsbs
- [#15125](https://github.com/influxdata/telegraf/pull/15125) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.36.1 to 1.37.0
- [#15164](https://github.com/influxdata/telegraf/pull/15164) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.27.1 to 1.27.4
- [#15161](https://github.com/influxdata/telegraf/pull/15161) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.25.2 to 1.25.5
- [#15162](https://github.com/influxdata/telegraf/pull/15162) `deps` Bump github.com/go-sql-driver/mysql from 1.7.1 to 1.8.1
- [#15084](https://github.com/influxdata/telegraf/pull/15084) `deps` Bump github.com/gophercloud/gophercloud from 1.9.0 to 1.11.0
- [#15126](https://github.com/influxdata/telegraf/pull/15126) `deps` Bump github.com/jackc/pgtype from 1.14.2 to 1.14.3
- [#15100](https://github.com/influxdata/telegraf/pull/15100) `deps` Bump github.com/prometheus/client_golang from 1.18.0 to 1.19.0
- [#15127](https://github.com/influxdata/telegraf/pull/15127) `deps` Bump github.com/redis/go-redis/v9 from 9.2.1 to 9.5.1
- [#15082](https://github.com/influxdata/telegraf/pull/15082) `deps` Bump github.com/shirou/gopsutil from v3.23.11 to v3.24.3
- [#15085](https://github.com/influxdata/telegraf/pull/15085) `deps` Bump github.com/testcontainers/testcontainers-go from 0.27.0 to 0.29.1
- [#15160](https://github.com/influxdata/telegraf/pull/15160) `deps` Bump github.com/vmware/govmomi from 0.33.1 to 0.37.0
- [#15193](https://github.com/influxdata/telegraf/pull/15193) `deps` Bump golang.org/x/net from 0.22.0 to 0.23.0
- [#15128](https://github.com/influxdata/telegraf/pull/15128) `deps` Bump golang.org/x/oauth2 from 0.18.0 to 0.19.0
- [#15124](https://github.com/influxdata/telegraf/pull/15124) `deps` Bump k8s.io/client-go from 0.29.2 to 0.29.3
- [#15123](https://github.com/influxdata/telegraf/pull/15123) `deps` Bump super-linter/super-linter from 6.3.0 to 6.3.1
- [#15083](https://github.com/influxdata/telegraf/pull/15083) `deps` Bump tj-actions/changed-files from 43 to 44

## v1.30.1 [2024-04-01]

### Bugfixes

- [#14966](https://github.com/influxdata/telegraf/pull/14966) `inputs.chrony` Remove chronyc dependency in documentation
- [#15003](https://github.com/influxdata/telegraf/pull/15003) `inputs.diskio` Add missing udev properties
- [#14979](https://github.com/influxdata/telegraf/pull/14979) `inputs.dns_query` Fill out additional record fields
- [#15025](https://github.com/influxdata/telegraf/pull/15025) `inputs.dns_query` Include the canonical CNAME target
- [#15007](https://github.com/influxdata/telegraf/pull/15007) `inputs.knx_listener` Ignore GroupValueRead requests
- [#14959](https://github.com/influxdata/telegraf/pull/14959) `inputs.knx_listener` Reconnect after connection loss
- [#15063](https://github.com/influxdata/telegraf/pull/15063) `inputs.mysql` Parse boolean values in metric v1 correctly
- [#15012](https://github.com/influxdata/telegraf/pull/15012) `inputs.mysql` Use correct column-types for Percona 8 user stats
- [#15023](https://github.com/influxdata/telegraf/pull/15023) `inputs.nvidia_smi` Add process info metrics
- [#14977](https://github.com/influxdata/telegraf/pull/14977) `inputs.openstack` Resolve regression in block storage and server info
- [#15036](https://github.com/influxdata/telegraf/pull/15036) `inputs.phpfpm` Add timeout for fcgi
- [#15011](https://github.com/influxdata/telegraf/pull/15011) `inputs.ping` Add option to force ipv4
- [#15021](https://github.com/influxdata/telegraf/pull/15021) `inputs.prometheus` Initialize logger of parser
- [#14996](https://github.com/influxdata/telegraf/pull/14996) `inputs.smart` Improve regexp to support flags with a plus
- [#14987](https://github.com/influxdata/telegraf/pull/14987) `inputs.systemd_units` Handle disabled multi-instance units correctly
- [#14958](https://github.com/influxdata/telegraf/pull/14958) `outputs.bigquery` Add scope to bigquery and remove timeout context
- [#14991](https://github.com/influxdata/telegraf/pull/14991) `secrets` Avoid count underflow by only counting initialized secrets
- [#15040](https://github.com/influxdata/telegraf/pull/15040) `windows` Ensure watch-config is passed to Windows service

### Dependency Updates

- [#15071](https://github.com/influxdata/telegraf/pull/15071) `deps` Bump github.com/IBM/sarama from v1.42.2 to v1.43.1
- [#15017](https://github.com/influxdata/telegraf/pull/15017) `deps` Bump github.com/aws/aws-sdk-go-v2 from 1.25.3 to 1.26.0
- [#15058](https://github.com/influxdata/telegraf/pull/15058) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.27.5 to 1.27.9
- [#15060](https://github.com/influxdata/telegraf/pull/15060) `deps` Bump github.com/aws/aws-sdk-go-v2/feature/ec2/imds from 1.15.2 to 1.16.0
- [#14969](https://github.com/influxdata/telegraf/pull/14969) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.34.2 to 1.34.3
- [#15014](https://github.com/influxdata/telegraf/pull/15014) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.149.3 to 1.151.1
- [#14971](https://github.com/influxdata/telegraf/pull/14971) `deps` Bump github.com/aws/aws-sdk-go-v2/service/sts from 1.28.2 to 1.28.4
- [#15029](https://github.com/influxdata/telegraf/pull/15029) `deps` Bump github.com/docker/docker from 25.0.0+incompatible to 25.0.5+incompatible
- [#15016](https://github.com/influxdata/telegraf/pull/15016) `deps` Bump github.com/jackc/pgtype from 1.14.0 to 1.14.2
- [#14978](https://github.com/influxdata/telegraf/pull/14978) `deps` Bump github.com/jackc/pgx/v4 from 4.18.1 to 4.18.2
- [#14968](https://github.com/influxdata/telegraf/pull/14968) `deps` Bump github.com/klauspost/compress from 1.17.6 to 1.17.7
- [#14967](https://github.com/influxdata/telegraf/pull/14967) `deps` Bump github.com/pion/dtls/v2 from 2.2.8 to 2.2.10
- [#15059](https://github.com/influxdata/telegraf/pull/15059) `deps` Bump github.com/prometheus-community/pro-bing from 0.3.0 to 0.4.0
- [#14970](https://github.com/influxdata/telegraf/pull/14970) `deps` Bump github.com/prometheus/procfs from 0.12.0 to 0.13.0
- [#15009](https://github.com/influxdata/telegraf/pull/15009) `deps` Bump github.com/stretchr/testify v1.8.4 to v1.9.0
- [#15061](https://github.com/influxdata/telegraf/pull/15061) `deps` Bump go.step.sm/crypto from 0.43.0 to 0.44.1
- [#15018](https://github.com/influxdata/telegraf/pull/15018) `deps` Bump golang.org/x/crypto from 0.20.0 to 0.21.0
- [#15015](https://github.com/influxdata/telegraf/pull/15015) `deps` Bump gonum.org/v1/gonum from 0.14.0 to 0.15.0
- [#15057](https://github.com/influxdata/telegraf/pull/15057) `deps` Bump google.golang.org/api from 0.165.0 to 0.171.0
- [#14989](https://github.com/influxdata/telegraf/pull/14989) `deps` Bump google.golang.org/protobuf from 1.32.0 to 1.33.0
- [#15013](https://github.com/influxdata/telegraf/pull/15013) `deps` Bump tj-actions/changed-files from 42 to 43

## v1.30.0 [2024-03-11]

### Deprecation Removals

This release removes the following deprecated plugins:

- `inputs.cassandra` in [#14859](https://github.com/influxdata/telegraf/pull/14859)
- `inputs.httpjson` in [#14860](https://github.com/influxdata/telegraf/pull/14860)
- `inputs.io` in [#14861](https://github.com/influxdata/telegraf/pull/14861)
- `inputs.jolokia` in [#14862](https://github.com/influxdata/telegraf/pull/14862)
- `inputs.kafka_consumer_legacy` in [#14863](https://github.com/influxdata/telegraf/pull/14863)
- `inputs.snmp_legacy` in [#14864](https://github.com/influxdata/telegraf/pull/14864)
- `inputs.tcp_listener` in [#14865](https://github.com/influxdata/telegraf/pull/14865)
- `inputs.udp_listener` in [#14866](https://github.com/influxdata/telegraf/pull/14866)
- `outputs.riemann_legacy` in [#14867](https://github.com/influxdata/telegraf/pull/14867)

Furthermore, the following deprecated plugin options are removed:

- `mountpoints` of `inputs.disk` in [#14913](https://github.com/influxdata/telegraf/pull/14913)
- `metric_buffer` of `inputs.mqtt_consumer` in [#14914](https://github.com/influxdata/telegraf/pull/14914)
- `metric_buffer` of `inputs.nats_consumer` in [#14915](https://github.com/influxdata/telegraf/pull/14915)
- `url` of `outputs.influxdb` in [#14916](https://github.com/influxdata/telegraf/pull/14916)

Replacements do exist, so please migrate your configuration in case you are
still using one of those plugins. The `telegraf config migrate` command might
be able to assist with the procedure.

### Important Changes

- The default read-timeout of `inputs.syslog` of five seconds is not a sensible
  default as the plugin will close the connection if the time between
  consecutive messages exceeds the timeout.
  [#14837](https://github.com/influxdata/telegraf/pull/14828) sets the timeout
  to infinite (i.e zero) as this is the expected behavior.
- With correctly sanitizing PostgreSQL addresses ([PR #14829](https://github.com/influxdata/telegraf/pull/14829))
  the `server` tag value for a URI-format address might change in case it
  contains spaces, backslashes or single-quotes in non-redacted parameters.

### New Plugins

- [#13739](https://github.com/influxdata/telegraf/pull/13739) `outputs.zabbix` Add Zabbix plugin
- [#14474](https://github.com/influxdata/telegraf/pull/14474) `serializers.binary` Add binary serializer
- [#14223](https://github.com/influxdata/telegraf/pull/14223) `processors.snmp_lookup` Add SNMP lookup processor

### Features

- [#14491](https://github.com/influxdata/telegraf/pull/14491) Add loongarch64 nightly and release builds
- [#14882](https://github.com/influxdata/telegraf/pull/14882) `agent` Add option to skip re-running processors after aggregators
- [#14676](https://github.com/influxdata/telegraf/pull/14676) `common.opcua` Add debug info for nodes not in server namespace
- [#14743](https://github.com/influxdata/telegraf/pull/14743) `http` Allow secrets in headers
- [#14806](https://github.com/influxdata/telegraf/pull/14806) `inputs.aerospike` Deprecate plugin
- [#14872](https://github.com/influxdata/telegraf/pull/14872) `inputs.amd_rocm_smi` Add startup_error_behavior config option
- [#14673](https://github.com/influxdata/telegraf/pull/14673) `inputs.chrony` Allow to collect additional metrics
- [#14629](https://github.com/influxdata/telegraf/pull/14629) `inputs.chrony` Remove chronyc dependency
- [#14585](https://github.com/influxdata/telegraf/pull/14585) `inputs.kafka_consumer` Mark messages that failed parsing
- [#14507](https://github.com/influxdata/telegraf/pull/14507) `inputs.kernel` Add Pressure Stall Information
- [#14764](https://github.com/influxdata/telegraf/pull/14764) `inputs.modbus` Add workaround for unusual string-byte locations
- [#14625](https://github.com/influxdata/telegraf/pull/14625) `inputs.net` Add speed metric
- [#14680](https://github.com/influxdata/telegraf/pull/14680) `inputs.nvidia_smi` Add startup_error_behavior config option
- [#14424](https://github.com/influxdata/telegraf/pull/14424) `inputs.prometheus` Add internal metrics
- [#14661](https://github.com/influxdata/telegraf/pull/14661) `inputs.prometheus` Add option to limit body length
- [#14702](https://github.com/influxdata/telegraf/pull/14702) `inputs.redfish` Allow secrets for username/password configuration
- [#14613](https://github.com/influxdata/telegraf/pull/14613) `inputs.smart` Add a device_type tag to differentiate disks behind a RAID controller
- [#14792](https://github.com/influxdata/telegraf/pull/14792) `inputs.sqlserver` Add stolen target memory ratio
- [#14814](https://github.com/influxdata/telegraf/pull/14814) `inputs.systemd_units` Allow to query unloaded/disabled units
- [#14539](https://github.com/influxdata/telegraf/pull/14539) `inputs.systemd_units` Introduce show subcommand for additional data
- [#14684](https://github.com/influxdata/telegraf/pull/14684) `inputs.win_services` Make service selection case-insensitive
- [#14628](https://github.com/influxdata/telegraf/pull/14628) `outputs.graphite` Allow to set the local address to bind
- [#14236](https://github.com/influxdata/telegraf/pull/14236) `outputs.nats` Introduce NATS Jetstream option
- [#14658](https://github.com/influxdata/telegraf/pull/14658) `outputs.nebius_cloud_monitoring` Add service configuration setting
- [#14836](https://github.com/influxdata/telegraf/pull/14836) `outputs.websocket` Allow specifying secrets in headers
- [#14870](https://github.com/influxdata/telegraf/pull/14870) `serializers.csv` Allow specifying fixed column order

### Bugfixes

- [#14840](https://github.com/influxdata/telegraf/pull/14840) `agent` Catch panics in inputs goroutine
- [#14858](https://github.com/influxdata/telegraf/pull/14858) `config` Reword error message about missing config option
- [#14874](https://github.com/influxdata/telegraf/pull/14874) `inputs.docker_log` Use correct name when matching container
- [#14951](https://github.com/influxdata/telegraf/pull/14951) `inputs.gnmi` Add option to guess path tag from subscription
- [#14953](https://github.com/influxdata/telegraf/pull/14953) `inputs.gnmi` Handle canonical field-name correctly
- [#14910](https://github.com/influxdata/telegraf/pull/14910) `inputs.netflow` Fallback to IPFIX mappings for Netflow v9
- [#14852](https://github.com/influxdata/telegraf/pull/14852) `inputs.phpfpm` Continue despite erroneous sockets
- [#14871](https://github.com/influxdata/telegraf/pull/14871) `inputs.prometheus` List namespaces only when filtering by namespace
- [#14606](https://github.com/influxdata/telegraf/pull/14606) `parsers.prometheus` Do not touch input data for protocol-buffers
- [#14880](https://github.com/influxdata/telegraf/pull/14880) `processors.override` Correct TOML tag name
- [#14937](https://github.com/influxdata/telegraf/pull/14937) `statefile` Ensure valid statefile in package

### Dependency Updates

- [#14931](https://github.com/influxdata/telegraf/pull/14931) `deps` Bump all github.com/aws/aws-sdk-go-v2 dependencies
- [#14894](https://github.com/influxdata/telegraf/pull/14894) `deps` Bump cloud.google.com/go/bigquery from 1.58.0 to 1.59.1
- [#14932](https://github.com/influxdata/telegraf/pull/14932) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.27.0 to 1.30.2
- [#14949](https://github.com/influxdata/telegraf/pull/14949) `deps` Bump github.com/cloudevents/sdk-go/v2 from 2.15.0 to 2.15.2
- [#14929](https://github.com/influxdata/telegraf/pull/14929) `deps` Bump github.com/eclipse/paho.golang from 0.20.0 to 0.21.0
- [#14892](https://github.com/influxdata/telegraf/pull/14892) `deps` Bump github.com/microsoft/go-mssqldb from 1.6.0 to 1.7.0
- [#14923](https://github.com/influxdata/telegraf/pull/14923) `deps` Bump github.com/netsampler/goflow2 from v1.3.6 to v2.1.2
- [#14895](https://github.com/influxdata/telegraf/pull/14895) `deps` Bump github.com/peterbourgon/unixtransport from 0.0.3 to 0.0.4
- [#14933](https://github.com/influxdata/telegraf/pull/14933) `deps` Bump github.com/prometheus/client_model from 0.5.0 to 0.6.0
- [#14857](https://github.com/influxdata/telegraf/pull/14857) `deps` Bump github.com/srebhan/cborquery from v0.0.0-20230626165538-38be85b82316 to v1.0.1
- [#14918](https://github.com/influxdata/telegraf/pull/14918) `deps` Bump github.com/vapourismo/knx-go from v0.0.0-20240107135439-816b70397a00 to v0.0.0-20240217175130-922a0d50c241
- [#14893](https://github.com/influxdata/telegraf/pull/14893) `deps` Bump go.mongodb.org/mongo-driver from 1.13.1 to 1.14.0
- [#14891](https://github.com/influxdata/telegraf/pull/14891) `deps` Bump golang.org/x/crypto from 0.19.0 to 0.20.0
- [#14930](https://github.com/influxdata/telegraf/pull/14930) `deps` Bump modernc.org/sqlite from 1.28.0 to 1.29.2
- [#14897](https://github.com/influxdata/telegraf/pull/14897) `deps` Bump super-linter/super-linter from 6.1.1 to 6.2.0
- [#14934](https://github.com/influxdata/telegraf/pull/14934) `deps` Bump super-linter/super-linter from 6.2.0 to 6.3.0

## v1.29.5 [2024-02-20]

### Bugfixes

- [#14669](https://github.com/influxdata/telegraf/pull/14669) `inputs.filecount` Respect symlink files with FollowSymLinks
- [#14838](https://github.com/influxdata/telegraf/pull/14838) `inputs.gnmi` Normalize path for inline origin handling
- [#14679](https://github.com/influxdata/telegraf/pull/14679) `inputs.kafka_consumer` Fix typo of msg_headers_as_tags
- [#14707](https://github.com/influxdata/telegraf/pull/14707) `inputs.postgresql_extensible` Add support for bool tags
- [#14659](https://github.com/influxdata/telegraf/pull/14659) `inputs.redfish` Resolve iLO4 fan data
- [#14665](https://github.com/influxdata/telegraf/pull/14665) `inputs.snmp_trap` Enable SHA ciphers
- [#14635](https://github.com/influxdata/telegraf/pull/14635) `inputs.vsphere` Use guest.guestId value if set for guest name
- [#14752](https://github.com/influxdata/telegraf/pull/14752) `outputs.mqtt` Retry metrics for server timeout
- [#14770](https://github.com/influxdata/telegraf/pull/14770) `processors.execd` Accept tracking metrics instead of dropping them
- [#14832](https://github.com/influxdata/telegraf/pull/14832) `processors.unpivot` Handle tracking metrics correctly
- [#14654](https://github.com/influxdata/telegraf/pull/14654) `rpm` Ensure telegraf is installed after useradd

### Dependency Updates

- [#14690](https://github.com/influxdata/telegraf/pull/14690) `deps` Bump cloud.google.com/go/bigquery from 1.57.1 to 1.58.0
- [#14772](https://github.com/influxdata/telegraf/pull/14772) `deps` Bump cloud.google.com/go/pubsub from 1.33.0 to 1.36.1
- [#14819](https://github.com/influxdata/telegraf/pull/14819) `deps` Bump cloud.google.com/go/storage from 1.36.0 to 1.38.0
- [#14688](https://github.com/influxdata/telegraf/pull/14688) `deps` Bump github.com/Azure/azure-event-hubs-go/v3 from 3.6.1 to 3.6.2
- [#14845](https://github.com/influxdata/telegraf/pull/14845) `deps` Bump github.com/DATA-DOG/go-sqlmock from 1.5.0 to 1.5.2
- [#14820](https://github.com/influxdata/telegraf/pull/14820) `deps` Bump github.com/IBM/sarama from 1.42.1 to 1.42.2
- [#14774](https://github.com/influxdata/telegraf/pull/14774) `deps` Bump github.com/awnumar/memguard from 0.22.4-0.20231204102859-fce56aae03b8 to 0.22.4
- [#14687](https://github.com/influxdata/telegraf/pull/14687) `deps` Bump github.com/cloudevents/sdk-go/v2 from 2.14.0 to 2.15.0
- [#14769](https://github.com/influxdata/telegraf/pull/14769) `deps` Bump github.com/eclipse/paho.golang from 0.11.0 to 0.20.0
- [#14775](https://github.com/influxdata/telegraf/pull/14775) `deps` Bump github.com/google/uuid from 1.5.0 to 1.6.0
- [#14686](https://github.com/influxdata/telegraf/pull/14686) `deps` Bump github.com/gopcua/opcua from 0.4.0 to 0.5.3
- [#14848](https://github.com/influxdata/telegraf/pull/14848) `deps` Bump github.com/gophercloud/gophercloud from 1.7.0 to 1.9.0
- [#14755](https://github.com/influxdata/telegraf/pull/14755) `deps` Bump github.com/gwos/tcg/sdk from v0.0.0-20220621192633-df0eac0a1a4c to v8.7.2
- [#14816](https://github.com/influxdata/telegraf/pull/14816) `deps` Bump github.com/jhump/protoreflect from 1.15.4 to 1.15.6
- [#14773](https://github.com/influxdata/telegraf/pull/14773) `deps` Bump github.com/klauspost/compress from 1.17.4 to 1.17.6
- [#14817](https://github.com/influxdata/telegraf/pull/14817) `deps` Bump github.com/miekg/dns from 1.1.57 to 1.1.58
- [#14766](https://github.com/influxdata/telegraf/pull/14766) `deps` Bump github.com/showwin/speedtest-go from 1.6.7 to 1.6.10
- [#14765](https://github.com/influxdata/telegraf/pull/14765) `deps` Bump github.com/urfave/cli/v2 from 2.25.7 to 2.27.1
- [#14818](https://github.com/influxdata/telegraf/pull/14818) `deps` Bump go.opentelemetry.io/collector/pdata from 1.0.1 to 1.1.0
- [#14768](https://github.com/influxdata/telegraf/pull/14768) `deps` Bump golang.org/x/oauth2 from 0.16.0 to 0.17.0
- [#14849](https://github.com/influxdata/telegraf/pull/14849) `deps` Bump google.golang.org/api from 0.162.0 to 0.165.0
- [#14847](https://github.com/influxdata/telegraf/pull/14847) `deps` Bump google.golang.org/grpc from 1.61.0 to 1.61.1
- [#14689](https://github.com/influxdata/telegraf/pull/14689) `deps` Bump k8s.io/apimachinery from 0.29.0 to 0.29.1
- [#14767](https://github.com/influxdata/telegraf/pull/14767) `deps` Bump k8s.io/client-go from 0.29.0 to 0.29.1
- [#14846](https://github.com/influxdata/telegraf/pull/14846) `deps` Bump k8s.io/client-go from 0.29.1 to 0.29.2
- [#14850](https://github.com/influxdata/telegraf/pull/14850) `deps` Bump super-linter/super-linter from 6.0.0 to 6.1.1
- [#14771](https://github.com/influxdata/telegraf/pull/14771) `deps` Bump tj-actions/changed-files from 41 to 42
- [#14757](https://github.com/influxdata/telegraf/pull/14757) `deps` Get rid of golang.org/x/exp and use stable versions instead
- [#14753](https://github.com/influxdata/telegraf/pull/14753) `deps` Use github.com/coreos/go-systemd/v22 instead of git version

## v1.29.4 [2024-01-31]

### Bugfixes

- [#14619](https://github.com/influxdata/telegraf/pull/14619) `inputs.snmp_trap` Handle octet strings
- [#14649](https://github.com/influxdata/telegraf/pull/14649) `inputs.temp` Fix regression in metric formats
- [#14655](https://github.com/influxdata/telegraf/pull/14655) `processors.parser` Drop tracking metrics when not carried forward

### Dependency Updates

- [#14651](https://github.com/influxdata/telegraf/pull/14651) `deps` Bump all AWS dependencies
- [#14642](https://github.com/influxdata/telegraf/pull/14642) `deps` Bump github.com/compose-spec/compose-go from 1.20.0 to 1.20.2
- [#14641](https://github.com/influxdata/telegraf/pull/14641) `deps` Bump github.com/gosnmp/gosnmp from 1.36.1 to 1.37.0
- [#14643](https://github.com/influxdata/telegraf/pull/14643) `deps` Bump github.com/microsoft/go-mssqldb from 1.5.0 to 1.6.0
- [#14644](https://github.com/influxdata/telegraf/pull/14644) `deps` Bump github.com/nats-io/nats-server/v2 from 2.10.6 to 2.10.9
- [#14640](https://github.com/influxdata/telegraf/pull/14640) `deps` Bump github.com/yuin/goldmark from 1.5.6 to 1.6.0

## v1.29.3 [2024-01-29]

### Bugfixes

- [#14627](https://github.com/influxdata/telegraf/pull/14627) `common.encoding` Remove locally-defined errors and use upstream ones
- [#14553](https://github.com/influxdata/telegraf/pull/14553) `inputs.gnmi` Refactor alias handling to prevent clipping
- [#14575](https://github.com/influxdata/telegraf/pull/14575) `inputs.temp` Recover pre-v1.22.4 temperature sensor readings
- [#14526](https://github.com/influxdata/telegraf/pull/14526) `inputs.win_perf_counters` Check errors post-collection for skip
- [#14570](https://github.com/influxdata/telegraf/pull/14570) `inputs.win_perf_counters` Ignore PdhCstatusNoInstance as well
- [#14519](https://github.com/influxdata/telegraf/pull/14519) `outputs.iotdb` Handle paths that contain illegal characters
- [#14604](https://github.com/influxdata/telegraf/pull/14604) `outputs.loki` Do not close body before reading it
- [#14582](https://github.com/influxdata/telegraf/pull/14582) `outputs.mqtt` Preserve leading slash in topic

### Dependency Updates

- [#14578](https://github.com/influxdata/telegraf/pull/14578) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.29.5 to 1.31.0
- [#14576](https://github.com/influxdata/telegraf/pull/14576) `deps` Bump github.com/aws/aws-sdk-go-v2/service/sts from 1.26.5 to 1.26.7
- [#14577](https://github.com/influxdata/telegraf/pull/14577) `deps` Bump github.com/clarify/clarify-go from 0.2.4 to 0.3.1
- [#14607](https://github.com/influxdata/telegraf/pull/14607) `deps` Bump github.com/docker/docker from 24.0.7+incompatible to 25.0.0+incompatible
- [#14545](https://github.com/influxdata/telegraf/pull/14545) `deps` Bump github.com/docker/go-connections from 0.4.0 to 0.5.0
- [#14609](https://github.com/influxdata/telegraf/pull/14609) `deps` Bump github.com/fatih/color from 1.15.0 to 1.16.0
- [#14546](https://github.com/influxdata/telegraf/pull/14546) `deps` Bump github.com/gorilla/mux from 1.8.0 to 1.8.1
- [#14562](https://github.com/influxdata/telegraf/pull/14562) `deps` Bump github.com/intel/powertelemetry from 1.0.0 to 1.0.1
- [#14611](https://github.com/influxdata/telegraf/pull/14611) `deps` Bump github.com/nats-io/nats.go from 1.31.0 to 1.32.0
- [#14544](https://github.com/influxdata/telegraf/pull/14544) `deps` Bump github.com/prometheus/common from 0.44.0 to 0.45.0
- [#14608](https://github.com/influxdata/telegraf/pull/14608) `deps` Bump github.com/testcontainers/testcontainers-go from 0.26.0 to 0.27.0
- [#14573](https://github.com/influxdata/telegraf/pull/14573) `deps` Bump github.com/vapourismo/knx-go from v0.0.0-20220829185957-fb5458a5389d to 20240107135439-816b70397a00
- [#14574](https://github.com/influxdata/telegraf/pull/14574) `deps` Bump go.opentelemetry.io/collector/pdata from 1.0.0-rcv0016 to 1.0.1
- [#14541](https://github.com/influxdata/telegraf/pull/14541) `deps` Bump go.starlark.net from go.starlark.net v0.0.0-20220328144851-d1966c6b9fcd to v0.0.0-20231121155337-90ade8b19d09
- [#14543](https://github.com/influxdata/telegraf/pull/14543) `deps` Bump k8s.io/client-go from 0.28.3 to 0.29.0
- [#14610](https://github.com/influxdata/telegraf/pull/14610) `deps` Bump modernc.org/sqlite from 1.24.0 to 1.28.0

## v1.29.2 [2024-01-08]

### Bugfixes

- [#14522](https://github.com/influxdata/telegraf/pull/14522) `common.kafka` Correctly set gssapi username/password
- [#14462](https://github.com/influxdata/telegraf/pull/14462) `inputs.phpfpm` Add pid field to differentiate metrics
- [#14489](https://github.com/influxdata/telegraf/pull/14489) `inputs.phpfpm` Use logger without causing panic
- [#14493](https://github.com/influxdata/telegraf/pull/14493) `inputs.procstat` Correctly set tags on procstat_lookup
- [#14447](https://github.com/influxdata/telegraf/pull/14447) `inputs.upsd` Add additional fields to upsd from NUT
- [#14463](https://github.com/influxdata/telegraf/pull/14463) `inputs.vsphere` Resolve occasional serverFault
- [#14458](https://github.com/influxdata/telegraf/pull/14458) `outputs.bigquery` Ignore fields containing NaN or infinity
- [#14481](https://github.com/influxdata/telegraf/pull/14481) `outputs.influxdb` Support setting Host header
- [#14481](https://github.com/influxdata/telegraf/pull/14481) `outputs.influxdb_v2` Support setting Host header
- [#14471](https://github.com/influxdata/telegraf/pull/14471) `outputs.prometheus_client` Always default to TCP
- [#14460](https://github.com/influxdata/telegraf/pull/14460) `processors.filter` Rename processors.Filter -> processors.filter
- [#14523](https://github.com/influxdata/telegraf/pull/14523) `processors.starlark` Use tracking ID to identify tracking metrics
- [#14517](https://github.com/influxdata/telegraf/pull/14517) `systemd` Allow notify access from all

### Dependency Updates

- [#14525](https://github.com/influxdata/telegraf/pull/14525) `deps` Bump collectd.org from v0.5.0 to v0.6.0
- [#14506](https://github.com/influxdata/telegraf/pull/14506) `deps` Bump github.com/Azure/azure-kusto-go from 0.13.1 to 0.15.0
- [#14483](https://github.com/influxdata/telegraf/pull/14483) `deps` Bump github.com/containerd/containerd from 1.7.7 to 1.7.11
- [#14476](https://github.com/influxdata/telegraf/pull/14476) `deps` Bump github.com/djherbis/times from 1.5.0 to 1.6.0
- [#14496](https://github.com/influxdata/telegraf/pull/14496) `deps` Bump github.com/dvsekhvalnov/jose2go from v1.5.0 to v1.5.1-0.20231206184617-48ba0b76bc88
- [#14478](https://github.com/influxdata/telegraf/pull/14478) `deps` Bump github.com/google/uuid from 1.4.0 to 1.5.0
- [#14477](https://github.com/influxdata/telegraf/pull/14477) `deps` Bump github.com/jhump/protoreflect from 1.15.3 to 1.15.4
- [#14504](https://github.com/influxdata/telegraf/pull/14504) `deps` Bump github.com/pion/dtls/v2 from 2.2.7 to 2.2.8
- [#14503](https://github.com/influxdata/telegraf/pull/14503) `deps` Bump github.com/prometheus/prometheus from 0.48.0 to 0.48.1
- [#14515](https://github.com/influxdata/telegraf/pull/14515) `deps` Bump github.com/sijms/go-ora/v2 from 2.7.18 to 2.8.4
- [#14475](https://github.com/influxdata/telegraf/pull/14475) `deps` Bump go.mongodb.org/mongo-driver from 1.12.1 to 1.13.1
- [#14480](https://github.com/influxdata/telegraf/pull/14480) `deps` Bump golang.org/x/crypto from 0.16.0 to 0.17.0
- [#14479](https://github.com/influxdata/telegraf/pull/14479) `deps` Bump golang.org/x/net from 0.17.0 to 0.19.0
- [#14505](https://github.com/influxdata/telegraf/pull/14505) `deps` Bump google.golang.org/protobuf from 1.31.1-0.20231027082548-f4a6c1f6e5c1 to 1.32.0

## v1.29.1 [2023-12-13]

### Bugfixes

- [#14443](https://github.com/influxdata/telegraf/pull/14443) `inputs.clickhouse` Omit zookeeper metrics on clickhouse cloud
- [#14430](https://github.com/influxdata/telegraf/pull/14430) `inputs.php-fpm` Parse JSON output
- [#14440](https://github.com/influxdata/telegraf/pull/14440) `inputs.procstat` Revert unintended renaming of systemd_unit option

### Dependency Updates

- [#14435](https://github.com/influxdata/telegraf/pull/14435) `deps` Bump github.com/go-ldap/ldap/v3 from 3.4.5 to 3.4.6
- [#14433](https://github.com/influxdata/telegraf/pull/14433) `deps` Bump github.com/klauspost/compress from 1.17.3 to 1.17.4
- [#14432](https://github.com/influxdata/telegraf/pull/14432) `deps` Bump github.com/openzipkin/zipkin-go from 0.4.1 to 0.4.2
- [#14431](https://github.com/influxdata/telegraf/pull/14431) `deps` Bump github.com/tidwall/gjson from 1.14.4 to 1.17.0
- [#14441](https://github.com/influxdata/telegraf/pull/14441) `deps` Update all github.com/aws/aws-sdk-go-v2 dependencies

## v1.29.0 [2023-12-11]

### Important Changes

- Removed useless, all-zero fields in `inputs.procstat`. Up to now, Telegraf
  reports the fields `cpu_time_guest`, `cpu_time_guest_nice`, `cpu_time_idle`,
  `cpu_time_irq`, `cpu_time_nice`, `cpu_time_soft_irq` and `cpu_time_steal`
  which are never set by the underlying library. As a consequence those fields
  were always zero. [#14224](https://github.com/influxdata/telegraf/pull/14224)
  removes those useless fields. In case you reference them, please adapt your
  queries!

### New Plugins

- [#13995](https://github.com/influxdata/telegraf/pull/13995) `inputs.ldap` Add LDAP input plugin supporting OpenLDAP and 389ds
- [#11958](https://github.com/influxdata/telegraf/pull/11958) `outputs.opensearch` Add OpenSearch output plugin
- [#14330](https://github.com/influxdata/telegraf/pull/14330) `processors.filter` Add filter processor plugin
- [#13657](https://github.com/influxdata/telegraf/pull/13657) `secretstores` Add systemd-credentials plugin

### Features

- [#14361](https://github.com/influxdata/telegraf/pull/14361) `agent` Allow separators for namepass and namedrop filters
- [#14062](https://github.com/influxdata/telegraf/pull/14062) `aggregators.final` Allow to specify output strategy
- [#14103](https://github.com/influxdata/telegraf/pull/14103) `common.http` Add support for connecting over unix-socket
- [#14345](https://github.com/influxdata/telegraf/pull/14345) `common.opcua` Add option to include OPC-UA DataType as a field
- [#14012](https://github.com/influxdata/telegraf/pull/14012) `config` Deprecate `fieldpass` and `fielddrop` modifiers
- [#14004](https://github.com/influxdata/telegraf/pull/14004) `input.intel_pmt` Add pci_bdf tag to uniquely identify GPUs and other peripherals
- [#14001](https://github.com/influxdata/telegraf/pull/14001) `inputs.amqp_consumer` Add secretstore support for username and password
- [#13894](https://github.com/influxdata/telegraf/pull/13894) `inputs.docker` Add disk usage
- [#14308](https://github.com/influxdata/telegraf/pull/14308) `inputs.dpdk` Add options to customize error-behavior and metric layout
- [#14207](https://github.com/influxdata/telegraf/pull/14207) `inputs.elasticsearch` Use HTTPClientConfig struct
- [#14207](https://github.com/influxdata/telegraf/pull/14207) `inputs.elasticsearch_query` Use HTTPClientConfig struct
- [#14091](https://github.com/influxdata/telegraf/pull/14091) `inputs.gnmi` Rework plugin
- [#14189](https://github.com/influxdata/telegraf/pull/14189) `inputs.http_response` Add body form config option
- [#14363](https://github.com/influxdata/telegraf/pull/14363) `inputs.intel_powerstat` Extract business logic to external library
- [#13924](https://github.com/influxdata/telegraf/pull/13924) `inputs.kafka_consumer` Add message headers as metric tags
- [#14320](https://github.com/influxdata/telegraf/pull/14320) `inputs.kafka_consumer` Add option to set metric name from message header
- [#14207](https://github.com/influxdata/telegraf/pull/14207) `inputs.kibana` Use HTTPClientConfig struct
- [#13993](https://github.com/influxdata/telegraf/pull/13993) `inputs.kube_inventory` Support filtering pods and nodes by node name
- [#13996](https://github.com/influxdata/telegraf/pull/13996) `inputs.kube_inventory` Support using kubelet to get pods data
- [#14092](https://github.com/influxdata/telegraf/pull/14092) `inputs.ldap` Collect additional fields
- [#14207](https://github.com/influxdata/telegraf/pull/14207) `inputs.logstash` Use HTTPClientConfig struct
- [#14145](https://github.com/influxdata/telegraf/pull/14145) `inputs.modbus` Add support for string fields
- [#14375](https://github.com/influxdata/telegraf/pull/14375) `inputs.nats_consumer` Add nkey-seed-file authentication
- [#13923](https://github.com/influxdata/telegraf/pull/13923) `inputs.opcua_listener` Add monitoring params
- [#14214](https://github.com/influxdata/telegraf/pull/14214) `inputs.openweathermap` Add per-city query scheme for current weather
- [#13417](https://github.com/influxdata/telegraf/pull/13417) `inputs.procstat` Obtain process information through supervisor
- [#13991](https://github.com/influxdata/telegraf/pull/13991) `inputs.rabbitmq` Add secretstore support for username and password
- [#14143](https://github.com/influxdata/telegraf/pull/14143) `inputs.redfish` Allow specifying which metrics to collect
- [#14111](https://github.com/influxdata/telegraf/pull/14111) `inputs.snmp` Hint to use source tag
- [#14172](https://github.com/influxdata/telegraf/pull/14172) `inputs.socket_listener` Add vsock support to socket listener and writer
- [#13978](https://github.com/influxdata/telegraf/pull/13978) `inputs.sql` Add Oracle driver
- [#14200](https://github.com/influxdata/telegraf/pull/14200) `inputs.sql` Add IBM Netezza driver
- [#14073](https://github.com/influxdata/telegraf/pull/14073) `inputs.win_service` Reduce required rights to GENERIC_READ
- [#14401](https://github.com/influxdata/telegraf/pull/14401) `migrations` Add migration for fieldpass and fielddrop
- [#14114](https://github.com/influxdata/telegraf/pull/14114) `migrations` Add migration for inputs.jolokia
- [#14122](https://github.com/influxdata/telegraf/pull/14122) `migrations` Add migration for inputs.kafka_consumer_legacy
- [#14123](https://github.com/influxdata/telegraf/pull/14123) `migrations` Add migration for inputs.snmp_legacy
- [#14119](https://github.com/influxdata/telegraf/pull/14119) `migrations` Add migration for inputs.tcp_listener
- [#14120](https://github.com/influxdata/telegraf/pull/14120) `migrations` Add migration for inputs.udp_listener
- [#14121](https://github.com/influxdata/telegraf/pull/14121) `migrations` Add migration for outputs.riemann_legacy
- [#14141](https://github.com/influxdata/telegraf/pull/14141) `migrations` Add option migration for inputs.disk
- [#14233](https://github.com/influxdata/telegraf/pull/14233) `migrations` Add option migration for inputs.mqtt_consumer
- [#14234](https://github.com/influxdata/telegraf/pull/14234) `migrations` Add option migration for inputs.nats_consumer
- [#14341](https://github.com/influxdata/telegraf/pull/14341) `migrations` Add option migration for outputs.influxdb
- [#14047](https://github.com/influxdata/telegraf/pull/14047) `outputs.azure_data_explorer` Set user agent string
- [#14342](https://github.com/influxdata/telegraf/pull/14342) `outputs.bigquery` Allow to add metrics in one compact table
- [#14086](https://github.com/influxdata/telegraf/pull/14086) `outputs.bigquery` Make project no longer a required field
- [#13672](https://github.com/influxdata/telegraf/pull/13672) `outputs.exec` Add ability to exec command once per metric
- [#14108](https://github.com/influxdata/telegraf/pull/14108) `outputs.prometheus_client` Support listening on vsock
- [#14172](https://github.com/influxdata/telegraf/pull/14172) `outputs.socket_writer` Add vsock support to socket listener and writer
- [#14017](https://github.com/influxdata/telegraf/pull/14017) `outputs.stackdriver` Add metric type config options
- [#14275](https://github.com/influxdata/telegraf/pull/14275) `outputs.stackdriver` Enable histogram support
- [#14136](https://github.com/influxdata/telegraf/pull/14136) `outputs.wavefront` Use common/http to configure http client
- [#13903](https://github.com/influxdata/telegraf/pull/13903) `parsers.avro` Allow connection to https schema registry
- [#13914](https://github.com/influxdata/telegraf/pull/13914) `parsers.avro` Get metric name from the message field
- [#13945](https://github.com/influxdata/telegraf/pull/13945) `parsers.avro` Support multiple modes for union handling
- [#14065](https://github.com/influxdata/telegraf/pull/14065) `processors.dedup` Add state persistence between runs
- [#13971](https://github.com/influxdata/telegraf/pull/13971) `processors.regex` Allow batch transforms using named groups
- [#13998](https://github.com/influxdata/telegraf/pull/13998) `secrets` Add unprotected secret implementation

### Bugfixes

- [#14331](https://github.com/influxdata/telegraf/pull/14331) `common.oauth` Initialize EndpointParams to avoid panic with audience settings
- [#14350](https://github.com/influxdata/telegraf/pull/14350) `inputs.http` Use correct token variable
- [#14420](https://github.com/influxdata/telegraf/pull/14420) `inputs.intel_powerstat` Fix unit tests to work on every CPU/platform
- [#14388](https://github.com/influxdata/telegraf/pull/14388) `inputs.modbus` Split large request correctly at field borders
- [#14373](https://github.com/influxdata/telegraf/pull/14373) `inputs.netflow` Handle malformed inputs gracefully
- [#14394](https://github.com/influxdata/telegraf/pull/14394) `inputs.s7comm` Reconnect if query fails
- [#14357](https://github.com/influxdata/telegraf/pull/14357) `inputs.tail` Retry opening file after permission denied
- [#14419](https://github.com/influxdata/telegraf/pull/14419) `license` Correct spelling of jmhodges/clock license
- [#14416](https://github.com/influxdata/telegraf/pull/14416) `outputs.bigquery` Correct use of auto-detected project ID
- [#14340](https://github.com/influxdata/telegraf/pull/14340) `outputs.opensearch` Expose TLS setting correctly
- [#14021](https://github.com/influxdata/telegraf/pull/14021) `outputs.opensearch` Migrate to new secrets API
- [#14232](https://github.com/influxdata/telegraf/pull/14232) `outputs.prometheus_client` Ensure v1 collector data expires promptly
- [#13961](https://github.com/influxdata/telegraf/pull/13961) `parsers.avro` Clean up Warnf error wrapping error
- [#13939](https://github.com/influxdata/telegraf/pull/13939) `parsers.avro` Attempt to read CA cert file only if filename is not empty string
- [#14351](https://github.com/influxdata/telegraf/pull/14351) `parsers.json v2` Correct wrong name of config option
- [#14344](https://github.com/influxdata/telegraf/pull/14344) `parsers.json_v2` Reset state before parsing
- [#14395](https://github.com/influxdata/telegraf/pull/14395) `processors.starlark` Avoid negative refcounts for tracking metrics
- [#14137](https://github.com/influxdata/telegraf/pull/14137) `processors.starlark` Maintain tracking information post-apply

### Dependency Updates

- [#14352](https://github.com/influxdata/telegraf/pull/14352) `deps` Bump cloud.google.com/go/bigquery from 1.56.0 to 1.57.1
- [#14324](https://github.com/influxdata/telegraf/pull/14324) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.26.0 to 1.27.2
- [#14323](https://github.com/influxdata/telegraf/pull/14323) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/monitor/armmonitor from 0.10.1 to 0.10.2
- [#14354](https://github.com/influxdata/telegraf/pull/14354) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/monitor/armmonitor from 0.10.2 to 0.11.0
- [#14355](https://github.com/influxdata/telegraf/pull/14355) `deps` Bump github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources from 1.1.1 to 1.2.0
- [#14382](https://github.com/influxdata/telegraf/pull/14382) `deps` Bump github.com/golang-jwt/jwt/v5 from 5.0.0 to 5.2.0
- [#14385](https://github.com/influxdata/telegraf/pull/14385) `deps` Bump github.com/IBM/sarama from 1.41.3 to 1.42.1
- [#14384](https://github.com/influxdata/telegraf/pull/14384) `deps` Bump github.com/influxdata/tail from 1.0.1-0.20210707231403-b283181d1fa7 to 1.0.1-0.20221130111531-19b97bffd978
- [#14383](https://github.com/influxdata/telegraf/pull/14383) `deps` Bump github.com/jackc/pgconn from 1.14.0 to 1.14.1
- [#14386](https://github.com/influxdata/telegraf/pull/14386) `deps` Bump github.com/nats-io/nats-server/v2 from 2.9.23 to 2.10.6
- [#14321](https://github.com/influxdata/telegraf/pull/14321) `deps` Bump github.com/prometheus/prometheus from 0.46.0 to 0.48.0
- [#14325](https://github.com/influxdata/telegraf/pull/14325) `deps` Bump github.com/vmware/govmomi from 0.32.0 to 0.33.1
- [#14353](https://github.com/influxdata/telegraf/pull/14353) `deps` Bump golang.org/x/text from 0.13.0 to 0.14.0
- [#14322](https://github.com/influxdata/telegraf/pull/14322) `deps` Bump k8s.io/api from 0.28.3 to 0.28.4
- [#14349](https://github.com/influxdata/telegraf/pull/14349) `deps` Point kafka dependency to IBM organization

## v1.28.5 [2023-11-15]

### Bugfixes

- [#14294](https://github.com/influxdata/telegraf/pull/14294) `inputs.ecs` Correct v4 metadata URLs
- [#14274](https://github.com/influxdata/telegraf/pull/14274) `inputs.intel_rdt` Do not fail on missing PIDs
- [#14283](https://github.com/influxdata/telegraf/pull/14283) `inputs.s7comm` Truncate strings to reported length
- [#14296](https://github.com/influxdata/telegraf/pull/14296) `parsers.json_v2` Log inner errors

### Dependency Updates

- [#14287](https://github.com/influxdata/telegraf/pull/14287) `deps` Bump github.com/gosnmp/gosnmp from 1.35.1-0.20230602062452-f30602b8dad6 to 1.36.1
- [#14286](https://github.com/influxdata/telegraf/pull/14286) `deps` Bump github.com/Masterminds/semver/v3 from 3.2.0 to 3.2.1
- [#14285](https://github.com/influxdata/telegraf/pull/14285) `deps` Bump golang.org/x/sync from 0.4.0 to 0.5.0
- [#14289](https://github.com/influxdata/telegraf/pull/14289) `deps` Bump golang.org/x/mod from 0.13.0 to 0.14.0
- [#14288](https://github.com/influxdata/telegraf/pull/14288) `deps` Bump google.golang.org/api from 0.149.0 to 0.150.0

## v1.28.4 [2023-11-13]

### Bugfixes

- [#14240](https://github.com/influxdata/telegraf/pull/14240) `config` Fix comment removal in TOML files
- [#14187](https://github.com/influxdata/telegraf/pull/14187) `inputs.cgroup` Escape backslashes in path
- [#14267](https://github.com/influxdata/telegraf/pull/14267) `inputs.disk` Add inodes_used_percent field
- [#14197](https://github.com/influxdata/telegraf/pull/14197) `inputs.ecs` Fix cgroupv2 CPU metrics
- [#14194](https://github.com/influxdata/telegraf/pull/14194) `inputs.ecs` Test for v4 metadata endpoint
- [#14262](https://github.com/influxdata/telegraf/pull/14262) `inputs.ipset` Parse lines with timeout
- [#14243](https://github.com/influxdata/telegraf/pull/14243) `inputs.mqtt_consumer` Resolve could not mark message delivered
- [#14195](https://github.com/influxdata/telegraf/pull/14195) `inputs.netflow` Fix sFlow metric timestamp
- [#14191](https://github.com/influxdata/telegraf/pull/14191) `inputs.prometheus` Read bearer token from file every time
- [#14068](https://github.com/influxdata/telegraf/pull/14068) `inputs.s7comm` Fix bit queries
- [#14241](https://github.com/influxdata/telegraf/pull/14241) `inputs.win_perf_counter` Do not rely on returned buffer size
- [#14176](https://github.com/influxdata/telegraf/pull/14176) `inputs.zfs` Parse metrics correctly on FreeBSD 14
- [#14280](https://github.com/influxdata/telegraf/pull/14280) `inputs.zfs` Support gathering metrics on zfs 2.2.0 and later
- [#14115](https://github.com/influxdata/telegraf/pull/14115) `outputs.elasticsearch` Print error status value
- [#14213](https://github.com/influxdata/telegraf/pull/14213) `outputs.timestream` Clip uint64 values
- [#14149](https://github.com/influxdata/telegraf/pull/14149) `parsers.json_v2` Prevent race condition in parse function

### Dependency Updates

- [#14253](https://github.com/influxdata/telegraf/pull/14253) `deps` Bump cloud.google.com/go/storage from 1.30.1 to 1.34.1
- [#14218](https://github.com/influxdata/telegraf/pull/14218) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.18.42 to 1.19.1
- [#14167](https://github.com/influxdata/telegraf/pull/14167) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.13.40 to 1.13.43
- [#14249](https://github.com/influxdata/telegraf/pull/14249) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.23.5 to 1.26.0
- [#14166](https://github.com/influxdata/telegraf/pull/14166) `deps` Bump github.com/antchfx/xmlquery from 1.3.17 to 1.3.18
- [#14217](https://github.com/influxdata/telegraf/pull/14217) `deps` Bump github.com/antchfx/xpath from 1.2.5-0.20230505064641-588960cceeac to 1.2.5
- [#14219](https://github.com/influxdata/telegraf/pull/14219) `deps` Bump github.com/benbjohnson/clock from 1.3.3 to 1.3.5
- [#14216](https://github.com/influxdata/telegraf/pull/14216) `deps` Bump github.com/compose-spec/compose-go from 1.16.0 to 1.20.0
- [#14211](https://github.com/influxdata/telegraf/pull/14211) `deps` Bump github.com/docker/docker from 24.0.6 to 24.0.7
- [#14164](https://github.com/influxdata/telegraf/pull/14164) `deps` Bump github.com/hashicorp/consul/api from 1.24.0 to 1.25.1
- [#14251](https://github.com/influxdata/telegraf/pull/14251) `deps` Bump github.com/hashicorp/consul/api from 1.25.1 to 1.26.1
- [#14225](https://github.com/influxdata/telegraf/pull/14225) `deps` Bump github.com/nats-io/nkeys from 0.4.5 to 0.4.6
- [#14168](https://github.com/influxdata/telegraf/pull/14168) `deps` Bump github.com/prometheus/client_golang from 1.16.0 to 1.17.0
- [#14252](https://github.com/influxdata/telegraf/pull/14252) `deps` Bump github.com/rabbitmq/amqp091-go from 1.8.1 to 1.9.0
- [#14250](https://github.com/influxdata/telegraf/pull/14250) `deps` Bump github.com/showwin/speedtest-go from 1.6.6 to 1.6.7
- [#14192](https://github.com/influxdata/telegraf/pull/14192) `deps` Bump google.golang.org/grpc from 1.58.2 to 1.58.3
- [#14165](https://github.com/influxdata/telegraf/pull/14165) `deps` Bump k8s.io/client-go from 0.28.2 to 0.28.3

## v1.28.3 [2023-10-23]

### Bugfixes

- [#14049](https://github.com/influxdata/telegraf/pull/14049) `inputs.infiniband` Handle devices without counters
- [#14105](https://github.com/influxdata/telegraf/pull/14105) `inputs.jenkins` Filter after searching sub-folders
- [#14132](https://github.com/influxdata/telegraf/pull/14132) `inputs.jolokia2_agent` Trim quotes around tags
- [#14041](https://github.com/influxdata/telegraf/pull/14041) `inputs.mqtt` Reference correct password variable
- [#14010](https://github.com/influxdata/telegraf/pull/14010) `inputs.postgresql_extensible` Restore default db name
- [#14045](https://github.com/influxdata/telegraf/pull/14045) `inputs.s7comm` Allow PDU-size to be set as config option
- [#14153](https://github.com/influxdata/telegraf/pull/14153) `inputs.vault` Use http client to handle redirects correctly
- [#14131](https://github.com/influxdata/telegraf/pull/14131) `metricpass` Use correct logic expression in benchmark
- [#14154](https://github.com/influxdata/telegraf/pull/14154) `outputs.kafka` Simplify send-error handling
- [#14135](https://github.com/influxdata/telegraf/pull/14135) `outputs.nebius_cloud_monitoring` Use correct endpoint
- [#14060](https://github.com/influxdata/telegraf/pull/14060) `outputs.redistimeseries` Handle string fields correctly
- [#14150](https://github.com/influxdata/telegraf/pull/14150) `serializers.json` Append newline for batch-serialization

### Dependency Updates

- [#14036](https://github.com/influxdata/telegraf/pull/14036) `deps` Bump github.com/apache/arrow/go/v13 from 13.0.0-git to 13.0.0
- [#14125](https://github.com/influxdata/telegraf/pull/14125) `deps` Bump github.com/google/cel-go from 0.14.1-git to 0.18.1
- [#14127](https://github.com/influxdata/telegraf/pull/14127) `deps` Bump github.com/google/go-cmp from 0.5.9 to 0.6.0
- [#14085](https://github.com/influxdata/telegraf/pull/14085) `deps` Bump github.com/jhump/protoreflect from 1.15.1 to 1.15.3
- [#14039](https://github.com/influxdata/telegraf/pull/14039) `deps` Bump github.com/klauspost/compress from 1.16.7 to 1.17.0
- [#14077](https://github.com/influxdata/telegraf/pull/14077) `deps` Bump github.com/miekg/dns from 1.1.55 to 1.1.56
- [#14124](https://github.com/influxdata/telegraf/pull/14124) `deps` Bump github.com/nats-io/nats.go from 1.28.0 to 1.31.0
- [#14146](https://github.com/influxdata/telegraf/pull/14146) `deps` Bump github.com/nats-io/nats-server/v2 from 2.9.9 to 2.9.23
- [#14037](https://github.com/influxdata/telegraf/pull/14037) `deps` Bump github.com/netsampler/goflow2 from 1.3.3 to 1.3.6
- [#14040](https://github.com/influxdata/telegraf/pull/14040) `deps` Bump github.com/signalfx/golib/v3 from 3.3.50 to 3.3.53
- [#14076](https://github.com/influxdata/telegraf/pull/14076) `deps` Bump github.com/testcontainers/testcontainers-go from 0.22.0 to 0.25.0
- [#14038](https://github.com/influxdata/telegraf/pull/14038) `deps` Bump github.com/yuin/goldmark from 1.5.4 to 1.5.6
- [#14075](https://github.com/influxdata/telegraf/pull/14075) `deps` Bump golang.org/x/mod from 0.12.0 to 0.13.0
- [#14095](https://github.com/influxdata/telegraf/pull/14095) `deps` Bump golang.org/x/net from 0.15.0 to 0.17.0
- [#14074](https://github.com/influxdata/telegraf/pull/14074) `deps` Bump golang.org/x/oauth2 from 0.11.0 to 0.13.0
- [#14078](https://github.com/influxdata/telegraf/pull/14078) `deps` Bump gonum.org/v1/gonum from 0.13.0 to 0.14.0
- [#14126](https://github.com/influxdata/telegraf/pull/14126) `deps` Bump google.golang.org/api from 0.139.0 to 0.147.0

## v1.28.2 [2023-10-02]

### Bugfixes

- [#13963](https://github.com/influxdata/telegraf/pull/13963) `inputs.cisco_telemetry_mdt` Print string message on decode failure
- [#13937](https://github.com/influxdata/telegraf/pull/13937) `inputs.exec` Clean up grandchildren processes
- [#13977](https://github.com/influxdata/telegraf/pull/13977) `inputs.intel_pmt` Handle telem devices without numa_node attribute
- [#13958](https://github.com/influxdata/telegraf/pull/13958) `inputs.jti_openconfig_telemetry` Do not block gRPC dial
- [#13997](https://github.com/influxdata/telegraf/pull/13997) `inputs.mock` Align plugin with documentation
- [#13982](https://github.com/influxdata/telegraf/pull/13982) `inputs.nfsclient` Avoid panics, better error messages
- [#13962](https://github.com/influxdata/telegraf/pull/13962) `inputs.nvidia_smi` Add legacy power readings to v12 schema
- [#14011](https://github.com/influxdata/telegraf/pull/14011) `inputs.openstack` Handle dependencies between enabled services and available endpoints
- [#13972](https://github.com/influxdata/telegraf/pull/13972) `inputs.postgresql_extensible` Restore outputaddress behavior
- [#13927](https://github.com/influxdata/telegraf/pull/13927) `inputs.smart` Remove parsing error message
- [#13915](https://github.com/influxdata/telegraf/pull/13915) `inputs.systemd_units` Add missing upstream states
- [#13930](https://github.com/influxdata/telegraf/pull/13930) `outputs.cloudwatch` Increase number of metrics per write
- [#14009](https://github.com/influxdata/telegraf/pull/14009) `outputs.stackdriver` Do not shallow copy map
- [#13931](https://github.com/influxdata/telegraf/pull/13931) `outputs.stackdriver` Drop metrics on InvalidArgument gRPC error
- [#14008](https://github.com/influxdata/telegraf/pull/14008) `parsers.json_v2` Handle optional fields properly
- [#13947](https://github.com/influxdata/telegraf/pull/13947) `processors.template` Handle tracking metrics correctly

### Dependency Updates

- [#13941](https://github.com/influxdata/telegraf/pull/13941) `deps` Bump github.com/aliyun/alibaba-cloud-sdk-go from 1.62.470 to 1.62.563
- [#13988](https://github.com/influxdata/telegraf/pull/13988) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.18.27 to 1.18.42
- [#13943](https://github.com/influxdata/telegraf/pull/13943) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.20.9 to 1.23.5
- [#13986](https://github.com/influxdata/telegraf/pull/13986) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.80.1 to 1.120.0
- [#13987](https://github.com/influxdata/telegraf/pull/13987) `deps` Bump github.com/aws/aws-sdk-go-v2/feature/ec2/imds from 1.13.8 to 1.13.11
- [#13985](https://github.com/influxdata/telegraf/pull/13985) `deps` Bump github.com/eclipse/paho.mqtt.golang from 1.4.2 to 1.4.3
- [#13989](https://github.com/influxdata/telegraf/pull/13989) `deps` Bump github.com/google/uuid from 1.3.0 to 1.3.1
- [#13942](https://github.com/influxdata/telegraf/pull/13942) `deps` Bump github.com/shirou/gopsutil/v3 from 3.23.6 to 3.23.8
- [#14022](https://github.com/influxdata/telegraf/pull/14022) `deps` Bump github.com/vmware/govmomi from 0.28.0 to 0.32.0
- [#13940](https://github.com/influxdata/telegraf/pull/13940) `deps` Bump golang.org/x/net from 0.14.0 to 0.15.0
- [#13944](https://github.com/influxdata/telegraf/pull/13944) `deps` Bump k8s.io/api from 0.28.1 to 0.28.2

## v1.28.1 [2023-09-12]

### Bugfixes

- [#13909](https://github.com/influxdata/telegraf/pull/13909) `packaging` Revert permission change on package configs
- [#13910](https://github.com/influxdata/telegraf/pull/13910) `inputs.redis` Fix password typo
- [#13907](https://github.com/influxdata/telegraf/pull/13907) `inputs.vsphere` Fix config name typo in example

## v1.28.0 [2023-09-11]

### Important Changes

- [#13791](https://github.com/influxdata/telegraf/pull/13791) `metricpass`
Removed the Python compatibility support for "not", "and", and "or" keywords.
This support was incorrectly removing these keywords from actual data. Users
should instead use the standard "!", "&&", and "||" operators.
- [#13856](https://github.com/influxdata/telegraf/pull/13856) `parsers.avro`
The avro processor will no longer create a timestamp field by default unless
explicitly provided in the parser config.
- [#13778](https://github.com/influxdata/telegraf/pull/13778) `packaging`
The default permissions on `/etc/telegraf/telegraf.conf` and
`/etc/telegraf/telegraf.d` on new installs will drop read access for other.
Updates and upgrades do not change permissions.

### New Plugins

- [#13801](https://github.com/influxdata/telegraf/pull/13801) `inputs.intel_pmt` Intel PMT
- [#13731](https://github.com/influxdata/telegraf/pull/13731) `inputs.s7comm` S7comm
- [#12747](https://github.com/influxdata/telegraf/pull/12747) `inputs.tacacs` Tacacs
- [#13785](https://github.com/influxdata/telegraf/pull/13785) `processors.split` Split metrics
- [#13621](https://github.com/influxdata/telegraf/pull/13621) `secretstores.oauth2` OAuth2 services
- [#13656](https://github.com/influxdata/telegraf/pull/13656) `serializers.template` Template based serializer

### Features

- [#13605](https://github.com/influxdata/telegraf/pull/13605) `agent` Add option to avoid filtering of global tags
- [#13774](https://github.com/influxdata/telegraf/pull/13774) `agent` Watch default config files if none specified
- [#13787](https://github.com/influxdata/telegraf/pull/13787) `cli` Add plugins subcommand to list available and deprecated
- [#13496](https://github.com/influxdata/telegraf/pull/13496) `inputs.amqp_consumer` Add support to rabbitmq stream queue
- [#13877](https://github.com/influxdata/telegraf/pull/13877) `inputs.cisco_telemetry_mdt` Add microbust support
- [#13825](https://github.com/influxdata/telegraf/pull/13825) `inputs.couchbase` Add failover metrics
- [#13452](https://github.com/influxdata/telegraf/pull/13452) `inputs.fail2ban` Allow specification of socket
- [#13754](https://github.com/influxdata/telegraf/pull/13754) `inputs.fibaro` Support HC3 device types
- [#13622](https://github.com/influxdata/telegraf/pull/13622) `inputs.http` Rework token options
- [#13610](https://github.com/influxdata/telegraf/pull/13610) `inputs.influxdb_listener` Add token based authentication
- [#13793](https://github.com/influxdata/telegraf/pull/13793) `inputs.internal` Add Go metric collection option
- [#13649](https://github.com/influxdata/telegraf/pull/13649) `inputs.jenkins` Add option for node labels as tag
- [#13709](https://github.com/influxdata/telegraf/pull/13709) `inputs.jti_openconfig_telemetry` Add keep-alive setting
- [#13728](https://github.com/influxdata/telegraf/pull/13728) `inputs.kernel` Collect KSM metrics
- [#13507](https://github.com/influxdata/telegraf/pull/13507) `inputs.modbus` Add per-metric configuration style
- [#13733](https://github.com/influxdata/telegraf/pull/13733) `inputs.nvidia_smi` Add Nvidia DCGM MIG usage values
- [#13783](https://github.com/influxdata/telegraf/pull/13783) `inputs.nvidia_smi` Add additional fields
- [#13678](https://github.com/influxdata/telegraf/pull/13678) `inputs.nvidia_smi` Support newer data schema versions
- [#13443](https://github.com/influxdata/telegraf/pull/13443) `inputs.openstack` Gather cinder services
- [#13846](https://github.com/influxdata/telegraf/pull/13846) `inputs.opentelemetry` Add configurable log record dimensions
- [#13436](https://github.com/influxdata/telegraf/pull/13436) `inputs.pgbouncer` Add show_commands to select the collected pgbouncer metrics
- [#13620](https://github.com/influxdata/telegraf/pull/13620) `inputs.postgresql_extensible` Introduce max_version for query
- [#13505](https://github.com/influxdata/telegraf/pull/13505) `inputs.procstat` Add status field
- [#13624](https://github.com/influxdata/telegraf/pull/13624) `inputs.prometheus` Always apply kubernetes label and field selectors
- [#13433](https://github.com/influxdata/telegraf/pull/13433) `inputs.ravendb` Add new disk metrics fields
- [#13727](https://github.com/influxdata/telegraf/pull/13727) `inputs.redfish` Add additional chassis tags
- [#13866](https://github.com/influxdata/telegraf/pull/13866) `inputs.redis` Add additional commandstat fields
- [#13723](https://github.com/influxdata/telegraf/pull/13723) `inputs.redis` Support of redis 6.2 ERRORSTATS
- [#13864](https://github.com/influxdata/telegraf/pull/13864) `inputs.redis_sentinel` Allow username and password
- [#13699](https://github.com/influxdata/telegraf/pull/13699) `inputs.solr` Support version 7.x to 9.3
- [#13448](https://github.com/influxdata/telegraf/pull/13448) `inputs.sqlserver` Add IsHadrEnabled server property
- [#13890](https://github.com/influxdata/telegraf/pull/13890) `inputs.vsphere` Allow to set vSAN sampling interval
- [#13720](https://github.com/influxdata/telegraf/pull/13720) `inputs.vsphere` Support explicit proxy setting
- [#13471](https://github.com/influxdata/telegraf/pull/13471) `internal` Add gather_timeouts metric
- [#13423](https://github.com/influxdata/telegraf/pull/13423) `internal` Add zstd to internal content_coding
- [#13411](https://github.com/influxdata/telegraf/pull/13411) `kafka` Set and send SASL extensions
- [#13532](https://github.com/influxdata/telegraf/pull/13532) `migrations` Add migration for inputs.httpjson
- [#13536](https://github.com/influxdata/telegraf/pull/13536) `migrations` Add migration for inputs.io
- [#13673](https://github.com/influxdata/telegraf/pull/13673) `outputs.execd` Add option for batch format
- [#13245](https://github.com/influxdata/telegraf/pull/13245) `outputs.file` Add compression
- [#13651](https://github.com/influxdata/telegraf/pull/13651) `outputs.http` Allow PATCH method
- [#13763](https://github.com/influxdata/telegraf/pull/13763) `outputs.postgresql` Add option to create time column with timezone
- [#13750](https://github.com/influxdata/telegraf/pull/13750) `outputs.postgresql` Add option to rename time column
- [#13899](https://github.com/influxdata/telegraf/pull/13899) `outputs.prometheus_client` Add secretstore support for basic_password
- [#13857](https://github.com/influxdata/telegraf/pull/13857) `outputs.wavefront` Add more auth options and update SDK
- [#13607](https://github.com/influxdata/telegraf/pull/13607) `parsers.avro` Add support for JSON format
- [#13419](https://github.com/influxdata/telegraf/pull/13419) `parsers.influx` Allow a user to set the timestamp precision
- [#13506](https://github.com/influxdata/telegraf/pull/13506) `parsers.value` Add support for automatic fallback for numeric types
- [#13480](https://github.com/influxdata/telegraf/pull/13480) `parsers.xpath` Add Concise Binary Object Representation parser
- [#13690](https://github.com/influxdata/telegraf/pull/13690) `parsers.xpath` Add option to store fields as base64
- [#13553](https://github.com/influxdata/telegraf/pull/13553) `processors.parser` Allow also non-string fields
- [#13606](https://github.com/influxdata/telegraf/pull/13606) `processors.template` Unify template metric
- [#13874](https://github.com/influxdata/telegraf/pull/13874) `prometheus` Allow to specify metric type

### Bugfixes

- [#13849](https://github.com/influxdata/telegraf/pull/13849) Change the systemd KillMode from control-group to mixed
- [#13777](https://github.com/influxdata/telegraf/pull/13777) `inputs.amqp_consumer` Print error on connection failure
- [#13886](https://github.com/influxdata/telegraf/pull/13886) `inputs.kafka_consumer` Use per-message parser to avoid races
- [#13840](https://github.com/influxdata/telegraf/pull/13840) `inputs.opcua` Verify groups or root nodes included in config
- [#13602](https://github.com/influxdata/telegraf/pull/13602) `inputs.postgresql` Fix default database definition
- [#13779](https://github.com/influxdata/telegraf/pull/13779) `inputs.procstat` Collect swap via /proc/$pid/smaps
- [#13870](https://github.com/influxdata/telegraf/pull/13870) `inputs.sqlserver` Cast max_size to bigint
- [#13833](https://github.com/influxdata/telegraf/pull/13833) `inputs.sysstat` Remove tmpfile to avoid file-descriptor leak
- [#13791](https://github.com/influxdata/telegraf/pull/13791) `metricpass` Remove python logic compatibility
- [#13875](https://github.com/influxdata/telegraf/pull/13875) `outputs.sql` Move conversion_style config option to the right place
- [#13856](https://github.com/influxdata/telegraf/pull/13856) `parsers.avro` Do not force addition of timestamp as a field
- [#13855](https://github.com/influxdata/telegraf/pull/13855) `parsers.avro` Handle timestamp format checking correctly
- [#13865](https://github.com/influxdata/telegraf/pull/13865) `sql` Allow sqlite on Windows (amd64 and arm64)

### Dependency Updates

- [#13808](https://github.com/influxdata/telegraf/pull/13808) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.18.2 to 1.18.5
- [#13811](https://github.com/influxdata/telegraf/pull/13811) `deps` Bump github.com/hashicorp/consul/api from 1.20.0 to 1.24.0
- [#13809](https://github.com/influxdata/telegraf/pull/13809) `deps` Bump github.com/nats-io/nats.go from 1.27.0 to 1.28.0
- [#13765](https://github.com/influxdata/telegraf/pull/13765) `deps` Bump github.com/prometheus/prometheus from 0.42.0 to 0.46.0
- [#13895](https://github.com/influxdata/telegraf/pull/13895) `deps` Bump github.com/showwin/speedtest-go from 1.6.2 to 1.6.6
- [#13810](https://github.com/influxdata/telegraf/pull/13810) `deps` Bump k8s.io/api from 0.27.4 to 0.28.1

## v1.27.4 [2023-08-21]

### Bugfixes

- [#13693](https://github.com/influxdata/telegraf/pull/13693) `inputs.cisco_telemetry_mdt` Fix MDT source field overwrite
- [#13682](https://github.com/influxdata/telegraf/pull/13682) `inputs.opcua` Register node IDs again on reconnect
- [#13742](https://github.com/influxdata/telegraf/pull/13742) `inputs.opcua_listener` Avoid segfault when subscription was not successful
- [#13745](https://github.com/influxdata/telegraf/pull/13745) `outputs.stackdriver` Regenerate time interval for unknown metrics
- [#13719](https://github.com/influxdata/telegraf/pull/13719) `parsers.xpath` Handle protobuf maps correctly
- [#13722](https://github.com/influxdata/telegraf/pull/13722) `serializers.nowmetric` Add option for JSONv2 format

### Dependency Updates

- [#13766](https://github.com/influxdata/telegraf/pull/13766) `deps` Bump cloud.google.com/go/pubsub from 1.32.0 to 1.33.0
- [#13767](https://github.com/influxdata/telegraf/pull/13767) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.13.26 to 1.13.32
- [#13703](https://github.com/influxdata/telegraf/pull/13703) `deps` Bump github.com/aws/aws-sdk-go-v2/feature/ec2/imds from 1.13.4 to 1.13.7
- [#13702](https://github.com/influxdata/telegraf/pull/13702) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.17.14 to 1.18.0
- [#13769](https://github.com/influxdata/telegraf/pull/13769) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.18.0 to 1.18.2
- [#13734](https://github.com/influxdata/telegraf/pull/13734) `deps` Bump github.com/aws/aws-sdk-go-v2/service/sts from 1.19.3 to 1.21.2
- [#13735](https://github.com/influxdata/telegraf/pull/13735) `deps` Bump github.com/gophercloud/gophercloud from 1.2.0 to 1.5.0
- [#13737](https://github.com/influxdata/telegraf/pull/13737) `deps` Bump github.com/microsoft/go-mssqldb from 1.3.1-0.20230630170514-78ad89164253 to 1.5.0
- [#13768](https://github.com/influxdata/telegraf/pull/13768) `deps` Bump github.com/miekg/dns from 1.1.51 to 1.1.55
- [#13706](https://github.com/influxdata/telegraf/pull/13706) `deps` Bump github.com/openconfig/gnmi from 0.9.1 to 0.10.0
- [#13705](https://github.com/influxdata/telegraf/pull/13705) `deps` Bump github.com/santhosh-tekuri/jsonschema/v5 from 5.3.0 to 5.3.1
- [#13736](https://github.com/influxdata/telegraf/pull/13736) `deps` Bump go.mongodb.org/mongo-driver from 1.11.6 to 1.12.1
- [#13738](https://github.com/influxdata/telegraf/pull/13738) `deps` Bump golang.org/x/oauth2 from 0.10.0 to 0.11.0
- [#13704](https://github.com/influxdata/telegraf/pull/13704) `deps` Bump google.golang.org/api from 0.129.0 to 0.134.0

## v1.27.3 [2023-07-31]

### Bugfixes

- [#13614](https://github.com/influxdata/telegraf/pull/13614) `agent` Respect processor order in file
- [#13675](https://github.com/influxdata/telegraf/pull/13675) `config` Handle escaping and quotation correctly
- [#13671](https://github.com/influxdata/telegraf/pull/13671) `config` Setup logger for secret-stores
- [#13646](https://github.com/influxdata/telegraf/pull/13646) `inputs.docker` Add restart count
- [#13647](https://github.com/influxdata/telegraf/pull/13647) `inputs.jti_openconfig_telemetry` Reauthenticate connection on reconnect
- [#13663](https://github.com/influxdata/telegraf/pull/13663) `inputs.mqtt_consumer` Add client trace logs via option
- [#13629](https://github.com/influxdata/telegraf/pull/13629) `inputs.prometheus` Do not collect metrics from finished pods
- [#13627](https://github.com/influxdata/telegraf/pull/13627) `inputs.prometheus` Fix missing metrics when multiple plugin instances specified
- [#13597](https://github.com/influxdata/telegraf/pull/13597) `outputs.nebius_cloud_monitoring` Replace reserved label names
- [#13292](https://github.com/influxdata/telegraf/pull/13292) `outputs.opentelemetry` Group metrics by age and timestamp
- [#13575](https://github.com/influxdata/telegraf/pull/13575) `outputs.stackdriver` Add tag as resource label option
- [#13662](https://github.com/influxdata/telegraf/pull/13662) `parsers.xpath` Ensure precedence of explicitly defined tags and fields
- [#13665](https://github.com/influxdata/telegraf/pull/13665) `parsers.xpath` Fix field-names for arrays of simple types
- [#13660](https://github.com/influxdata/telegraf/pull/13660) `parsers.xpath` Improve handling of complex-type nodes
- [#13604](https://github.com/influxdata/telegraf/pull/13604) `tools.custom_builder` Ignore non-plugin sections during configuration

### Dependency Updates

- [#13668](https://github.com/influxdata/telegraf/pull/13668) `deps` Bump github.com/aliyun/alibaba-cloud-sdk-go 1.62.389 to 1.62.470
- [#13640](https://github.com/influxdata/telegraf/pull/13640) `deps` Bump github.com/antchfx/jsonquery from 1.3.1 to 1.3.2
- [#13639](https://github.com/influxdata/telegraf/pull/13639) `deps` Bump github.com/antchfx/xmlquery from 1.3.15 to 1.3.17
- [#13679](https://github.com/influxdata/telegraf/pull/13679) `deps` Bump github.com/antchfx/xpath from v1.2.4 to latest master
- [#13589](https://github.com/influxdata/telegraf/pull/13589) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.17.3 to 1.20.0
- [#13669](https://github.com/influxdata/telegraf/pull/13669) `deps` Bump github.com/aws/aws-sdk-go-v2/service/sts from 1.19.2 to 1.19.3
- [#13670](https://github.com/influxdata/telegraf/pull/13670) `deps` Bump github.com/eclipse/paho.golang from 0.10.0 to 0.11.0
- [#13588](https://github.com/influxdata/telegraf/pull/13588) `deps` Bump github.com/go-ldap/ldap/v3 from 3.4.4 to 3.4.5
- [#13603](https://github.com/influxdata/telegraf/pull/13603) `deps` Bump github.com/jaegertracing/jaeger from 1.38.0 to 1.47.0
- [#13586](https://github.com/influxdata/telegraf/pull/13586) `deps` Bump github.com/opensearch-project/opensearch-go/v2 from 2.2.0 to 2.3.0
- [#13585](https://github.com/influxdata/telegraf/pull/13585) `deps` Bump github.com/prometheus-community/pro-bing from 0.2.0 to 0.3.0
- [#13666](https://github.com/influxdata/telegraf/pull/13666) `deps` Bump github.com/shirou/gopsutil/v3 from 3.23.5 to 3.23.6
- [#13638](https://github.com/influxdata/telegraf/pull/13638) `deps` Bump github.com/thomasklein94/packer-plugin-libvirt from 0.3.4 to 0.5.0
- [#13667](https://github.com/influxdata/telegraf/pull/13667) `deps` Bump k8s.io/api from 0.27.2 to 0.27.4
- [#13587](https://github.com/influxdata/telegraf/pull/13587) `deps` Bump k8s.io/apimachinery from 0.27.2 to 0.27.3
- [#13641](https://github.com/influxdata/telegraf/pull/13641) `deps` Bump modernc.org/sqlite from 1.23.1 to 1.24.0

## v1.27.2 [2023-07-10]

### Bugfixes

- [#13570](https://github.com/influxdata/telegraf/pull/13570) `config` Replace environment variables if existing but empty
- [#13525](https://github.com/influxdata/telegraf/pull/13525) `inputs.cloud_pubsub` Properly lock for decompression
- [#13517](https://github.com/influxdata/telegraf/pull/13517) `inputs.gnmi` Add option to explicitly trim field-names
- [#13497](https://github.com/influxdata/telegraf/pull/13497) `inputs.internet_speed` Add location as a field
- [#13485](https://github.com/influxdata/telegraf/pull/13485) `inputs.modbus` Check number of register for datatype
- [#13486](https://github.com/influxdata/telegraf/pull/13486) `inputs.modbus` Fix optimization of overlapping requests and add warning
- [#13478](https://github.com/influxdata/telegraf/pull/13478) `inputs.mqtt_consumer` Correctly handle semaphores on messages
- [#13574](https://github.com/influxdata/telegraf/pull/13574) `inputs.mqtt_consumer` Print warning on no metrics generated
- [#13514](https://github.com/influxdata/telegraf/pull/13514) `inputs.opcua` Ensure connection after reconnect
- [#13495](https://github.com/influxdata/telegraf/pull/13495) `inputs.phpfpm` Check address length to avoid crash
- [#13542](https://github.com/influxdata/telegraf/pull/13542) `inputs.snmp_trap` Copy GoSNMP global defaults to prevent side-effects
- [#13557](https://github.com/influxdata/telegraf/pull/13557) `inputs.vpshere` Compare versions as a string
- [#13527](https://github.com/influxdata/telegraf/pull/13527) `outputs.graphite` Rework connection handling
- [#13562](https://github.com/influxdata/telegraf/pull/13562) `outputs.influxdb_v2` Expose HTTP/2 client timeouts
- [#13454](https://github.com/influxdata/telegraf/pull/13454) `outputs.stackdriver` Options to use official path and types
- [#13522](https://github.com/influxdata/telegraf/pull/13522) `outputs.sumologic` Unwrap serializer for type check
- [#13547](https://github.com/influxdata/telegraf/pull/13547) `parsers.binary` Fix binary parser example in README.md
- [#13526](https://github.com/influxdata/telegraf/pull/13526) `parsers.grok` Use UTC as the default timezone
- [#13550](https://github.com/influxdata/telegraf/pull/13550) `parsers.xpath` Handle explicitly defined fields correctly
- [#13564](https://github.com/influxdata/telegraf/pull/13564) `processors.printer` Convert output to string
- [#13489](https://github.com/influxdata/telegraf/pull/13489) `secretstores` Skip dbus connection with kwallet
- [#13511](https://github.com/influxdata/telegraf/pull/13511) `serializers.splunkmetric` Fix TOML option name for multi-metric
- [#13563](https://github.com/influxdata/telegraf/pull/13563) `tools.custom_builder` Error out for unknown plugins in configuration

### Dependency Updates

- [#13524](https://github.com/influxdata/telegraf/pull/13524) Replace github.com/denisenkom/go-mssqldb with github.com/microsoft/go-mssqldb
- [#13501](https://github.com/influxdata/telegraf/pull/13501) `deps` Bump cloud.google.com/go/bigquery from 1.51.1 to 1.52.0
- [#13500](https://github.com/influxdata/telegraf/pull/13500) `deps` Bump github.com/aliyun/alibaba-cloud-sdk-go from 1.62.337 to 1.62.389
- [#13504](https://github.com/influxdata/telegraf/pull/13504) `deps` Bump github.com/aws/aws-sdk-go-v2/config from 1.18.8 to 1.18.27
- [#13537](https://github.com/influxdata/telegraf/pull/13537) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.17.8 to 1.17.14
- [#13509](https://github.com/influxdata/telegraf/pull/13509) `deps` Bump github.com/gopcua/opcua from 0.3.7 to 0.4.0
- [#13502](https://github.com/influxdata/telegraf/pull/13502) `deps` Bump github.com/prometheus/client_golang from 1.15.1 to 1.16.0
- [#13544](https://github.com/influxdata/telegraf/pull/13544) `deps` Bump github.com/snowflakedb/gosnowflake from 1.6.13 to 1.6.22
- [#13541](https://github.com/influxdata/telegraf/pull/13541) `deps` Bump github.com/urfave/cli/v2 from 2.25.5 to 2.25.7
- [#13538](https://github.com/influxdata/telegraf/pull/13538) `deps` Bump golang.org/x/text from 0.9.0 to 0.10.0
- [#13554](https://github.com/influxdata/telegraf/pull/13554) `deps` Bump golang.org/x/text from 0.10.0 to 0.11.0
- [#13540](https://github.com/influxdata/telegraf/pull/13540) `deps` Bump google.golang.org/api from 0.126.0 to 0.129.0

## v1.27.1 [2023-06-21]

### Bugfixes

- [#13434](https://github.com/influxdata/telegraf/pull/13434) Handle compression level correctly for different algorithms
- [#13457](https://github.com/influxdata/telegraf/pull/13457) `config` Restore old environment var behavior with option
- [#13446](https://github.com/influxdata/telegraf/pull/13446) `custom_builder` Correctly handle serializers and parsers

### Dependency Updates

- [#13469](https://github.com/influxdata/telegraf/pull/13469) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.13.20 to 1.13.26
- [#13468](https://github.com/influxdata/telegraf/pull/13468) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.9 to 1.26.2
- [#13465](https://github.com/influxdata/telegraf/pull/13465) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.16.0 to 1.17.2
- [#13466](https://github.com/influxdata/telegraf/pull/13466) `deps` Bump github.com/go-sql-driver/mysql from 1.6.0 to 1.7.1
- [#13427](https://github.com/influxdata/telegraf/pull/13427) `deps` Bump github.com/jackc/pgx/v4 from 4.17.1 to 4.18.1
- [#13429](https://github.com/influxdata/telegraf/pull/13429) `deps` Bump github.com/nats-io/nats.go from 1.24.0 to 1.27.0
- [#13467](https://github.com/influxdata/telegraf/pull/13467) `deps` Bump github.com/prometheus-community/pro-bing from 0.1.0 to 0.2.0
- [#13428](https://github.com/influxdata/telegraf/pull/13428) `deps` Bump golang.org/x/crypto from 0.8.0 to 0.9.0
- [#13431](https://github.com/influxdata/telegraf/pull/13431) `deps` Bump golang.org/x/term from 0.8.0 to 0.9.0
- [#13430](https://github.com/influxdata/telegraf/pull/13430) `deps` Bump modernc.org/sqlite from 1.21.0 to 1.23.1

## v1.27.0 [2023-06-12]

### Important Changes

- Fix parsing of timezone abbreviations such as `MST`. Up to now, when parsing
  times with abbreviated timezones (i.e. the format ) the timezone information
  is ignored completely and the _timestamp_ is located in UTC. This is a golang
  issue (see [#9617](https://github.com/golang/go/issues/9617) or
  [#56528](https://github.com/golang/go/issues/56528)). If you worked around
  that issue, please remove the workaround before using v1.27+. In case you
  experience issues with abbreviated timezones please file an issue!
- Removal of old-style parser creation. This should not directly affect users as
  it is an API change. All parsers in Telegraf are already ported to the new
  framework. If you experience any issues with not being able to create parsers
  let us know!

### New Plugins

- [#11155](https://github.com/influxdata/telegraf/pull/11155) `inputs.ctrlx_datalayer` ctrlX Data Layer
- [#13397](https://github.com/influxdata/telegraf/pull/13397) `inputs.intel_baseband` Intel Baseband Accelerator
- [#13220](https://github.com/influxdata/telegraf/pull/13220) `outputs.clarify` Clarify
- [#13379](https://github.com/influxdata/telegraf/pull/13379) `outputs.nebius_cloud_monitoring` Nebius Cloud Monitoring
- [#13061](https://github.com/influxdata/telegraf/pull/13061) `processors.scale` Scale
- [#13035](https://github.com/influxdata/telegraf/pull/13035) `secretstores.docker` Docker Store
- [#13150](https://github.com/influxdata/telegraf/pull/13150) `secretstores.http` HTTP Store
- [#13224](https://github.com/influxdata/telegraf/pull/13224) `serializers.cloudevents` CloudEvents

### Features

- [#13144](https://github.com/influxdata/telegraf/pull/13144) Add common expression language metric filtering
- [#13364](https://github.com/influxdata/telegraf/pull/13364) `agent` Add option to avoid filtering of explicit plugin tags
- [#13118](https://github.com/influxdata/telegraf/pull/13118) `aggregators.basicstats` Add percentage change
- [#13094](https://github.com/influxdata/telegraf/pull/13094) `cloud_pubsub` Add support for gzip compression
- [#12863](https://github.com/influxdata/telegraf/pull/12863) `common.opcua` Add support for secret-store secrets
- [#13262](https://github.com/influxdata/telegraf/pull/13262) `common.tls` Add support for passphrase-protected private key
- [#13377](https://github.com/influxdata/telegraf/pull/13377) `config` Add framework for migrating deprecated plugins
- [#13229](https://github.com/influxdata/telegraf/pull/13229) `config` Support shell like syntax for environment variable substitution
- [#12448](https://github.com/influxdata/telegraf/pull/12448) `inputs.cloudwatch` Add support for cross account observability
- [#13089](https://github.com/influxdata/telegraf/pull/13089) `inputs.directory_monitor` Improve internal stats
- [#13163](https://github.com/influxdata/telegraf/pull/13163) `inputs.filecount` Add oldestFileTimestamp and newestFileTimestamp
- [#13326](https://github.com/influxdata/telegraf/pull/13326) `inputs.gnmi` Allow canonical field names
- [#13116](https://github.com/influxdata/telegraf/pull/13116) `inputs.gnmi` Support Juniper GNMI Extension Header
- [#12797](https://github.com/influxdata/telegraf/pull/12797) `inputs.internet_speed` Support multi-server test
- [#11831](https://github.com/influxdata/telegraf/pull/11831) `inputs.kafka_consumer` Add regular expression support for topics
- [#13040](https://github.com/influxdata/telegraf/pull/13040) `inputs.kubernetes` Extend kube_inventory plugin to include and extend resource quota, secret, node, and pod measurement
- [#13293](https://github.com/influxdata/telegraf/pull/13293) `inputs.nats_consumer` Add receiver subject as tag
- [#13047](https://github.com/influxdata/telegraf/pull/13047) `inputs.netflow` Add sFlow decoder
- [#13360](https://github.com/influxdata/telegraf/pull/13360) `inputs.netflow` Allow custom PEN field mappings
- [#13133](https://github.com/influxdata/telegraf/pull/13133) `inputs.nvidia_smi` Add additional memory related fields
- [#13404](https://github.com/influxdata/telegraf/pull/13404) `inputs.opentelemetry` Add configurable span dimensions
- [#12851](https://github.com/influxdata/telegraf/pull/12851) `inputs.prometheus` Control which pod metadata is added as tags
- [#13289](https://github.com/influxdata/telegraf/pull/13289) `inputs.sql` Add disconnected_servers_behavior field in the configuration
- [#13091](https://github.com/influxdata/telegraf/pull/13091) `inputs.sql` Add FlightSQL support
- [#13261](https://github.com/influxdata/telegraf/pull/13261) `inputs.sqlserver` Add Azure Arc-enabled SQL MI support
- [#13284](https://github.com/influxdata/telegraf/pull/13284) `inputs.sqlserver` Check SQL Server encryptionEnforce with xp_instance_regread
- [#13087](https://github.com/influxdata/telegraf/pull/13087) `inputs.statsd` Add optional temporality and start_time tag for statsd metrics
- [#13048](https://github.com/influxdata/telegraf/pull/13048) `inputs.suricata` Add ability to parse drop or rejected
- [#11955](https://github.com/influxdata/telegraf/pull/11955) `inputs.vsphere` Add vSAN extension
- [#13316](https://github.com/influxdata/telegraf/pull/13316) `internal` Add additional faster compression options
- [#13157](https://github.com/influxdata/telegraf/pull/13157) `outputs.loki` Add option for metric name label
- [#13349](https://github.com/influxdata/telegraf/pull/13349) `outputs.wavefront` Add TLS and HTTP Timeout configuration fields
- [#13167](https://github.com/influxdata/telegraf/pull/13167) `parsers.opentsdb` Add OpenTSDB data format parser
- [#13075](https://github.com/influxdata/telegraf/pull/13075) `processors.aws_ec2` Add caching of imds and ec2 tags
- [#13147](https://github.com/influxdata/telegraf/pull/13147) `processors.parser` Add merge with timestamp option
- [#13227](https://github.com/influxdata/telegraf/pull/13227) `processors.scale`  Add scaling by factor and offset
- [#13253](https://github.com/influxdata/telegraf/pull/13253) `processors.template` Allow `tag` to be a template
- [#12971](https://github.com/influxdata/telegraf/pull/12971) `serializer.prometheusremote` Improve performance
- [#13275](https://github.com/influxdata/telegraf/pull/13275) `test` Allow to capture all messages during test

### Bugfixes

- [#13238](https://github.com/influxdata/telegraf/pull/13238) `inputs.cloud_pubsub` Fix gzip decompression
- [#13304](https://github.com/influxdata/telegraf/pull/13304) `inputs.gnmi` Allow optional origin for update path
- [#13332](https://github.com/influxdata/telegraf/pull/13332) `inputs.gnmi` Handle canonical field-name correctly for non-explicit subscriptions
- [#13350](https://github.com/influxdata/telegraf/pull/13350) `inputs.mqtt` ACK messages when persistence is enabled
- [#13361](https://github.com/influxdata/telegraf/pull/13361) `inputs.mysql` Update MariaDB Dialect regex version check
- [#13325](https://github.com/influxdata/telegraf/pull/13325) `inputs.netflow` Fix field mappings
- [#13320](https://github.com/influxdata/telegraf/pull/13320) `inputs.netflow` Handle PEN messages correctly
- [#13231](https://github.com/influxdata/telegraf/pull/13231) `inputs.prometheus` Avoid race when creating informer factory
- [#13288](https://github.com/influxdata/telegraf/pull/13288) `inputs.socket_listener` Avoid noisy logs on closed connection
- [#13307](https://github.com/influxdata/telegraf/pull/13307) `inputs.temp` Ignore warnings and instead return only errors
- [#13412](https://github.com/influxdata/telegraf/pull/13412) `inputs.upsd` Handle float battery.runtime value
- [#13363](https://github.com/influxdata/telegraf/pull/13363) `internal` Fix time parsing for abbreviated timezones
- [#13408](https://github.com/influxdata/telegraf/pull/13408) `outputs.sql` Use config.duration to correctly to parse toml config
- [#13252](https://github.com/influxdata/telegraf/pull/13252) `outputs.wavefront` Flush metric buffer before reaching overflow
- [#13301](https://github.com/influxdata/telegraf/pull/13301) `processors.lookup` Do not strip tracking info
- [#13164](https://github.com/influxdata/telegraf/pull/13164) `serializers.influx` Restore disabled uint support by default
- [#13394](https://github.com/influxdata/telegraf/pull/13394) `tests` Replace last 'cat' instance in tests

### Dependency Updates

- [#13359](https://github.com/influxdata/telegraf/pull/13359) `deps` Bump cloud.google.com/go/monitoring from 1.13.0 to 1.14.0
- [#13312](https://github.com/influxdata/telegraf/pull/13312) `deps` Bump github.com/aliyun/alibaba-cloud-sdk-go from 1.62.193 to 1.62.337
- [#13390](https://github.com/influxdata/telegraf/pull/13390) `deps` Bump github.com/aws/aws-sdk-go-v2/feature/ec2/imds from 1.13.2 to 1.13.3
- [#13391](https://github.com/influxdata/telegraf/pull/13391) `deps` Bump github.com/aws/aws-sdk-go-v2/service/sts from 1.18.9 to 1.19.0
- [#13313](https://github.com/influxdata/telegraf/pull/13313) `deps` Bump github.com/Azure/azure-event-hubs-go/v3 from 3.4.0 to 3.5.0
- [#13314](https://github.com/influxdata/telegraf/pull/13314) `deps` Bump github.com/Azure/go-autorest/autorest from 0.11.28 to 0.11.29
- [#13265](https://github.com/influxdata/telegraf/pull/13265) `deps` Bump github.com/influxdata/influxdb-observability libraries from 0.3.3 to 0.3.15
- [#13311](https://github.com/influxdata/telegraf/pull/13311) `deps` Bump github.com/jackc/pgconn from 1.13.0 to 1.14.0
- [#13357](https://github.com/influxdata/telegraf/pull/13357) `deps` Bump github.com/jackc/pgtype from 1.12.0 to 1.14.0
- [#13392](https://github.com/influxdata/telegraf/pull/13392) `deps` Bump github.com/Mellanox/rdmamap to 1.1.0
- [#13356](https://github.com/influxdata/telegraf/pull/13356) `deps` Bump github.com/pion/dtls/v2 from 2.2.6 to 2.2.7
- [#13389](https://github.com/influxdata/telegraf/pull/13389) `deps` Bump github.com/prometheus/common from 0.43.0 to 0.44.0
- [#13355](https://github.com/influxdata/telegraf/pull/13355) `deps` Bump github.com/rabbitmq/amqp091-go from 1.8.0 to 1.8.1
- [#13396](https://github.com/influxdata/telegraf/pull/13396) `deps` Bump github.com/shirou/gopsutil from 3.23.4 to 3.23.5
- [#13369](https://github.com/influxdata/telegraf/pull/13369) `deps` Bump github.com/showwin/speedtest-go from 1.5.2 to 1.6.2
- [#13388](https://github.com/influxdata/telegraf/pull/13388) `deps` Bump github.com/urfave/cli/v2 from 2.23.5 to 2.25.5
- [#13315](https://github.com/influxdata/telegraf/pull/13315) `deps` Bump k8s.io/client-go from 0.26.2 to 0.27.2

## v1.26.3 [2023-05-22]

### Bugfixes

- [#13149](https://github.com/influxdata/telegraf/pull/13149) `inputs.gnmi` Create selfstat to track connection state
- [#13139](https://github.com/influxdata/telegraf/pull/13139) `inputs.intel_pmu` Fix handling of the json perfmon format
- [#13056](https://github.com/influxdata/telegraf/pull/13056) `inputs.socket_listener` Fix loss of connection tracking
- [#13300](https://github.com/influxdata/telegraf/pull/13300) `inputs.socket_listener` Fix race in tests
- [#13286](https://github.com/influxdata/telegraf/pull/13286) `inputs.vsphere` Specify the correct option for disconnected_servers_behavior
- [#13239](https://github.com/influxdata/telegraf/pull/13239) `outputs.graphite` Fix logic to reconnect with servers that were not up on agent startup
- [#13169](https://github.com/influxdata/telegraf/pull/13169) `outputs.prometheus_client` Fix export_timestamp for v1 metric type
- [#13168](https://github.com/influxdata/telegraf/pull/13168) `outputs.stackdriver` Allow for custom metric type prefix
- [#12994](https://github.com/influxdata/telegraf/pull/12994) `outputs.stackdriver` Group batches by timestamp
- [#13126](https://github.com/influxdata/telegraf/pull/13126) `outputs.warp10` Support Infinity/-Infinity/NaN values
- [#13156](https://github.com/influxdata/telegraf/pull/13156) `processors.starlark` Do not reject tracking metrics twice

### Dependency Updates

- [#13256](https://github.com/influxdata/telegraf/pull/13256) `deps` Bump cloud.google.com/go/pubsub from 1.30.0 to 1.30.1
- [#13258](https://github.com/influxdata/telegraf/pull/13258) `deps` Bump github.com/aerospike/aerospike-client-go/v5 from 5.10.0 to 5.11.0
- [#13242](https://github.com/influxdata/telegraf/pull/13242) `deps` Bump github.com/antchfx/xpath to latest master for string-join()
- [#13255](https://github.com/influxdata/telegraf/pull/13255) `deps` Bump github.com/aws/aws-sdk-go-v2 from 1.17.8 to 1.18.0
- [#13215](https://github.com/influxdata/telegraf/pull/13215) `deps` Bump github.com/Azure/go-autorest/autorest/adal from 0.9.22 to 0.9.23
- [#13254](https://github.com/influxdata/telegraf/pull/13254) `deps` Bump github.com/benbjohnson/clock from 1.3.0 to 1.3.3
- [#13269](https://github.com/influxdata/telegraf/pull/13269) `deps` Bump github.com/docker/distribution from 2.8.1 to 2.8.2
- [#13216](https://github.com/influxdata/telegraf/pull/13216) `deps` Bump github.com/fatih/color from 1.13.0 to 1.15.0
- [#13104](https://github.com/influxdata/telegraf/pull/13104) `deps` Bump github.com/netsampler/goflow2 from 1.1.1 to 1.3.3
- [#13138](https://github.com/influxdata/telegraf/pull/13138) `deps` Bump github.com/yuin/goldmark from 1.5.3 to 1.5.4
- [#13257](https://github.com/influxdata/telegraf/pull/13257) `deps` Bump go.opentelemetry.io/collector/pdata from 1.0.0-rc7 to 1.0.0-rcv0011
- [#13137](https://github.com/influxdata/telegraf/pull/13137) `deps` Bump golang.org/x/net from 0.8.0 to 0.9.0
- [#13276](https://github.com/influxdata/telegraf/pull/13276) `deps` Bump golang.org/x/net from 0.9.0 to 0.10.0
- [#13217](https://github.com/influxdata/telegraf/pull/13217) `deps` Bump golang.org/x/oauth2 from 0.5.0 to 0.7.0
- [#13170](https://github.com/influxdata/telegraf/pull/13170) `deps` Bump google.golang.org/api from 0.106.0 to 0.120.0
- [#13223](https://github.com/influxdata/telegraf/pull/13223) `deps` Bump govulncheck-action from 0.10.0 to 0.10.1
- [#13225](https://github.com/influxdata/telegraf/pull/13225) `deps` Bump prometheus from v1.8.2 to v2.42.0
- [#13230](https://github.com/influxdata/telegraf/pull/13230) `deps` Bump signalfx/golib from 3.3.46 to 3.3.50

## v1.26.2 [2023-04-24]

### Bugfixes

- [#13020](https://github.com/influxdata/telegraf/pull/13020) `agent` Pass quiet flag earlier
- [#13063](https://github.com/influxdata/telegraf/pull/13063) `inputs.prometheus` Add namespace option in k8s informer factory
- [#13059](https://github.com/influxdata/telegraf/pull/13059) `inputs.socket_listener` Fix tracking of unix sockets
- [#13078](https://github.com/influxdata/telegraf/pull/13078) `parsers.grok` Fix nil metric for multiline inputs
- [#13092](https://github.com/influxdata/telegraf/pull/13092) `processors.lookup` Fix tracking metrics

### Dependency Updates

- [#13106](https://github.com/influxdata/telegraf/pull/13106) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.13.15 to 1.13.20
- [#13072](https://github.com/influxdata/telegraf/pull/13072) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.21.6 to 1.25.9
- [#13107](https://github.com/influxdata/telegraf/pull/13107) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.15.13 to 1.20.9
- [#13027](https://github.com/influxdata/telegraf/pull/13027) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.15.19 to 1.17.8
- [#13069](https://github.com/influxdata/telegraf/pull/13069) `deps` Bump github.com/aws/aws-sdk-go-v2/service/sts from 1.18.5 to 1.18.9
- [#13105](https://github.com/influxdata/telegraf/pull/13105) `deps` Bump github.com/docker/docker from 23.0.0 to 23.0.4
- [#13024](https://github.com/influxdata/telegraf/pull/13024) `deps` Bump github.com/openconfig/gnmi from 0.0.0-20220920173703-480bf53a74d2 to 0.9.1
- [#13026](https://github.com/influxdata/telegraf/pull/13026) `deps` Bump github.com/prometheus/common from 0.41.0 to 0.42.0
- [#13025](https://github.com/influxdata/telegraf/pull/13025) `deps` Bump github.com/safchain/ethtool from 0.2.0 to 0.3.0
- [#13023](https://github.com/influxdata/telegraf/pull/13023) `deps` Bump github.com/tinylib/msgp from 1.1.6 to 1.1.8
- [#13071](https://github.com/influxdata/telegraf/pull/13071) `deps` Bump github.com/vishvananda/netns from 0.0.2 to 0.0.4
- [#13070](https://github.com/influxdata/telegraf/pull/13070) `deps` Bump github.com/wavefronthq/wavefront-sdk-go from 0.11.0 to 0.12.0

## v1.26.1 [2023-04-03]

### Bugfixes

- [#12880](https://github.com/influxdata/telegraf/pull/12880) `config` Return error on order set as string
- [#12867](https://github.com/influxdata/telegraf/pull/12867) `inputs.ethtool` Check for nil
- [#12935](https://github.com/influxdata/telegraf/pull/12935) `inputs.execd` Add option to set buffer size
- [#12877](https://github.com/influxdata/telegraf/pull/12877) `inputs.internet_speed` Rename host tag to source
- [#12918](https://github.com/influxdata/telegraf/pull/12918) `inputs.kubernetes` Apply timeout for the whole HTTP request
- [#13006](https://github.com/influxdata/telegraf/pull/13006) `inputs.netflow` Use correct name in the build tag
- [#13015](https://github.com/influxdata/telegraf/pull/13015) `inputs.procstat` Return tags of pids if lookup_error
- [#12864](https://github.com/influxdata/telegraf/pull/12864) `inputs.prometheus` Correctly set timeout param
- [#12907](https://github.com/influxdata/telegraf/pull/12907) `inputs.prometheus` Use set over add for custom headers
- [#12961](https://github.com/influxdata/telegraf/pull/12961) `inputs.upsd` Include ups.real_power
- [#12908](https://github.com/influxdata/telegraf/pull/12908) `outputs.graphite` Add custom regex to outputs
- [#13012](https://github.com/influxdata/telegraf/pull/13012) `secrets` Add function to set a secret
- [#13002](https://github.com/influxdata/telegraf/pull/13002) `secrets` Minimize secret holding time
- [#12993](https://github.com/influxdata/telegraf/pull/12993) `secrets` Warn if OS limit for locked memory is too low
- [#12919](https://github.com/influxdata/telegraf/pull/12919) `secrets` Handle array of secrets correctly
- [#12835](https://github.com/influxdata/telegraf/pull/12835) `serializers.graphite` Allow for specifying regex to sanitize
- [#12990](https://github.com/influxdata/telegraf/pull/12990) `systemd` Increase lock memory for service to 8192kb

### Dependency Updates

- [#12857](https://github.com/influxdata/telegraf/pull/12857) `deps` Bump github.com/antchfx/xpath from 1.2.3 to 1.2.4
- [#12909](https://github.com/influxdata/telegraf/pull/12909) `deps` Bump github.com/apache/thrift from 0.16.0 to 0.18.1
- [#12856](https://github.com/influxdata/telegraf/pull/12856) `deps` Bump github.com/Azure/azure-event-hubs-go/v3 from 3.3.20 to 3.4.0
- [#12966](https://github.com/influxdata/telegraf/pull/12966) `deps` Bump github.com/Azure/go-autorest/autorest/azure/auth from 0.5.11 to 0.5.12
- [#12964](https://github.com/influxdata/telegraf/pull/12964) `deps` Bump github.com/golang-jwt/jwt/v4 from 4.4.2 to 4.5.0
- [#12967](https://github.com/influxdata/telegraf/pull/12967) `deps` Bump github.com/jhump/protoreflect from 1.8.3-0.20210616212123-6cc1efa697ca to 1.15.1
- [#12855](https://github.com/influxdata/telegraf/pull/12855) `deps` Bump github.com/nats-io/nats.go from 1.19.0 to 1.24.0
- [#12981](https://github.com/influxdata/telegraf/pull/12981) `deps` Bump github.com/opencontainers/runc from 1.1.4 to 1.1.5
- [#12913](https://github.com/influxdata/telegraf/pull/12913) `deps` Bump github.com/pion/dtls/v2 from 2.2.4 to 2.2.6
- [#12968](https://github.com/influxdata/telegraf/pull/12968) `deps` Bump github.com/rabbitmq/amqp091-go from 1.7.0 to 1.8.0
- [#13017](https://github.com/influxdata/telegraf/pull/13017) `deps` Bump github.com/shirou/gopsutil from 3.23.2 to 3.23.3
- [#12853](https://github.com/influxdata/telegraf/pull/12853) `deps` Bump github.com/Shopify/sarama from 1.37.2 to 1.38.1
- [#12854](https://github.com/influxdata/telegraf/pull/12854) `deps` Bump github.com/sensu/sensu-go/api/core/v2 from 2.15.0 to 2.16.0
- [#12911](https://github.com/influxdata/telegraf/pull/12911) `deps` Bump github.com/tidwall/gjson from 1.14.3 to 1.14.4
- [#12912](https://github.com/influxdata/telegraf/pull/12912) `deps` Bump golang.org/x/net from 0.7.0 to 0.8.0
- [#12910](https://github.com/influxdata/telegraf/pull/12910) `deps` Bump modernc.org/sqlite from 1.19.2 to 1.21.0

## v1.26.0 [2023-03-13]

### Important Changes

- Static Builds: Linux builds are now statically built. Other operating systems
  were cross-built in the past and as a result, already static. Users should
  not notice any change in behavior. The `_static` specific Linux binary is no
  longer produced as a result.
- telegraf.d Behavior: The default behavior of reading
  `/etc/telegraf/telegraf.conf` now includes any .conf files under
  `/etc/telegraf/telegraf.d/`. This change will apply to the official Telegraf
  Docker image as well. This will simplify docker usage when using multiple
  configuration files.
- Default Configuration: The `telegraf config` command and default config file
  provided by Telegraf now includes all plugins and produces the same output
  across all operating systems. Plugin comments specify what platforms are
  supported or not.
- State Persistence: State persistence is now available in select plugins. This
  will allow plugins to start collecting data, where they left off. A
  configuration with state persistence cannot change or it will not be able to
  recover.

### New Plugins

- [#12393](https://github.com/influxdata/telegraf/pull/12393) `inputs.opensearch_query` Opensearch Query
- [#12473](https://github.com/influxdata/telegraf/pull/12473) `inputs.p4runtime` P4Runtime
- [#12736](https://github.com/influxdata/telegraf/pull/12736) `inputs.radius` Radius Auth Response Time
- [#11250](https://github.com/influxdata/telegraf/pull/11250) `inputs.win_wmi` Windows Management Instrumentation (WMI)
- [#12809](https://github.com/influxdata/telegraf/pull/12809) `processors.lookup` Lookup

### Features

- [#12600](https://github.com/influxdata/telegraf/pull/12600) Always disable cgo support (static builds)
- [#12166](https://github.com/influxdata/telegraf/pull/12166) Plugin state-persistence
- [#12608](https://github.com/influxdata/telegraf/pull/12608) `agent` Add /etc/telegraf/telegraf.d to default config locations
- [#12827](https://github.com/influxdata/telegraf/pull/12827) `agent` Print loaded configs
- [#12821](https://github.com/influxdata/telegraf/pull/12821) `common.oauth` Add audience parameter
- [#12727](https://github.com/influxdata/telegraf/pull/12727) `common.tls` Add enable flag
- [#12579](https://github.com/influxdata/telegraf/pull/12579) `config` Accept durations given in days (e.g. 7d)
- [#12798](https://github.com/influxdata/telegraf/pull/12798) `inputs.cgroup` Added support for cpu.stat
- [#12345](https://github.com/influxdata/telegraf/pull/12345) `inputs.cisco_telemetry_mdt` Include delete field
- [#12696](https://github.com/influxdata/telegraf/pull/12696) `inputs.disk` Add label as tag
- [#12519](https://github.com/influxdata/telegraf/pull/12519) `inputs.dns_query` Add IP field(s)
- [#12775](https://github.com/influxdata/telegraf/pull/12775) `inputs.docker_log` Add state-persistence capabilities
- [#12814](https://github.com/influxdata/telegraf/pull/12814) `inputs.ethtool` Add support for link speed, duplex, etc.
- [#12550](https://github.com/influxdata/telegraf/pull/12550) `inputs.example` Add secret-store sample code
- [#12495](https://github.com/influxdata/telegraf/pull/12495) `inputs.gnmi` Set max gRPC message size
- [#12680](https://github.com/influxdata/telegraf/pull/12680) `inputs.haproxy` Add support for tcp endpoints in haproxy plugin
- [#12645](https://github.com/influxdata/telegraf/pull/12645) `inputs.http_listener_v2` Add custom server http headers
- [#12506](https://github.com/influxdata/telegraf/pull/12506) `inputs.icinga2` Support collecting hosts, services, and endpoint metrics
- [#12493](https://github.com/influxdata/telegraf/pull/12493) `inputs.influxdb` Collect uptime statistics
- [#12452](https://github.com/influxdata/telegraf/pull/12452) `inputs.intel_powerstat` Add CPU base frequency metric and add support for new platforms
- [#12707](https://github.com/influxdata/telegraf/pull/12707) `inputs.internet_speed` Add the best server selection via latency and jitter field
- [#12617](https://github.com/influxdata/telegraf/pull/12617) `inputs.internet_speed` Server ID include and exclude filter
- [#12730](https://github.com/influxdata/telegraf/pull/12730) `inputs.jti_openconfig_telemetry` Set timestamp from data
- [#12786](https://github.com/influxdata/telegraf/pull/12786) `inputs.modbus` Add RS485 specific config options
- [#12408](https://github.com/influxdata/telegraf/pull/12408) `inputs.modbus` Add workaround to enforce reads from zero for coil registers
- [#12825](https://github.com/influxdata/telegraf/pull/12825) `inputs.modbus` Allow to convert coil and discrete registers to boolean
- [#12591](https://github.com/influxdata/telegraf/pull/12591) `inputs.mysql` Add secret-store support
- [#12466](https://github.com/influxdata/telegraf/pull/12466) `inputs.openweathermap` Add snow parameter
- [#12628](https://github.com/influxdata/telegraf/pull/12628) `inputs.processes` Add use_sudo option for BSD
- [#12777](https://github.com/influxdata/telegraf/pull/12777) `inputs.prometheus` Use namespace annotations to filter pods to be scraped
- [#12496](https://github.com/influxdata/telegraf/pull/12496) `inputs.redfish` Add power control metric
- [#12400](https://github.com/influxdata/telegraf/pull/12400) `inputs.sqlserver` Get database pages performance counter
- [#12377](https://github.com/influxdata/telegraf/pull/12377) `inputs.stackdriver` Allow filtering by resource metadata labels
- [#12318](https://github.com/influxdata/telegraf/pull/12318) `inputs.statsd` Add pending messages stat and allow to configure number of threads
- [#12828](https://github.com/influxdata/telegraf/pull/12828) `inputs.vsphere` Flag for more lenient behavior when connect fails on startup
- [#12790](https://github.com/influxdata/telegraf/pull/12790) `inputs.win_eventlog` Add state-persistence capabilities
- [#12556](https://github.com/influxdata/telegraf/pull/12556) `inputs.win_perf_counters` Add remote system support
- [#12729](https://github.com/influxdata/telegraf/pull/12729) `inputs.wireguard` Add allowed_peer_cidr field
- [#12444](https://github.com/influxdata/telegraf/pull/12444) `inputs.x509_cert` Add OCSP stapling information for leaf certificates (#10550)
- [#12656](https://github.com/influxdata/telegraf/pull/12656) `inputs.x509_cert` Add tag for certificate type-classification
- [#12697](https://github.com/influxdata/telegraf/pull/12697) `outputs.mqtt` Add option to specify topic layouts
- [#12678](https://github.com/influxdata/telegraf/pull/12678) `outputs.mqtt` Add support for MQTT 5 publish properties
- [#12224](https://github.com/influxdata/telegraf/pull/12224) `outputs.mqtt` Enhance routing capabilities
- [#11816](https://github.com/influxdata/telegraf/pull/11816) `parsers.avro` Add Apache Avro parser
- [#12820](https://github.com/influxdata/telegraf/pull/12820) `parsers.xpath` Add timezone handling
- [#12767](https://github.com/influxdata/telegraf/pull/12767) `processors.converter` Convert tag or field as metric timestamp
- [#12659](https://github.com/influxdata/telegraf/pull/12659) `processors.unpivot` Add mode to create new metrics
- [#12812](https://github.com/influxdata/telegraf/pull/12812) `secretstores` Add command-line option to specify password
- [#12067](https://github.com/influxdata/telegraf/pull/12067) `secretstores` Add support for additional input plugins
- [#12497](https://github.com/influxdata/telegraf/pull/12497) `secretstores` Convert many output plugins

### Bugfixes

- [#12781](https://github.com/influxdata/telegraf/pull/12781) `agent` Allow graceful shutdown on interrupt (e.g. Ctrl-C)
- [#12740](https://github.com/influxdata/telegraf/pull/12740) `agent` Only rotate log on SIGHUP if needed
- [#12818](https://github.com/influxdata/telegraf/pull/12818) `inputs.amqp_consumer` Avoid deprecations when handling defaults
- [#12817](https://github.com/influxdata/telegraf/pull/12817) `inputs.amqp_consumer` Fix panic on Stop() if not connected successfully
- [#12815](https://github.com/influxdata/telegraf/pull/12815) `inputs.ethtool` Close namespace file to prevent crash
- [#12778](https://github.com/influxdata/telegraf/pull/12778) `inputs.statsd` On close, verify listener is not nil

### Dependency Updates

- [#12805](https://github.com/influxdata/telegraf/pull/12805) `deps` Bump cloud.google.com/go/storage from 1.28.1 to 1.29.0
- [#12804](https://github.com/influxdata/telegraf/pull/12804) `deps` Bump github.com/Azure/go-autorest/autorest/adal from 0.9.21 to 0.9.22
- [#12757](https://github.com/influxdata/telegraf/pull/12757) `deps` Bump github.com/aliyun/alibaba-cloud-sdk-go from 1.62.77 to 1.62.193
- [#12808](https://github.com/influxdata/telegraf/pull/12808) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.13.2 to 1.13.15
- [#12756](https://github.com/influxdata/telegraf/pull/12756) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.14.5 to 1.16.0
- [#12754](https://github.com/influxdata/telegraf/pull/12754) `deps` Bump github.com/coocood/freecache from 1.2.2 to 1.2.3
- [#12852](https://github.com/influxdata/telegraf/pull/12852) `deps` Bump github.com/opencontainers/runc from 1.1.3 to 1.1.4
- [#12806](https://github.com/influxdata/telegraf/pull/12806) `deps` Bump github.com/opensearch-project/opensearch-go/v2 from 2.1.0 to 2.2.0
- [#12753](https://github.com/influxdata/telegraf/pull/12753) `deps` Bump github.com/openzipkin-contrib/zipkin-go-opentracing from 0.4.5 to 0.5.0
- [#12755](https://github.com/influxdata/telegraf/pull/12755) `deps` Bump github.com/rabbitmq/amqp091-go from 1.5.0 to 1.7.0
- [#12822](https://github.com/influxdata/telegraf/pull/12822) `deps` Bump github.com/shirou/gopsutil from v3.22.12 to v3.23.2
- [#12807](https://github.com/influxdata/telegraf/pull/12807) `deps` Bump github.com/stretchr/testify from 1.8.1 to 1.8.2
- [#12840](https://github.com/influxdata/telegraf/pull/12840) `deps` Bump OpenTelemetry from 0.3.1 to 0.3.3
- [#12801](https://github.com/influxdata/telegraf/pull/12801) `deps` Downgrade github.com/karrick/godirwalk from v1.17.0 to v1.16.2

## v1.25.3 [2023-02-27]

### Bugfixes

- [#12721](https://github.com/influxdata/telegraf/pull/12721) `agent` Fix reload config on config update/SIGHUP
- [#12462](https://github.com/influxdata/telegraf/pull/12462) `inputs.bond` Reset slave stats for each interface
- [#12677](https://github.com/influxdata/telegraf/pull/12677) `inputs.cloudwatch` Verify endpoint is not nil
- [#12725](https://github.com/influxdata/telegraf/pull/12725) `inputs.lvm` Add options to specify path to binaries
- [#12724](https://github.com/influxdata/telegraf/pull/12724) `parsers.xpath` Fix panic for JSON name expansion
- [#12735](https://github.com/influxdata/telegraf/pull/12735) `serializers.json` Fix stateful transformations

### Dependency Updates

- [#12714](https://github.com/influxdata/telegraf/pull/12714) `deps` Bump cloud.google.com/go/pubsub from 1.27.1 to 1.28.0
- [#12693](https://github.com/influxdata/telegraf/pull/12693) `deps` Bump github.com/containerd/containerd from 1.6.8 to 1.6.18
- [#12715](https://github.com/influxdata/telegraf/pull/12715) `deps` Bump github.com/go-logfmt/logfmt from 0.5.1 to 0.6.0
- [#12668](https://github.com/influxdata/telegraf/pull/12668) `deps` Bump github.com/gofrs/uuid from 4.3.1 to 5.0.0
- [#12712](https://github.com/influxdata/telegraf/pull/12712) `deps` Bump github.com/gophercloud/gophercloud from 1.0.0 to 1.2.0
- [#12667](https://github.com/influxdata/telegraf/pull/12667) `deps` Bump github.com/pion/dtls/v2 from 2.1.5 to 2.2.4
- [#12699](https://github.com/influxdata/telegraf/pull/12699) `deps` Bump golang.org/x/net from 0.5.0 to 0.7.0
- [#12670](https://github.com/influxdata/telegraf/pull/12670) `deps` Bump golang.org/x/sys from 0.4.0 to 0.5.0
- [#12713](https://github.com/influxdata/telegraf/pull/12713) `deps` Bump google.golang.org/grpc from 1.52.3 to 1.53.0
- [#12669](https://github.com/influxdata/telegraf/pull/12669) `deps` Bump k8s.io/apimachinery from 0.25.3 to 0.25.6
- [#12698](https://github.com/influxdata/telegraf/pull/12698) `deps` Bump testcontainers from 0.14.0 to 0.18.0

## v1.25.2 [2023-02-13]

### Bugfixes

- [#12607](https://github.com/influxdata/telegraf/pull/12607) `agent` Only read the config once
- [#12586](https://github.com/influxdata/telegraf/pull/12586) `docs` Fix link to license for Google flatbuffers
- [#12637](https://github.com/influxdata/telegraf/pull/12637) `inputs.cisco_telemetry_mdt` Check subfield sizes to avoid panics
- [#12657](https://github.com/influxdata/telegraf/pull/12657) `inputs.cloudwatch` Enable custom endpoint support
- [#12603](https://github.com/influxdata/telegraf/pull/12603) `inputs.conntrack` Resolve segfault when setting collect field
- [#12512](https://github.com/influxdata/telegraf/pull/12512) `inputs.gnmi` Handle both new-style `tag_subscription` and old-style `tag_only`
- [#12599](https://github.com/influxdata/telegraf/pull/12599) `inputs.mongodb` Improve error logging
- [#12604](https://github.com/influxdata/telegraf/pull/12604) `inputs.mongodb` SIGSEGV when restarting MongoDB node
- [#12576](https://github.com/influxdata/telegraf/pull/12576) `inputs.mysql` Avoid side-effects for TLS between plugin instances
- [#12626](https://github.com/influxdata/telegraf/pull/12626) `inputs.prometheus` Deprecate and rename the timeout variable
- [#12648](https://github.com/influxdata/telegraf/pull/12648) `inputs.tail` Fix typo in the README
- [#12543](https://github.com/influxdata/telegraf/pull/12543) `inputs.upsd` Add additional fields
- [#12629](https://github.com/influxdata/telegraf/pull/12629) `inputs.x509_cert` Fix Windows path handling
- [#12560](https://github.com/influxdata/telegraf/pull/12560) `outputs.prometheus_client` Expire with ticker, not add/collect
- [#12644](https://github.com/influxdata/telegraf/pull/12644) `secretstores` Check store id format and presence

### Dependency Updates

- [#12630](https://github.com/influxdata/telegraf/pull/12630) `deps` Bump cloud.google.com/go/bigquery from 1.44.0 to 1.45.0
- [#12568](https://github.com/influxdata/telegraf/pull/12568) `deps` Bump github.com/99designs/keyring from 1.2.1 to 1.2.2
- [#12634](https://github.com/influxdata/telegraf/pull/12634) `deps` Bump github.com/antchfx/xmlquery from 1.3.12 to 1.3.15
- [#12633](https://github.com/influxdata/telegraf/pull/12633) `deps` Bump github.com/antchfx/xpath from 1.2.2 to 1.2.3
- [#12571](https://github.com/influxdata/telegraf/pull/12571) `deps` Bump github.com/coreos/go-semver from 0.3.0 to 0.3.1
- [#12632](https://github.com/influxdata/telegraf/pull/12632) `deps` Bump github.com/moby/ipvs from 1.0.2 to 1.1.0
- [#12572](https://github.com/influxdata/telegraf/pull/12572) `deps` Bump github.com/multiplay/go-ts3 from 1.0.1 to 1.1.0
- [#12581](https://github.com/influxdata/telegraf/pull/12581) `deps` Bump github.com/prometheus/client_golang from 1.13.1 to 1.14.0
- [#12580](https://github.com/influxdata/telegraf/pull/12580) `deps` Bump github.com/shirou/gopsutil from 3.22.9 to 3.22.12
- [#12570](https://github.com/influxdata/telegraf/pull/12570) `deps` Bump go.mongodb.org/mongo-driver from 1.11.0 to 1.11.1
- [#12582](https://github.com/influxdata/telegraf/pull/12582) `deps` Bump golang/x dependencies
- [#12583](https://github.com/influxdata/telegraf/pull/12583) `deps` Bump google.golang.org/grpc from 1.51.0 to 1.52.0
- [#12631](https://github.com/influxdata/telegraf/pull/12631) `deps` Bump google.golang.org/grpc from 1.52.0 to 1.52.3

## v1.25.1 [2023-01-30]

### Bugfixes

- [#12549](https://github.com/influxdata/telegraf/pull/12549) `agent` Catch non-existing commands and error out
- [#12453](https://github.com/influxdata/telegraf/pull/12453) `agent` Correctly reload configuration files
- [#12491](https://github.com/influxdata/telegraf/pull/12491) `agent` Handle float time with fractions of seconds correctly
- [#12457](https://github.com/influxdata/telegraf/pull/12457) `agent` Only set default snmp after reading all configs
- [#12515](https://github.com/influxdata/telegraf/pull/12515) `common.cookie` Allow any 2xx status code
- [#12459](https://github.com/influxdata/telegraf/pull/12459) `common.kafka` Add keep-alive period setting for input and output
- [#12240](https://github.com/influxdata/telegraf/pull/12240) `inputs.cisco_telemetry_mdt` Add operation-metric and class-policy prefix
- [#12533](https://github.com/influxdata/telegraf/pull/12533) `inputs.exec` Restore pre-v1.21 behavior for CSV data_format
- [#12415](https://github.com/influxdata/telegraf/pull/12415) `inputs.gnmi` Update configuration documentation
- [#12536](https://github.com/influxdata/telegraf/pull/12536) `inputs.logstash` Collect opensearch specific stats
- [#12409](https://github.com/influxdata/telegraf/pull/12409) `inputs.mysql` Revert slice declarations with non-zero initial length
- [#12529](https://github.com/influxdata/telegraf/pull/12529) `inputs.opcua` Fix opcua and opcua-listener for servers using password-based auth
- [#12522](https://github.com/influxdata/telegraf/pull/12522) `inputs.prometheus` Correctly track deleted pods
- [#12559](https://github.com/influxdata/telegraf/pull/12559) `inputs.prometheus` Set the timeout for slow running API endpoints correctly
- [#12384](https://github.com/influxdata/telegraf/pull/12384) `inputs.sqlserver` Add more precise version check
- [#12387](https://github.com/influxdata/telegraf/pull/12387) `inputs.sqlserver` Added own SPID filter
- [#12386](https://github.com/influxdata/telegraf/pull/12386) `inputs.sqlserver` SqlRequests include sleeping sessions with open transactions
- [#12528](https://github.com/influxdata/telegraf/pull/12528) `inputs.sqlserver` Suppress error on secondary replicas
- [#12516](https://github.com/influxdata/telegraf/pull/12516) `inputs.upsd` Always convert to float
- [#12486](https://github.com/influxdata/telegraf/pull/12486) `inputs.upsd` Ensure firmware is always a string
- [#12375](https://github.com/influxdata/telegraf/pull/12375) `inputs.win_eventlog` Handle remote events more robustly
- [#12404](https://github.com/influxdata/telegraf/pull/12404) `inputs.x509_cert` Fix off-by-one when adding intermediate certificates
- [#12399](https://github.com/influxdata/telegraf/pull/12399) `outputs.loki` Return response body on error
- [#12440](https://github.com/influxdata/telegraf/pull/12440) `parsers.json_v2` In case of invalid json, log message to debug log
- [#12401](https://github.com/influxdata/telegraf/pull/12401) `secretstores` Cleanup duplicate printing
- [#12468](https://github.com/influxdata/telegraf/pull/12468) `secretstores` Fix handling of "id" and print failing secret-store
- [#12490](https://github.com/influxdata/telegraf/pull/12490) `secretstores` Fix handling of TOML strings

### Dependency Updates

- [#12385](https://github.com/influxdata/telegraf/pull/12385) `deps` Bump cloud.google.com/go/storage from 1.23.0 to 1.28.1
- [#12511](https://github.com/influxdata/telegraf/pull/12511) `deps` Bump github.com/antchfx/jsonquery from 1.3.0 to 1.3.1
- [#12420](https://github.com/influxdata/telegraf/pull/12420) `deps` Bump github.com/aws/aws-sdk-go-v2 from 1.17.1 to 1.17.3
- [#12538](https://github.com/influxdata/telegraf/pull/12538) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.54.4 to 1.80.1
- [#12476](https://github.com/influxdata/telegraf/pull/12476) `deps` Bump github.com/denisenkom/go-mssqldb from 0.12.0 to 0.12.3
- [#12378](https://github.com/influxdata/telegraf/pull/12378) `deps` Bump github.com/eclipse/paho.mqtt.golang from 1.4.1 to 1.4.2
- [#12381](https://github.com/influxdata/telegraf/pull/12381) `deps` Bump github.com/hashicorp/consul/api from 1.15.2 to 1.18.0
- [#12417](https://github.com/influxdata/telegraf/pull/12417) `deps` Bump github.com/karrick/godirwalk from 1.16.1 to 1.17.0
- [#12418](https://github.com/influxdata/telegraf/pull/12418) `deps` Bump github.com/kardianos/service from 1.2.1 to 1.2.2
- [#12379](https://github.com/influxdata/telegraf/pull/12379) `deps` Bump github.com/nats-io/nats-server/v2 from 2.9.4 to 2.9.9

## v1.25.0 [2022-12-12]

### New Plugins

- [#10103](https://github.com/influxdata/telegraf/pull/10103) `inputs.azure_monitor` Azure Monitor
- [#8413](https://github.com/influxdata/telegraf/pull/8413) `inputs.gcs` Google Cloud Storage
- [#11824](https://github.com/influxdata/telegraf/pull/11824) `inputs.intel_dlb` Intel DLB
- [#11814](https://github.com/influxdata/telegraf/pull/11814) `inputs.libvirt` libvirt
- [#12108](https://github.com/influxdata/telegraf/pull/12108) `inputs.netflow` netflow v5, v9, and IPFIX
- [#11786](https://github.com/influxdata/telegraf/pull/11786) `inputs.opcua_listener` OPC UA Event subscriptions

### Features

- [#12130](https://github.com/influxdata/telegraf/pull/12130) Add arm64 Windows builds to nightly and CI
- [#11987](https://github.com/influxdata/telegraf/pull/11987) `agent` Add method to inform of deprecated plugin option values
- [#11232](https://github.com/influxdata/telegraf/pull/11232) `agent` Secret-store implementation
- [#12358](https://github.com/influxdata/telegraf/pull/12358) `agent` Deprecate active usage of netsnmp translator
- [#12302](https://github.com/influxdata/telegraf/pull/12302) `agent.tls` Allow setting renegotiation method
- [#12111](https://github.com/influxdata/telegraf/pull/12111) `common.kafka` Add exponential backoff when connecting or reconnecting and allow plugin to start without making initial connection
- [#11860](https://github.com/influxdata/telegraf/pull/11860) `inputs.amqp_consumer` Determine content encoding automatically
- [#12014](https://github.com/influxdata/telegraf/pull/12014) `inputs.apcupsd` Add new fields
- [#12342](https://github.com/influxdata/telegraf/pull/12342) `inputs.cgroups` Do not abort on first error, print message once
- [#8958](https://github.com/influxdata/telegraf/pull/8958) `inputs.conntrack` Parse conntrack stats
- [#11703](https://github.com/influxdata/telegraf/pull/11703) `inputs.diskio` Allow selecting devices by ID
- [#11895](https://github.com/influxdata/telegraf/pull/11895) `inputs.ethtool` Gather statistics from namespaces
- [#12087](https://github.com/influxdata/telegraf/pull/12087) `inputs.ethtool` Possibility to skip gathering metrics for downed interfaces
- [#12324](https://github.com/influxdata/telegraf/pull/12324) `inputs.http_response` Add User-Agent header
- [#12304](https://github.com/influxdata/telegraf/pull/12304) `inputs.kafka_consumer` Add sarama debug logs
- [#11783](https://github.com/influxdata/telegraf/pull/11783) `inputs.knx_listener` Support TCP as transport protocol
- [#12301](https://github.com/influxdata/telegraf/pull/12301) `inputs.kubernetes` Allow fetching kublet metrics remotely

- [#12255](https://github.com/influxdata/telegraf/pull/12255) `inputs.modbus` Add 8-bit integer types
- [#11983](https://github.com/influxdata/telegraf/pull/11983) `inputs.modbus` Add config option to pause after connect
- [#12340](https://github.com/influxdata/telegraf/pull/12340) `inputs.modbus` Add support for half-precision float (float16)
- [#11106](https://github.com/influxdata/telegraf/pull/11106) `inputs.modbus` Optimize grouped requests
- [#11273](https://github.com/influxdata/telegraf/pull/11273) `inputs.modbus` Optimize requests
- [#11630](https://github.com/influxdata/telegraf/pull/11630) `inputs.opcua` Add use regular reads workaround
- [#9633](https://github.com/influxdata/telegraf/pull/9633) `inputs.powerdns_recursor` Support for new PowerDNS recursor control protocol
- [#12050](https://github.com/influxdata/telegraf/pull/12050) `inputs.prometheus` Add support for custom header
- [#11962](https://github.com/influxdata/telegraf/pull/11962) `inputs.prometheus` Allow explicit scrape configuration without annotations
- [#11729](https://github.com/influxdata/telegraf/pull/11729) `inputs.prometheus` Use system wide proxy settings
- [#12329](https://github.com/influxdata/telegraf/pull/12329) `inputs.smart` Add additional SMART metrics that indicate/predict device failure
- [#11872](https://github.com/influxdata/telegraf/pull/11872) `inputs.snmp` Convert enum values
- [#12187](https://github.com/influxdata/telegraf/pull/12187) `inputs.socket_ listener` Allow to specify message separator for streams
- [#12351](https://github.com/influxdata/telegraf/pull/12351) `inputs.sqlserver` Add @@SERVICENAME and SERVERPROPERTY(IsClustered) in measurement sqlserver_server_properties
- [#12126](https://github.com/influxdata/telegraf/pull/12126) `inputs.sqlserver` Add data and log used space metrics for Azure SQL DB
- [#12292](https://github.com/influxdata/telegraf/pull/12292) `inputs.sqlserver` Add metric available_physical_memory_kb in sqlserver_server_properties
- [#12319](https://github.com/influxdata/telegraf/pull/12319) `inputs.sqlserver` Introduce timeout for query execution
- [#12147](https://github.com/influxdata/telegraf/pull/12147) `inputs.system` Collect unique user count logged in
- [#12281](https://github.com/influxdata/telegraf/pull/12281) `inputs.tail` Add option to preserve newlines for multiline data
- [#11762](https://github.com/influxdata/telegraf/pull/11762) `inputs.tail` Allow handling of quoted strings spanning multiple lines
- [#12170](https://github.com/influxdata/telegraf/pull/12170) `inputs.tomcat` Add source tag
- [#11874](https://github.com/influxdata/telegraf/pull/11874) `outputs.azure_data_explorer` Add support for streaming ingestion for ADX output plugin
- [#11991](https://github.com/influxdata/telegraf/pull/11991) `outputs.event_hubs` Expose max message size batch option
- [#11950](https://github.com/influxdata/telegraf/pull/11950) `outputs.graylog` Implement optional connection retries
- [#11385](https://github.com/influxdata/telegraf/pull/11385) `outputs.timestream` Support ingesting multi-measures
- [#12232](https://github.com/influxdata/telegraf/pull/12232) `parsers.binary` Handle hex-encoded inputs
- [#12008](https://github.com/influxdata/telegraf/pull/12008) `parsers.csv` Add option for overwrite tags
- [#12247](https://github.com/influxdata/telegraf/pull/12247) `parsers.csv` Support null delimiters
- [#12320](https://github.com/influxdata/telegraf/pull/12320) `parsers.grok` Add option to allow multiline messages
- [#11933](https://github.com/influxdata/telegraf/pull/11933) `parsers.xpath` Add option to skip (header) bytes
- [#11999](https://github.com/influxdata/telegraf/pull/11999) `parsers.xpath` Allow to specify byte-array fields to encode in HEX
- [#11552](https://github.com/influxdata/telegraf/pull/11552) `parsers` Add binary parser
- [#12260](https://github.com/influxdata/telegraf/pull/12260) `serializers.json` Support serializing JSON nested in string fields

### Bugfixes

- [#12113](https://github.com/influxdata/telegraf/pull/12113) `agent` Run processors in config order
- [#12127](https://github.com/influxdata/telegraf/pull/12127) `agent` Watch for changes in configuration files in config directories
- [#12062](https://github.com/influxdata/telegraf/pull/12062) `inputs.conntrack` Skip gather tests if conntrack kernel module is not loaded
- [#12295](https://github.com/influxdata/telegraf/pull/12295) `inputs.filecount` Revert library version
- [#12284](https://github.com/influxdata/telegraf/pull/12284) `inputs.kube_inventory` Change default token path, use in-cluster config by default
- [#12235](https://github.com/influxdata/telegraf/pull/12235) `inputs.modbus` Add workaround to read field in separate requests
- [#12339](https://github.com/influxdata/telegraf/pull/12339) `inputs.modbus` Fix Windows COM-port path
- [#12367](https://github.com/influxdata/telegraf/pull/12367) `inputs.modbus` Fix default value of transmission mode
- [#12330](https://github.com/influxdata/telegraf/pull/12330) `inputs.mongodb` Fix connection leak triggered by config reload
- [#12101](https://github.com/influxdata/telegraf/pull/12101) `inputs.opcua` Add support for opcua datetime values
- [#12376](https://github.com/influxdata/telegraf/pull/12376) `inputs.opcua` Parse full range of status codes with uint32
- [#12278](https://github.com/influxdata/telegraf/pull/12278) `inputs.promethes` Respect selectors when scraping pods
- [#12323](https://github.com/influxdata/telegraf/pull/12323) `inputs.sql` Cast measurement_column to string
- [#12259](https://github.com/influxdata/telegraf/pull/12259) `inputs.vsphere` Eliminated duplicate samples
- [#12307](https://github.com/influxdata/telegraf/pull/12307) `inputs.zfs` Unbreak datasets stats gathering in case listsnaps is enabled on a zfs pool
- [#12291](https://github.com/influxdata/telegraf/pull/12291) `outputs.azure_data_explorer` Update test call to NewSerializer
- [#12357](https://github.com/influxdata/telegraf/pull/12357) `processors.parser` Handle empty metric names correctly

### Dependency Updates

- [#12334](https://github.com/influxdata/telegraf/pull/12334) `deps` Update github.com/aliyun/alibaba-cloud-sdk-go from 1.61.1836 to 1.62.77
- [#12355](https://github.com/influxdata/telegraf/pull/12355) `deps` Update github.com/gosnmp/gosnmp from 1.34.0 to 1.35.0
- [#12372](https://github.com/influxdata/telegraf/pull/12372) `deps` Update OpenTelemetry from 0.2.30 to 0.2.33

## v1.24.4 [2022-11-29]

### Bugfixes

- [#12177](https://github.com/influxdata/telegraf/pull/12177) `inputs.cloudwatch` Correctly handle multiple namespaces
- [#12294](https://github.com/influxdata/telegraf/pull/12294) `inputs.directory_monitor` Close input file before removal
- [#12140](https://github.com/influxdata/telegraf/pull/12140) `inputs.gnmi` Handle decimal_val as per gnmi v0.8.0
- [#12275](https://github.com/influxdata/telegraf/pull/12275) `inputs.gnmi` Do not provide empty prefix for subscription request
- [#12258](https://github.com/influxdata/telegraf/pull/12258) `inputs.gnmi` Fix empty name for Sonic devices
- [#12171](https://github.com/influxdata/telegraf/pull/12171) `inputs.ping` Avoid -x/-X on FreeBSD 13 and newer with ping6
- [#12282](https://github.com/influxdata/telegraf/pull/12282) `inputs.prometheus` Correctly default to port 9102
- [#12229](https://github.com/influxdata/telegraf/pull/12229) `input.redis_sentinel` Fix sentinel and replica stats gathering
- [#12280](https://github.com/influxdata/telegraf/pull/12280) `inputs.socket_listener` Ensure closed connection
- [#12201](https://github.com/influxdata/telegraf/pull/12201) `output.datadog` Log response in case of non 2XX response from API
- [#12160](https://github.com/influxdata/telegraf/pull/12160) `outputs.prometheus` Expire metrics correctly during adds
- [#12156](https://github.com/influxdata/telegraf/pull/12156) `outputs.yandex_cloud_monitoring` Catch int64 values

### Dependency Updates

- [#12132](https://github.com/influxdata/telegraf/pull/12132) `deps` Bump github.com/aliyun/alibaba-cloud-sdk-go from 1.61.1818 to 1.61.1836
- [#12197](https://github.com/influxdata/telegraf/pull/12197) `deps` Bump github.com/prometheus/client_golang from 1.13.0 to 1.13.1
- [#12196](https://github.com/influxdata/telegraf/pull/12196) `deps` Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.13.12 to 1.14.5
- [#12198](https://github.com/influxdata/telegraf/pull/12198) `deps` Bump github.com/aws/aws-sdk-go-v2/feature/ec2/imds from 1.12.17 to 1.12.19
- [#12236](https://github.com/influxdata/telegraf/pull/12236) `deps` Bump github.com/gofrs/uuid from v4.3.0 to v4.3.1
- [#12237](https://github.com/influxdata/telegraf/pull/12237) `deps` Bump github.com/aws/aws-sdk-go-v2/service/sts from 1.16.19 to 1.17.2
- [#12238](https://github.com/influxdata/telegraf/pull/12238) `deps` Bump github.com/urfave/cli/v2 from 2.16.3 to 2.23.5
- [#12239](https://github.com/influxdata/telegraf/pull/12239) `deps` Bump github.com/Azure/azure-event-hubs-go/v3 from 3.3.18 to 3.3.20
- [#12248](https://github.com/influxdata/telegraf/pull/12248) `deps` Bump github.com/showwin/speedtest-go from 1.1.5 to 1.2.1
- [#12269](https://github.com/influxdata/telegraf/pull/12269) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.12.21 to 1.13.2
- [#12268](https://github.com/influxdata/telegraf/pull/12268) `deps` Bump github.com/yuin/goldmark from 1.5.2 to 1.5.3
- [#12267](https://github.com/influxdata/telegraf/pull/12267) `deps` Bump cloud.google.com/go/pubsub from 1.25.1 to 1.26.0
- [#12266](https://github.com/influxdata/telegraf/pull/12266) `deps` Bump go.mongodb.org/mongo-driver from 1.10.2 to 1.11.0

## v1.24.3 [2022-11-02]

### Bugfixes

- [#12063](https://github.com/influxdata/telegraf/pull/12063) Restore warning on unused config option(s)
- [#11941](https://github.com/influxdata/telegraf/pull/11941) Setting `enable_tls` has incorrect default value
- [#12093](https://github.com/influxdata/telegraf/pull/12093) Update systemd unit description
- [#12077](https://github.com/influxdata/telegraf/pull/12077) `agent` Fix panic due to tickers slice was off-by-one in size
- [#12076](https://github.com/influxdata/telegraf/pull/12076) `config` Set default parser
- [#12124](https://github.com/influxdata/telegraf/pull/12124) `inputs.directory_monitor` Allow cross filesystem directories
- [#12064](https://github.com/influxdata/telegraf/pull/12064) `inputs.kafka` Switch to sarama's new consumer group rebalance strategy setting
- [#12038](https://github.com/influxdata/telegraf/pull/12038) `inputs.modbus` Add slave id to failing connection
- [#12109](https://github.com/influxdata/telegraf/pull/12109) `inputs.modbus` Handle field-measurement definitions correctly on duplicate field check
- [#11912](https://github.com/influxdata/telegraf/pull/11912) `inputs.modbus` Improve duplicate field checks
- [#11993](https://github.com/influxdata/telegraf/pull/11993) `inputs.opcua` Add metric tags to node
- [#11997](https://github.com/influxdata/telegraf/pull/11997) `inputs.syslog` Print error when no error or message given
- [#12023](https://github.com/influxdata/telegraf/pull/12023) `inputs.zookeeper` Add the ability to parse floats as floats
- [#11926](https://github.com/influxdata/telegraf/pull/11926) `parsers.json_v2` Remove BOM before parsing
- [#12116](https://github.com/influxdata/telegraf/pull/12116) `processors.parser` Keep name of original metric if parser doesn't return one
- [#12081](https://github.com/influxdata/telegraf/pull/12081) `processors` Correctly setup processors
- [#12016](https://github.com/influxdata/telegraf/pull/12016) `regression` Fixes problem with metrics not exposed by plugins.
- [#12024](https://github.com/influxdata/telegraf/pull/12024) `serializers.splunkmetric` Provide option to remove event metric tag

### Features

- [#12075](https://github.com/influxdata/telegraf/pull/12075) `tools` Allow to markdown includes for sections

### Dependency Updates

- [#11886](https://github.com/influxdata/telegraf/pull/11886) `deps` Bump github.com/snowflakedb/gosnowflake from 1.6.2 to 1.6.13
- [#11928](https://github.com/influxdata/telegraf/pull/11928) `deps` Bump github.com/sensu/sensu-go/api/core/v2 from 2.14.0 to 2.15.0
- [#11935](https://github.com/influxdata/telegraf/pull/11935) `deps` Bump github.com/gofrs/uuid from 4.2.0& to 4.3.0
- [#11894](https://github.com/influxdata/telegraf/pull/11894) `deps` Bump github.com/hashicorp/consul/api from 1.14.0 to 1.15.2
- [#11936](https://github.com/influxdata/telegraf/pull/11936) `deps` Bump github.com/aws/aws-sdk-go-v2/credentials from 1.12.5 to 1.12.21
- [#11972](https://github.com/influxdata/telegraf/pull/11972) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch
- [#11979](https://github.com/influxdata/telegraf/pull/11979) `deps` Bump github.com/aws/aws-sdk-go-v2/config
- [#11938](https://github.com/influxdata/telegraf/pull/11938) `deps` Bump k8s.io/apimachinery from 0.25.1 to 0.25.2
- [#12001](https://github.com/influxdata/telegraf/pull/12001) `deps` Bump k8s.io/api from 0.25.0 to 0.25.2
- [#12029](https://github.com/influxdata/telegraf/pull/12029) `deps` Bump k8s.io/api from 0.25.2 to 0.25.3
- [#12030](https://github.com/influxdata/telegraf/pull/12030) `deps` Bump modernc.org/sqlite from 1.17.3 to 1.19.2
- [#12034](https://github.com/influxdata/telegraf/pull/12034) `deps` Bump github.com/signalfx/golib/v3 from 3.3.45 to 3.3.46
- [#12035](https://github.com/influxdata/telegraf/pull/12035) `deps` Bump github.com/yuin/goldmark from 1.4.13 to 1.5.2
- [#11937](https://github.com/influxdata/telegraf/pull/11937) `deps` Bump cloud.google.com/go/bigquery from 1.40.0 to 1.42.0
- [#12037](https://github.com/influxdata/telegraf/pull/12037) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis
- [#12036](https://github.com/influxdata/telegraf/pull/12036) `deps` Bump github.com/aliyun/alibaba-cloud-sdk-go
- [#11980](https://github.com/influxdata/telegraf/pull/11980) `deps` Bump github.com/Shopify/sarama from 1.36.0 to 1.37.2
- [#12039](https://github.com/influxdata/telegraf/pull/12039) `deps` Bump testcontainers-go from 0.13.0 to 0.14.0 and address breaking change
- [#12090](https://github.com/influxdata/telegraf/pull/12090) `deps` Bump modernc.org/libc from v1.20.3 to v1.21.2
- [#12098](https://github.com/influxdata/telegraf/pull/12098) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb
- [#12096](https://github.com/influxdata/telegraf/pull/12096) `deps` Bump google.golang.org/api from 0.95.0 to 0.100.0
- [#12095](https://github.com/influxdata/telegraf/pull/12095) `deps` Bump github.com/gopcua/opcua from 0.3.3 to 0.3.7
- [#12097](https://github.com/influxdata/telegraf/pull/12097) `deps` Bump github.com/prometheus/client_model from 0.2.0 to 0.3.0
- [#12135](https://github.com/influxdata/telegraf/pull/12135) `deps` Bump cloud.google.com/go/monitoring from 1.5.0 to 1.7.0
- [#12134](https://github.com/influxdata/telegraf/pull/12134) `deps` Bump github.com/nats-io/nats-server/v2 from 2.8.4 to 2.9.4

## v1.24.2 [2022-10-03]

### Bugfixes

- [#11806](https://github.com/influxdata/telegraf/pull/11806) Re-allow specifying the influx parser type
- [#11896](https://github.com/influxdata/telegraf/pull/11896) `cli` Support old style of filtering sample configs
- [#11519](https://github.com/influxdata/telegraf/pull/11519) `common.kafka` Enable TLS in Kafka plugins without custom config
- [#11866](https://github.com/influxdata/telegraf/pull/11866) `inputs.influxdb_listener` Error on invalid precision
- [#11877](https://github.com/influxdata/telegraf/pull/11877) `inputs.internet_speed` Rename enable_file_download to match upstream intent
- [#11849](https://github.com/influxdata/telegraf/pull/11849) `inputs.mongodb` Start plugin correctly
- [#10696](https://github.com/influxdata/telegraf/pull/10696) `inputs.mqtt_consumer` Rework connection and message tracking
- [#11696](https://github.com/influxdata/telegraf/pull/11696) `internal.ethtool` Avoid internal name conflict with aws
- [#11875](https://github.com/influxdata/telegraf/pull/11875) `parser.xpath` Handle floating-point times correctly

### Dependency Updates

- [#11861](https://github.com/influxdata/telegraf/pull/11861) Update dependencies for OpenBSD support
- [#11840](https://github.com/influxdata/telegraf/pull/11840) `deps` Bump k8s.io/apimachinery from 0.25.0 to 0.25.1
- [#11844](https://github.com/influxdata/telegraf/pull/11844) `deps` Bump github.com/aerospike/aerospike-client-go/v5 from 5.9.0 to 5.10.0
- [#11839](https://github.com/influxdata/telegraf/pull/11839) `deps` Bump github.com/nats-io/nats.go from 1.16.0 to 1.17.0
- [#11836](https://github.com/influxdata/telegraf/pull/11836) `deps` Replace go-ping by pro-bing
- [#11887](https://github.com/influxdata/telegraf/pull/11887) `deps` Bump go.mongodb.org/mongo-driver from 1.10.1 to 1.10.2
- [#11890](https://github.com/influxdata/telegraf/pull/11890) `deps` Bump github.com/aws/smithy-go from 1.13.2 to 1.13.3
- [#11891](https://github.com/influxdata/telegraf/pull/11891) `deps` Bump github.com/rabbitmq/amqp091-go from 1.4.0 to 1.5.0
- [#11893](https://github.com/influxdata/telegraf/pull/11893) `deps` Bump github.com/docker/distribution from v2.7.1 to v2.8.1

## v1.24.1 [2022-09-19]

### Bugfixes

- [#11787](https://github.com/influxdata/telegraf/pull/11787) Clear error message when provided config is not a text file
- [#11835](https://github.com/influxdata/telegraf/pull/11835) Enable global confirmation for installing mingw
- [#10797](https://github.com/influxdata/telegraf/pull/10797) `inputs.ceph` Modernize Ceph input plugin metrics
- [#11785](https://github.com/influxdata/telegraf/pull/11785) `inputs.modbus` Do not fail if a single slave reports errors
- [#11827](https://github.com/influxdata/telegraf/pull/11827) `inputs.ntpq` Handle pools with &#34;-&#34; when
- [#11825](https://github.com/influxdata/telegraf/pull/11825) `parsers.csv` Remove direct checks for the parser type
- [#11781](https://github.com/influxdata/telegraf/pull/11781) `parsers.xpath` Add array index when expanding names.
- [#11815](https://github.com/influxdata/telegraf/pull/11815) `parsers` Memory leak for plugins using ParserFunc.
- [#11826](https://github.com/influxdata/telegraf/pull/11826) `parsers` Unwrap parser and remove some special handling

### Features

- [#11228](https://github.com/influxdata/telegraf/pull/11228) `processors.parser` Add option to parse tags

### Dependency Updates

- [#11788](https://github.com/influxdata/telegraf/pull/11788) `deps` Bump cloud.google.com/go/pubsub from 1.24.0 to 1.25.1
- [#11794](https://github.com/influxdata/telegraf/pull/11794) `deps` Bump github.com/urfave/cli/v2 from 2.14.1 to 2.16.3
- [#11789](https://github.com/influxdata/telegraf/pull/11789) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2
- [#11799](https://github.com/influxdata/telegraf/pull/11799) `deps` Bump github.com/wavefronthq/wavefront-sdk-go
- [#11796](https://github.com/influxdata/telegraf/pull/11796) `deps` Bump cloud.google.com/go/bigquery from 1.33.0 to 1.40.0

## v1.24.0 [2022-09-12]

### Bugfixes

- [#11779](https://github.com/influxdata/telegraf/pull/11779) Add missing entry json_transformation to missingTomlField
- [#11288](https://github.com/influxdata/telegraf/pull/11288) Add reset-mode flag for CSV parser
- [#11512](https://github.com/influxdata/telegraf/pull/11512) Add version number to MacOS packages
- [#11489](https://github.com/influxdata/telegraf/pull/11489) Backport sync sample.conf and README.md files
- [#11777](https://github.com/influxdata/telegraf/pull/11777) Do not error out for parsing errors in datadog mode
- [#11521](https://github.com/influxdata/telegraf/pull/11521) Make docs & go.mod cleanup post-redis merge
- [#11656](https://github.com/influxdata/telegraf/pull/11656) Refactor telegraf version
- [#11563](https://github.com/influxdata/telegraf/pull/11563) Remove shell execution for license-checker
- [#11755](https://github.com/influxdata/telegraf/pull/11755) Sort labels in prometheusremotewrite serializer
- [#11440](https://github.com/influxdata/telegraf/pull/11440) Update prometheus parser to be a new style parser plugin
- [#11456](https://github.com/influxdata/telegraf/pull/11456) Update prometheusremotewrite parser to be a new style parser plugin
- [#10570](https://github.com/influxdata/telegraf/pull/10570) Use os-agnositc systemd detection, remove sysv in RPM packaging
- [#11615](https://github.com/influxdata/telegraf/pull/11615) `agent` Add flushBatch method
- [#11692](https://github.com/influxdata/telegraf/pull/11692) `inputs.jolokia2` Add optional origin header
- [#11629](https://github.com/influxdata/telegraf/pull/11629) `inputs.mongodb` Add an option to bypass connection errors on start
- [#11723](https://github.com/influxdata/telegraf/pull/11723) `inputs.opcua` Assign node id correctly
- [#11673](https://github.com/influxdata/telegraf/pull/11673) `inputs.prometheus` Plugin run outside k8s cluster error
- [#11701](https://github.com/influxdata/telegraf/pull/11701) `inputs.sqlserver` Fixing wrong filtering for sqlAzureMIRequests and sqlAzureDBRequests
- [#11471](https://github.com/influxdata/telegraf/pull/11471) `inputs.upsd` Move to new sample.conf style
- [#11613](https://github.com/influxdata/telegraf/pull/11613) `inputs.x509` Multiple sources with non-overlapping DNS entries
- [#11767](https://github.com/influxdata/telegraf/pull/11767) `outputs.execd` Fixing the execd behavior to not throw error when partially unserializable metrics are written
- [#11560](https://github.com/influxdata/telegraf/pull/11560) `outputs.wavefront` Update wavefront sdk and use non-deprecated APIs

### Features

- [#11307](https://github.com/influxdata/telegraf/pull/11307) `serializers.csv` Add CSV serializer
- [#11054](https://github.com/influxdata/telegraf/pull/11054) `outputs.redistimeseries` Add RedisTimeSeries plugin
- [#7995](https://github.com/influxdata/telegraf/pull/7995) `outputs.stomp` Add Stomp (Active MQ) output plugin
- [#11300](https://github.com/influxdata/telegraf/pull/11300) Add default appType as config option to groundwork output
- [#11398](https://github.com/influxdata/telegraf/pull/11398) Add license checking tool
- [#11399](https://github.com/influxdata/telegraf/pull/11399) Add proxy support for outputs/cloudwatch
- [#11516](https://github.com/influxdata/telegraf/pull/11516) Added metrics for member and replica-set avg health of MongoDB
- [#11233](https://github.com/influxdata/telegraf/pull/11233) Adding aws metric streams input plugin
- [#9717](https://github.com/influxdata/telegraf/pull/9717) Allow collecting node-level metrics for Couchbase buckets
- [#11282](https://github.com/influxdata/telegraf/pull/11282) Make the command config a subcommand
- [#11367](https://github.com/influxdata/telegraf/pull/11367) Migrate collectd parser to new style
- [#11371](https://github.com/influxdata/telegraf/pull/11371) Migrate dropwizard parser to new style
- [#11381](https://github.com/influxdata/telegraf/pull/11381) Migrate form_urlencoded parser to new style
- [#11405](https://github.com/influxdata/telegraf/pull/11405) Migrate graphite parser to new style
- [#11408](https://github.com/influxdata/telegraf/pull/11408) Migrate grok to new parser style
- [#11432](https://github.com/influxdata/telegraf/pull/11432) Migrate influx and influx_upstream parsers to new style
- [#11226](https://github.com/influxdata/telegraf/pull/11226) Migrate json parser to new style
- [#11343](https://github.com/influxdata/telegraf/pull/11343) Migrate json_v2 parser to new style
- [#11366](https://github.com/influxdata/telegraf/pull/11366) Migrate logfmt parser to new style
- [#11402](https://github.com/influxdata/telegraf/pull/11402) Migrate nagios parser to new style
- [#11700](https://github.com/influxdata/telegraf/pull/11700) Migrate to urfave/cli
- [#11407](https://github.com/influxdata/telegraf/pull/11407) Migrate value parser to new style
- [#11374](https://github.com/influxdata/telegraf/pull/11374) Migrate wavefront parser to new style
- [#11373](https://github.com/influxdata/telegraf/pull/11373) `inputs.nats_consumer` Add simple support for jetstream subjects
- [#9015](https://github.com/influxdata/telegraf/pull/9015) `inputs.supervisor` Add Supervisord input plugin
- [#11524](https://github.com/influxdata/telegraf/pull/11524) Tool to build custom Telegraf builds
- [#11493](https://github.com/influxdata/telegraf/pull/11493) `common.tls` Implement minimum TLS version for clients
- [#11619](https://github.com/influxdata/telegraf/pull/11619) `external` Add nsdp external plugin
- [#9890](https://github.com/influxdata/telegraf/pull/9890) `inputs.upsd` Add upsd implementation
- [#11458](https://github.com/influxdata/telegraf/pull/11458) `inputs.cisco_telemetry_mdt` Add GRPC Keepalive/timeout config options
- [#11784](https://github.com/influxdata/telegraf/pull/11784) `inputs.directory_monitor` Support paths for files_to_ignore and files_to_monitor
- [#11773](https://github.com/influxdata/telegraf/pull/11773) `inputs.directory_monitor` Traverse sub-directories
- [#11220](https://github.com/influxdata/telegraf/pull/11220) `inputs.kafka_consumer` Option to set default fetch message bytes
- [#8988](https://github.com/influxdata/telegraf/pull/8988) `inputs.linux_cpu` Add plugin to collect CPU metrics on Linux
- [#9185](https://github.com/influxdata/telegraf/pull/9185) `inputs.logstash` Record number of failures
- [#11469](https://github.com/influxdata/telegraf/pull/11469) `inputs.modbus` Error out on requests with no fields defined
- [#11426](https://github.com/influxdata/telegraf/pull/11426) `inputs.mqtt_consumer` Add incoming mqtt message size calculation
- [#10874](https://github.com/influxdata/telegraf/pull/10874) `inputs.nginx_plus_api` Gather limit_reqs metrics
- [#11593](https://github.com/influxdata/telegraf/pull/11593) `inputs.ntpq` Add option to specify command flags
- [#11592](https://github.com/influxdata/telegraf/pull/11592) `inputs.ntpq` Add possibility to query remote servers
- [#11594](https://github.com/influxdata/telegraf/pull/11594) `inputs.ntpq` Allow to specify `reach` output format
- [#11572](https://github.com/influxdata/telegraf/pull/11572) `inputs.openstack` Add allow_reauth config option for openstack client
- [#11391](https://github.com/influxdata/telegraf/pull/11391) `inputs.smart` Collect SSD endurance information where available in smartctl
- [#11688](https://github.com/influxdata/telegraf/pull/11688) `inputs.sqlserver` Add db name to io stats for MI
- [#11709](https://github.com/influxdata/telegraf/pull/11709) `inputs.sqlserver` Improved filtering for active requests
- [#11518](https://github.com/influxdata/telegraf/pull/11518) `inputs.statsd` Add median timing calculation to statsd input plugin
- [#9440](https://github.com/influxdata/telegraf/pull/9440) `inputs.syslog` Log remote host as source tag
- [#11271](https://github.com/influxdata/telegraf/pull/11271) `inputs.x509_cert` Add smtp protocol
- [#11284](https://github.com/influxdata/telegraf/pull/11284) `output.mqtt` Add support for MQTT protocol version 5
- [#11649](https://github.com/influxdata/telegraf/pull/11649) `outputs.amqp` Add proxy support
- [#11439](https://github.com/influxdata/telegraf/pull/11439) `outputs.graphite` Retry connecting to servers with failed send attempts
- [#11443](https://github.com/influxdata/telegraf/pull/11443) `outputs.groundwork` Improve metric parsing to extend output
- [#11557](https://github.com/influxdata/telegraf/pull/11557) `outputs.iotdb` Add new output plugin to support Apache IoTDB
- [#11672](https://github.com/influxdata/telegraf/pull/11672) `outputs.postgresql` Add Postgresql output
- [#11529](https://github.com/influxdata/telegraf/pull/11529) `outputs.redistimeseries` Add integration test
- [#11551](https://github.com/influxdata/telegraf/pull/11551) `outputs.sql` Add settings for go sql.DB settings
- [#11251](https://github.com/influxdata/telegraf/pull/11251) `parsers.json` Allow JSONata based transformations in JSON serializer
- [#11558](https://github.com/influxdata/telegraf/pull/11558) `parsers.xpath` Add support for returning underlying data-types
- [#11306](https://github.com/influxdata/telegraf/pull/11306) `processors.starlark` Add starlark benchmark for tag-concatenation
- [#11475](https://github.com/influxdata/telegraf/pull/11475) `inputs.rabbitmq` Add support for head_message_timestamp metric
- [#9333](https://github.com/influxdata/telegraf/pull/9333) `inputs.redis` Add Redis 6 ACL auth support
- [#11690](https://github.com/influxdata/telegraf/pull/11690) `serializers.prometheus` Provide option to reduce payload size by removing HELP from payload
- [#9319](https://github.com/influxdata/telegraf/pull/9319) `proxy.x509_cert` Add proxy support

### Dependency Updates

- [#11671](https://github.com/influxdata/telegraf/pull/11671) Update github.com/jackc/pgx/v4 from 4.16.1 to 4.17.0
- [#11669](https://github.com/influxdata/telegraf/pull/11669) Update github.com/Azure/go-autorest/autorest from 0.11.24 to 0.11.28
- [#11670](https://github.com/influxdata/telegraf/pull/11670) Update github.com/aws/aws-sdk-go-v2/service/ec2 from 1.51.2 to 1.52.1
- [#11675](https://github.com/influxdata/telegraf/pull/11675) Update github.com/urfave/cli/v2 from 2.3.0 to 2.11.2
- [#11679](https://github.com/influxdata/telegraf/pull/11679) Update github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.13.6 to 1.13.12
- [#11695](https://github.com/influxdata/telegraf/pull/11695) Update github.com/aliyun/alibaba-cloud-sdk-go from 1.61.1695 to 1.61.1727
- [#11676](https://github.com/influxdata/telegraf/pull/11676) Update go.mongodb.org/mongo-driver from 1.9.1 to 1.10.1
- [#11710](https://github.com/influxdata/telegraf/pull/11710) Update github.com/wavefronthq/wavefront-sdk-go from 0.10.1 to 0.10.2
- [#11711](https://github.com/influxdata/telegraf/pull/11711) Update github.com/aws/aws-sdk-go-v2/service/sts from 1.16.7 to 1.16.13
- [#11716](https://github.com/influxdata/telegraf/pull/11716) Update github.com/aerospike/aerospike-client-go/v5 from 5.7.0 to 5.9.0
- [#11717](https://github.com/influxdata/telegraf/pull/11717) Update github.com/hashicorp/consul/api from 1.13.1 to 1.14.0
- [#11721](https://github.com/influxdata/telegraf/pull/11721) Update github.com/tidwall/gjson from 1.14.1 to 1.14.3
- [#11699](https://github.com/influxdata/telegraf/pull/11699) Update github.com/rabbitmq/amqp091-go from 1.3.4 to 1.4.0
- [#11743](https://github.com/influxdata/telegraf/pull/11743) Update github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.15.10 to 1.16.1
- [#11744](https://github.com/influxdata/telegraf/pull/11744) Update github.com/gophercloud/gophercloud from 0.25.0 to 1.0.0
- [#11745](https://github.com/influxdata/telegraf/pull/11745) Update k8s.io/client-go from 0.24.3 to 0.25.0
- [#11747](https://github.com/influxdata/telegraf/pull/11747) Update github.com/aws/aws-sdk-go-v2/feature/ec2/imds from 1.12.11 to 1.12.13
- [#11763](https://github.com/influxdata/telegraf/pull/11763) Update github.com/urfave/cli/v2 from 2.11.2 to 2.14.1
- [#11764](https://github.com/influxdata/telegraf/pull/11764) Update gonum.org/v1/gonum from 0.11.0 to 0.12.0
- [#11770](https://github.com/influxdata/telegraf/pull/11770) Update github.com/Azure/azure-kusto-go from 0.7.0 to 0.8.0
- [#11746](https://github.com/influxdata/telegraf/pull/11746) Update google.golang.org/grpc from 1.48.0 to 1.49.0

### BREAKING CHANGES

- [#11493](https://github.com/influxdata/telegraf/pull/11493) `common.tls` Set default minimum TLS version to v1.2 for security reasons on both server and client connections. This is a change from the previous defaults (TLS v1.0) on the server configuration and might break clients relying on older TLS versions. You can manually revert to older versions on a per-plugin basis using the `tls_min_version` option in the plugins required

## v1.23.4 [2022-08-16]

### Bugfixes

- [#11647](https://github.com/influxdata/telegraf/pull/11647) Bump github.com/lxc/lxd to be able to run tests
- [#11664](https://github.com/influxdata/telegraf/pull/11664) Sync sql output and input build constraints to handle loong64 in go1.19.
- [#10841](https://github.com/influxdata/telegraf/pull/10841) Updating credentials file to not use endpoint_url parameter
- [#10851](https://github.com/influxdata/telegraf/pull/10851) `inputs.cloudwatch` Customizable batch size when querying
- [#11577](https://github.com/influxdata/telegraf/pull/11577) `inputs.kube_inventory` Send file location to enable token auto-refresh
- [#11578](https://github.com/influxdata/telegraf/pull/11578) `inputs.kubernetes` Refresh token from file at each read
- [#11635](https://github.com/influxdata/telegraf/pull/11635) `inputs.mongodb` Update version check for newer versions
- [#11539](https://github.com/influxdata/telegraf/pull/11539) `inputs.opcua` Return an error with mismatched types
- [#11548](https://github.com/influxdata/telegraf/pull/11548) `inputs.sqlserver` Set lower deadlock priority
- [#11556](https://github.com/influxdata/telegraf/pull/11556) `inputs.stackdriver` Handle when no buckets available
- [#11576](https://github.com/influxdata/telegraf/pull/11576) `inputs` Linter issues
- [#11595](https://github.com/influxdata/telegraf/pull/11595) `outputs` Linter issues
- [#11607](https://github.com/influxdata/telegraf/pull/11607) `parsers` Linter issues

### Features

- [#11622](https://github.com/influxdata/telegraf/pull/11622) Add coralogix dialect to opentelemetry

### Dependency Updates

- [#11412](https://github.com/influxdata/telegraf/pull/11412) `deps` Bump github.com/testcontainers/testcontainers-go from 0.12.0 to 0.13.0
- [#11565](https://github.com/influxdata/telegraf/pull/11565) `deps` Bump github.com/apache/thrift from 0.15.0 to 0.16.0
- [#11567](https://github.com/influxdata/telegraf/pull/11567) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.46.0 to 1.51.0
- [#11494](https://github.com/influxdata/telegraf/pull/11494) `deps` Update all go.opentelemetry.io dependencies
- [#11569](https://github.com/influxdata/telegraf/pull/11569) `deps` Bump github.com/go-ldap/ldap/v3 from 3.4.1 to 3.4.4
- [#11574](https://github.com/influxdata/telegraf/pull/11574) `deps` Bump github.com/karrick/godirwalk from 1.16.1 to 1.17.0
- [#11568](https://github.com/influxdata/telegraf/pull/11568) `deps` Bump github.com/vmware/govmomi from 0.28.0 to 0.29.0
- [#11347](https://github.com/influxdata/telegraf/pull/11347) `deps` Bump github.com/eclipse/paho.mqtt.golang from 1.3.5 to 1.4.1
- [#11580](https://github.com/influxdata/telegraf/pull/11580) `deps` Bump github.com/shirou/gopsutil/v3 from 3.22.4 to 3.22.7
- [#11582](https://github.com/influxdata/telegraf/pull/11582) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs
- [#11583](https://github.com/influxdata/telegraf/pull/11583) `deps` Bump github.com/Azure/go-autorest/autorest/adal
- [#11581](https://github.com/influxdata/telegraf/pull/11581) `deps` Bump github.com/pion/dtls/v2 from 2.0.13 to 2.1.5
- [#11590](https://github.com/influxdata/telegraf/pull/11590) `deps` Bump github.com/Azure/azure-event-hubs-go/v3
- [#11586](https://github.com/influxdata/telegraf/pull/11586) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatch
- [#11585](https://github.com/influxdata/telegraf/pull/11585) `deps` Bump github.com/aws/aws-sdk-go-v2/service/kinesis
- [#11584](https://github.com/influxdata/telegraf/pull/11584) `deps` Bump github.com/aws/aws-sdk-go-v2/service/dynamodb
- [#11598](https://github.com/influxdata/telegraf/pull/11598) `deps` Bump github.com/signalfx/golib/v3 from 3.3.43 to 3.3.45
- [#11605](https://github.com/influxdata/telegraf/pull/11605) `deps` Update github.com/BurntSushi/toml from 0.4.1 to 1.2.0
- [#11604](https://github.com/influxdata/telegraf/pull/11604) `deps` Update cloud.google.com/go/pubsub from 1.23.0 to 1.24.0
- [#11602](https://github.com/influxdata/telegraf/pull/11602) `deps` Update k8s.io/apimachinery from 0.24.2 to 0.24.3
- [#11603](https://github.com/influxdata/telegraf/pull/11603) `deps` Update github.com/Shopify/sarama from 1.34.1 to 1.35.0
- [#11616](https://github.com/influxdata/telegraf/pull/11616) `deps` Bump github.com/sirupsen/logrus from 1.8.1 to 1.9.0
- [#11636](https://github.com/influxdata/telegraf/pull/11636) `deps` Bump github.com/emicklei/go-restful from v2.9.5+incompatible to v3.8.0
- [#11641](https://github.com/influxdata/telegraf/pull/11641) `deps` Bump github.com/hashicorp/consul/api from 1.12.0 to 1.13.1
- [#11640](https://github.com/influxdata/telegraf/pull/11640) `deps` Bump github.com/prometheus/client_golang from 1.12.2 to 1.13.0
- [#11643](https://github.com/influxdata/telegraf/pull/11643) `deps` Bump google.golang.org/api from 0.85.0 to 0.91.0
- [#11644](https://github.com/influxdata/telegraf/pull/11644) `deps` Bump github.com/antchfx/xmlquery from 1.3.9 to 1.3.12
- [#11651](https://github.com/influxdata/telegraf/pull/11651) `deps` Bump github.com/aws/aws-sdk-go-v2/service/ec2
- [#11652](https://github.com/influxdata/telegraf/pull/11652) `deps` Bump github.com/aws/aws-sdk-go-v2/feature/ec2/imds
- [#11653](https://github.com/influxdata/telegraf/pull/11653) `deps` Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs

## v1.23.3 [2022-07-25]

### Bugfixes

- [#11481](https://github.com/influxdata/telegraf/pull/11481) `inputs.openstack` Use v3 volume library
- [#11482](https://github.com/influxdata/telegraf/pull/11482) `common.cookie` Use reader over readcloser, regen cookie-jar at reauth
- [#11527](https://github.com/influxdata/telegraf/pull/11527) `inputs.mqtt_consumer` Topic parsing error when topic having prefix '/'
- [#11534](https://github.com/influxdata/telegraf/pull/11534) `inputs.snmp_trap` Nil map panic when use snmp_trap with netsnmp translator
- [#11522](https://github.com/influxdata/telegraf/pull/11522) `inputs.sqlserver` Set lower deadlock priority on queries
- [#11486](https://github.com/influxdata/telegraf/pull/11486) `parsers.prometheus` Histogram infinity bucket must be always present

### Dependency Updates

- [#11461](https://github.com/influxdata/telegraf/pull/11461) Bump github.com/antchfx/jsonquery from 1.1.5 to 1.2.0

## v1.23.2 [2022-07-11]

### Bugfixes

- [#11460](https://github.com/influxdata/telegraf/pull/11460) Deprecation warnings for non-deprecated packages
- [#11472](https://github.com/influxdata/telegraf/pull/11472) `common.http` Allow 201 for cookies, update header docs
- [#11448](https://github.com/influxdata/telegraf/pull/11448) `inputs.sqlserver` Use bigint for backupsize in sqlserver
- [#11011](https://github.com/influxdata/telegraf/pull/11011) `inputs.gnmi` Refactor tag-only subs for complex keys
- [#10331](https://github.com/influxdata/telegraf/pull/10331) `inputs.snmp` Snmp UseUnconnectedUDPSocket when using udp

### Dependency Updates

- [#11438](https://github.com/influxdata/telegraf/pull/11438) Bump github.com/docker/docker from 20.10.14 to 20.10.17

## v1.23.1 [2022-07-05]

### Bugfixes

- [#11335](https://github.com/influxdata/telegraf/pull/11335) Bring back old xpath section names
- [#9315](https://github.com/influxdata/telegraf/pull/9315) `inputs.rabbitmq` Don't require listeners to be present in overview
- [#11280](https://github.com/influxdata/telegraf/pull/11280) Filter out views in mongodb lookup
- [#11311](https://github.com/influxdata/telegraf/pull/11311) Fix race condition in configuration and prevent concurrent map writes to c.UnusedFields
- [#11397](https://github.com/influxdata/telegraf/pull/11397) Resolve jolokia2 panic on null response
- [#11276](https://github.com/influxdata/telegraf/pull/11276) Restore sample configurations broken during initial migration
- [#11413](https://github.com/influxdata/telegraf/pull/11413) Sync back sample.confs for inputs.couchbase and outputs.groundwork.

### Dependency Updates

- [#11295](https://github.com/influxdata/telegraf/pull/11295) Bump cloud.google.com/go/monitoring from 1.2.0 to 1.5.0
- [#11297](https://github.com/influxdata/telegraf/pull/11297) Bump github.com/aws/aws-sdk-go-v2/credentials from 1.12.2 to 1.12.5
- [#11318](https://github.com/influxdata/telegraf/pull/11318) Bump google.golang.org/grpc from 1.46.2 to 1.47.0
- [#11223](https://github.com/influxdata/telegraf/pull/11223) Bump k8s.io/client-go from 0.23.3 to 0.24.1
- [#11299](https://github.com/influxdata/telegraf/pull/11299) Bump github.com/go-logfmt/logfmt from 0.5.0 to 0.5.1
- [#11328](https://github.com/influxdata/telegraf/pull/11328) Bump github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.15.3 to 1.15.7
- [#11320](https://github.com/influxdata/telegraf/pull/11320) Bump go.mongodb.org/mongo-driver from 1.9.0 to 1.9.1
- [#11321](https://github.com/influxdata/telegraf/pull/11321) Bump github.com/gophercloud/gophercloud from 0.24.0 to 0.25.0
- [#11338](https://github.com/influxdata/telegraf/pull/11338) Bump google.golang.org/api from 0.74.0 to 0.84.0
- [#11340](https://github.com/influxdata/telegraf/pull/11340) Bump github.com/fatih/color from 1.10.0 to 1.13.0
- [#11322](https://github.com/influxdata/telegraf/pull/11322) Bump github.com/aws/aws-sdk-go-v2/service/timestreamwrite from 1.3.2 to 1.13.6
- [#11319](https://github.com/influxdata/telegraf/pull/11319) Bump github.com/Shopify/sarama from 1.32.0 to 1.34.1
- [#11342](https://github.com/influxdata/telegraf/pull/11342) Bump github.com/dynatrace-oss/dynatrace-metric-utils-go from 0.3.0 to 0.5.0
- [#11339](https://github.com/influxdata/telegraf/pull/11339) Bump github.com/nats-io/nats.go from 1.15.0 to 1.16.0
- [#11349](https://github.com/influxdata/telegraf/pull/11349) Bump cloud.google.com/go/pubsub from 1.18.0 to 1.22.2
- [#11369](https://github.com/influxdata/telegraf/pull/11369) Bump go.opentelemetry.io/collector/pdata from 0.52.0 to 0.54.0
- [#11346](https://github.com/influxdata/telegraf/pull/11346) Bump github.com/jackc/pgx/v4 from 4.15.0 to 4.16.1
- [#11379](https://github.com/influxdata/telegraf/pull/11379) Bump cloud.google.com/go/bigquery from 1.8.0 to 1.33.0
- [#11378](https://github.com/influxdata/telegraf/pull/11378) Bump github.com/Azure/azure-kusto-go from 0.6.0 to 0.7.0
- [#11394](https://github.com/influxdata/telegraf/pull/11394) Bump cloud.google.com/go/pubsub from 1.22.2 to 1.23.0
- [#11380](https://github.com/influxdata/telegraf/pull/11380) Bump github.com/aws/aws-sdk-go-v2/service/kinesis from 1.13.0 to 1.15.7
- [#11382](https://github.com/influxdata/telegraf/pull/11382) Bump github.com/aws/aws-sdk-go-v2/service/ec2 from 1.1.0 to 1.46.0
- [#11395](https://github.com/influxdata/telegraf/pull/11395) Bump github.com/golang-jwt/jwt/v4 from 4.4.1 to 4.4.2
- [#11396](https://github.com/influxdata/telegraf/pull/11396) Bump github.com/vmware/govmomi from 0.27.3 to 0.28.0
- [#11415](https://github.com/influxdata/telegraf/pull/11415) Bump github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.15.4 to 1.15.8
- [#11416](https://github.com/influxdata/telegraf/pull/11416) Bump github.com/influxdata/influxdb-observability/otel2influx from 0.2.21 to 0.2.22
- [#11434](https://github.com/influxdata/telegraf/pull/11434) Bump k8s.io/api from 0.24.1 to 0.24.2
- [#11437](https://github.com/influxdata/telegraf/pull/11437) Bump github.com/prometheus/client_golang from 1.12.1 to 1.12.2

## v1.23.0 [2022-06-13]

### Bugfixes

- [#11272](https://github.com/influxdata/telegraf/pull/11272) Add missing build constraints for sqlite
- [#11253](https://github.com/influxdata/telegraf/pull/11253) Always build README-embedder for host-architecture
- [#11140](https://github.com/influxdata/telegraf/pull/11140) Avoid calling sadc with invalid 0 interval
- [#11093](https://github.com/influxdata/telegraf/pull/11093) Check net.Listen() error in tests
- [#11181](https://github.com/influxdata/telegraf/pull/11181) Convert slab plugin to new sample.conf.
- [#10979](https://github.com/influxdata/telegraf/pull/10979) Datadog count metrics
- [#11044](https://github.com/influxdata/telegraf/pull/11044) Deprecate useless database config option
- [#11150](https://github.com/influxdata/telegraf/pull/11150) Doc interval setting for internet speed plugin
- [#11120](https://github.com/influxdata/telegraf/pull/11120) Elasticsearch output float handling test
- [#11151](https://github.com/influxdata/telegraf/pull/11151) Improve slab testing without sudo.
- [#10995](https://github.com/influxdata/telegraf/pull/10995) Log instance name in skip warnings
- [#11069](https://github.com/influxdata/telegraf/pull/11069) Output erroneous namespace and continue instead of error out
- [#11237](https://github.com/influxdata/telegraf/pull/11237) Re-add event to splunk serializer
- [#11143](https://github.com/influxdata/telegraf/pull/11143) Redis plugin goroutine leak triggered by auto reload config mechanism
- [#11082](https://github.com/influxdata/telegraf/pull/11082) Remove any content type from prometheus accept header
- [#11261](https://github.com/influxdata/telegraf/pull/11261) Remove full access permissions
- [#11179](https://github.com/influxdata/telegraf/pull/11179) Search services file in /etc/services and fall back to /usr/etc/services
- [#11217](https://github.com/influxdata/telegraf/pull/11217) Update sample.conf for prometheus
- [#11241](https://github.com/influxdata/telegraf/pull/11241) Upgrade xpath and fix code
- [#11083](https://github.com/influxdata/telegraf/pull/11083) Use readers over closers in http input
- [#11149](https://github.com/influxdata/telegraf/pull/11149) `inputs.burrow` Move Dialer to variable and run `make fmt`
- [#10812](https://github.com/influxdata/telegraf/pull/10812) `outputs.sql` Table existence cache

### Features

- [#10880](https://github.com/influxdata/telegraf/pull/10880) Add ANSI color filter for tail input plugin
- [#11188](https://github.com/influxdata/telegraf/pull/11188) Add constant &#39;algorithm&#39; to the mock plugin
- [#11159](https://github.com/influxdata/telegraf/pull/11159) Add external huebridge input plugin
- [#11076](https://github.com/influxdata/telegraf/pull/11076) Add field key option to set event partition key
- [#10818](https://github.com/influxdata/telegraf/pull/10818) Add fritzbox as external plugin
- [#11037](https://github.com/influxdata/telegraf/pull/11037) Add influx semantic commits checker, checks only last commit.
- [#11039](https://github.com/influxdata/telegraf/pull/11039) Add mount option filtering to disk plugin
- [#11075](https://github.com/influxdata/telegraf/pull/11075) Add slab metrics input plugin
- [#11056](https://github.com/influxdata/telegraf/pull/11056) Allow other fluentd metrics apart from retry_count, buffer_queu…
- [#10918](https://github.com/influxdata/telegraf/pull/10918) Artifactory Webhook Receiver
- [#11000](https://github.com/influxdata/telegraf/pull/11000) Create and push nightly docker images to quay.io
- [#11102](https://github.com/influxdata/telegraf/pull/11102) Do not error if no nodes found for current config with xpath parser
- [#10886](https://github.com/influxdata/telegraf/pull/10886) Generate the plugins sample config
- [#11084](https://github.com/influxdata/telegraf/pull/11084) Google API Auth
- [#10607](https://github.com/influxdata/telegraf/pull/10607) In Lustre input plugin, support collecting per-client stats.
- [#10912](https://github.com/influxdata/telegraf/pull/10912) Migrate aggregator plugins to new sample config format
- [#10924](https://github.com/influxdata/telegraf/pull/10924) Migrate input plugins to new sample config format (A-L)
- [#10926](https://github.com/influxdata/telegraf/pull/10926) Migrate input plugins to new sample config format (M-Z)
- [#10910](https://github.com/influxdata/telegraf/pull/10910) Migrate output plugins to new sample config format
- [#10913](https://github.com/influxdata/telegraf/pull/10913) Migrate processor plugins to new sample config format
- [#11218](https://github.com/influxdata/telegraf/pull/11218) Migrate xpath parser to new style
- [#10885](https://github.com/influxdata/telegraf/pull/10885) Update etc/telegraf.conf and etc/telegraf_windows.conf
- [#6948](https://github.com/influxdata/telegraf/pull/6948) `inputs.burrow` fill more http transport parameters
- [#11141](https://github.com/influxdata/telegraf/pull/11141) `inputs.cpu` Add tags with core id or physical id to cpus
- [#7896](https://github.com/influxdata/telegraf/pull/7896) `inputs.mongodb` Add metrics about files currently open and currently active data handles
- [#10448](https://github.com/influxdata/telegraf/pull/10448) `inputs.nginx_plus_api` Gather slab metrics
- [#11216](https://github.com/influxdata/telegraf/pull/11216) `inputs.sqlserver` Update query store and latch performance counters
- [#10574](https://github.com/influxdata/telegraf/pull/10574) `inputs.vsphere` Collect resource pools metrics and add resource pool tag in VM metrics
- [#11035](https://github.com/influxdata/telegraf/pull/11035) `inputs.intel_powerstat` Add Max Turbo Frequency and introduce improvements
- [#11254](https://github.com/influxdata/telegraf/pull/11254) `inputs.intel_powerstat` Add uncore frequency metrics
- [#10954](https://github.com/influxdata/telegraf/pull/10954) `outputs.http` Support configuration of `MaxIdleConns` and `MaxIdleConnsPerHost`
- [#10853](https://github.com/influxdata/telegraf/pull/10853) `outputs.elasticsearch` Add healthcheck timeout

### Dependency Updates

- [#10970](https://github.com/influxdata/telegraf/pull/10970) Update github.com/wavefronthq/wavefront-sdk-go from 0.9.10 to 0.9.11
- [#11166](https://github.com/influxdata/telegraf/pull/11166) Update github.com/aws/aws-sdk-go-v2/config from 1.15.3 to 1.15.7
- [#11021](https://github.com/influxdata/telegraf/pull/11021) Update github.com/sensu/sensu-go/api/core/v2 from 2.13.0 to 2.14.0
- [#11088](https://github.com/influxdata/telegraf/pull/11088) Update go.opentelemetry.io/otel/metric from 0.28.0 to 0.30.0
- [#11221](https://github.com/influxdata/telegraf/pull/11221) Update github.com/nats-io/nats-server/v2 from 2.7.4 to 2.8.4
- [#11191](https://github.com/influxdata/telegraf/pull/11191) Update golangci-lint from v1.45.2 to v1.46.2
- [#11107](https://github.com/influxdata/telegraf/pull/11107) Update gopsutil from v3.22.3 to v3.22.4 to allow for HOST_PROC_MOUNTINFO.
- [#11242](https://github.com/influxdata/telegraf/pull/11242) Update moby/ipvs dependency from v1.0.1 to v1.0.2
- [#11260](https://github.com/influxdata/telegraf/pull/11260) Update modernc.org/sqlite from v1.10.8 to v1.17.3
- [#11266](https://github.com/influxdata/telegraf/pull/11266) Update github.com/containerd/containerd from v1.5.11 to v1.5.13
- [#11264](https://github.com/influxdata/telegraf/pull/11264) Update github.com/tidwall/gjson from 1.10.2 to 1.14.1

## v1.22.4 [2022-05-16]

### Bugfixes

- [#11045](https://github.com/influxdata/telegraf/pull/11045) `inputs.couchbase` Do not assume metrics will all be of the same length
- [#11043](https://github.com/influxdata/telegraf/pull/11043) `inputs.statsd` Do not error when closing statsd network connection
- [#11030](https://github.com/influxdata/telegraf/pull/11030) `outputs.azure_monitor` Re-init azure monitor http client on context deadline error
- [#11078](https://github.com/influxdata/telegraf/pull/11078) `outputs.wavefront` If no "host" tag is provided do not add "telegraf.host" tag
- [#11042](https://github.com/influxdata/telegraf/pull/11042) Have telegraf service wait for network up in systemd packaging

### Dependency Updates

- [#10722](https://github.com/influxdata/telegraf/pull/10722) `inputs.internet_speed` Update github.com/showwin/speedtest-go from 1.1.4 to 1.1.5
- [#11085](https://github.com/influxdata/telegraf/pull/11085) Update OpenTelemetry plugins to v0.51.0

## v1.22.3 [2022-04-28]

### Bugfixes

- [#10961](https://github.com/influxdata/telegraf/pull/10961) Update Go to 1.18.1
- [#10976](https://github.com/influxdata/telegraf/pull/10976) `inputs.influxdb_listener` Remove duplicate influxdb listener writes with upstream parser
- [#11024](https://github.com/influxdata/telegraf/pull/11024) `inputs.gnmi` Use external xpath parser for gnmi
- [#10925](https://github.com/influxdata/telegraf/pull/10925) `inputs.system` Reduce log level in disk plugin back to original level

## v1.22.2 [2022-04-25]

### Bugfixes

- [#11008](https://github.com/influxdata/telegraf/pull/11008) `inputs.gnmi` Add mutex to gnmi lookup map
- [#11010](https://github.com/influxdata/telegraf/pull/11010) `inputs.gnmi` Use sprint to cast to strings in gnmi
- [#11001](https://github.com/influxdata/telegraf/pull/11001) `inputs.consul_agent` Use correct auth token with consul_agent
- [#10486](https://github.com/influxdata/telegraf/pull/10486) `inputs.mysql` Add mariadb_dialect to address the MariaDB differences in INNODB_METRICS
- [#10923](https://github.com/influxdata/telegraf/pull/10923) `inputs.smart` Correctly parse various numeric forms
- [#10850](https://github.com/influxdata/telegraf/pull/10850) `inputs.aliyuncms` Ensure aliyuncms metrics accept array, fix discovery
- [#10930](https://github.com/influxdata/telegraf/pull/10930) `inputs.aerospike` Statistics query bug
- [#10947](https://github.com/influxdata/telegraf/pull/10947) `inputs.cisco_telemetry_mdt` Align the default value for msg size
- [#10959](https://github.com/influxdata/telegraf/pull/10959) `inputs.cisco_telemetry_mdt` Remove overly verbose info message from cisco mdt
- [#10958](https://github.com/influxdata/telegraf/pull/10958) `outputs.influxdb_v2` Improve influxdb_v2 error message
- [#10932](https://github.com/influxdata/telegraf/pull/10932) `inputs.prometheus` Moved from watcher to informer
- [#11013](https://github.com/influxdata/telegraf/pull/11013) Also allow 0 outputs when using test-wait parameter
- [#11015](https://github.com/influxdata/telegraf/pull/11015) Allow Makefile to work on Windows

### Dependency Updates

- [#10966](https://github.com/influxdata/telegraf/pull/10966) Update github.com/Azure/azure-kusto-go from 0.5.0 to 0.60
- [#10963](https://github.com/influxdata/telegraf/pull/10963) Update opentelemetry from v0.2.10 to v0.2.17
- [#10984](https://github.com/influxdata/telegraf/pull/10984) Update go.opentelemetry.io/collector/pdata from v0.48.0 to v0.49.0
- [#10998](https://github.com/influxdata/telegraf/pull/10998) Update github.com/aws/aws-sdk-go-v2/config from 1.13.1 to 1.15.3
- [#10997](https://github.com/influxdata/telegraf/pull/10997) Update github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs
- [#10975](https://github.com/influxdata/telegraf/pull/10975) Update github.com/aws/aws-sdk-go-v2/credentials from 1.8.0 to 1.11.2
- [#10981](https://github.com/influxdata/telegraf/pull/10981) Update github.com/containerd/containerd from v1.5.9 to v1.5.11
- [#10973](https://github.com/influxdata/telegraf/pull/10973) Update github.com/miekg/dns from 1.1.46 to 1.1.48
- [#10974](https://github.com/influxdata/telegraf/pull/10974) Update github.com/gopcua/opcua from v0.3.1 to v0.3.3
- [#10972](https://github.com/influxdata/telegraf/pull/10972) Update github.com/aws/aws-sdk-go-v2/service/dynamodb
- [#10773](https://github.com/influxdata/telegraf/pull/10773) Update github.com/xdg/scram from 1.0.3 to 1.0.5
- [#10971](https://github.com/influxdata/telegraf/pull/10971) Update go.mongodb.org/mongo-driver from 1.8.3 to 1.9.0
- [#10940](https://github.com/influxdata/telegraf/pull/10940) Update starlark 7a1108eaa012->d1966c6b9fcd

## v1.22.1 [2022-04-06]

### Bugfixes

- [#10937](https://github.com/influxdata/telegraf/pull/10937) Update gonum.org/v1/gonum from 0.9.3 to 0.11.0
- [#10906](https://github.com/influxdata/telegraf/pull/10906) Update github.com/golang-jwt/jwt/v4 from 4.2.0 to 4.4.1
- [#10931](https://github.com/influxdata/telegraf/pull/10931) Update gopsutil and associated dependencies for improved OpenBSD support
- [#10553](https://github.com/influxdata/telegraf/pull/10553) `inputs.sqlserver` Fix inconsistencies in sql*Requests queries
- [#10883](https://github.com/influxdata/telegraf/pull/10883) `agent` Fix default value for logfile rotation interval
- [#10871](https://github.com/influxdata/telegraf/pull/10871) `inputs.zfs` Fix redundant zfs pool tag
- [#10903](https://github.com/influxdata/telegraf/pull/10903) `inputs.vsphere` Update vsphere info message to debug
- [#10866](https://github.com/influxdata/telegraf/pull/10866) `outputs.azure_monitor` Include body in error message
- [#10830](https://github.com/influxdata/telegraf/pull/10830) `processors.topk` Clarify the k and fields topk params
- [#10858](https://github.com/influxdata/telegraf/pull/10858) `outputs.http` Switch HTTP 100 test case values
- [#10859](https://github.com/influxdata/telegraf/pull/10859) `inputs.intel_pmu` Fix slow running intel-pmu test
- [#10860](https://github.com/influxdata/telegraf/pull/10860) `inputs.cloud_pubsub` Skip longer/integration tests on -short mode
- [#10861](https://github.com/influxdata/telegraf/pull/10861) `inputs.cloud_pubsub_push` Reduce timeouts and sleeps

### New External Plugins

- [#10462](https://github.com/influxdata/telegraf/pull/10462) `external.psi` Add psi plugin

## v1.22.0

### Influx Line Protocol Parser

There is an option to use a faster, more memory-efficient
implementation of the Influx Line Protocol parser.

### SNMP Translator

This version introduces an agent setting to select the method of
translating SNMP objects. The agent setting "snmp_translator" can be
"netsnmp" which translates by calling external programs snmptranslate
and snmptable, or "gosmi" which translates using the built-in gosmi
library.

Before version 1.21.0, Telegraf only used the netsnmp method. Versions
1.21.0 through 1.21.4 only used the gosmi method. Since the
translation method is now configurable and "netsnmp" is the default,
users who wish to continue using "gosmi" must add `snmp_translator =
"gosmi"` in the agent section of their config file. See
[#10802](https://github.com/influxdata/telegraf/pull/10802).

### New Input Plugins

- [#3649](https://github.com/influxdata/telegraf/pull/3649) `inputs.socketstat` Add socketstat input plugin
- [#9697](https://github.com/influxdata/telegraf/pull/9697) `inputs.xtremio` Add xtremio input
- [#9782](https://github.com/influxdata/telegraf/pull/9782) `inputs.mock` Add mock input plugin
- [#10042](https://github.com/influxdata/telegraf/pull/10042) `inputs.redis_sentinel` Add redis sentinel input plugin
- [#10106](https://github.com/influxdata/telegraf/pull/10106) `inputs.nomad` Add nomad input plugin
- [#10198](https://github.com/influxdata/telegraf/pull/10198) `inputs.vault` Add vault input plugin
- [#10258](https://github.com/influxdata/telegraf/pull/10258) `inputs.consul_agent` Add consul agent input plugin
- [#10763](https://github.com/influxdata/telegraf/pull/10763) `inputs.hugepages` Add hugepages input plugin

### New Processor Plugins

- [#10057](https://github.com/influxdata/telegraf/pull/10057) `processors.noise` Add noise processor plugin

### Features

- [#9332](https://github.com/influxdata/telegraf/pull/9332) `agent` HTTP basic auth for webhooks
- [#10307](https://github.com/influxdata/telegraf/pull/10307) `agent` Improve error logging on plugin initialization
- [#10341](https://github.com/influxdata/telegraf/pull/10341) `agent` Check TLSConfig early to catch missing certificates
- [#10404](https://github.com/influxdata/telegraf/pull/10404) `agent` Support headers for http plugin with cookie auth
- [#10545](https://github.com/influxdata/telegraf/pull/10545) `agent` Add a collection offset implementation
- [#10559](https://github.com/influxdata/telegraf/pull/10559) `agent` Add autorestart and restartdelay flags to Windows service
- [#10515](https://github.com/influxdata/telegraf/pull/10515) `aggregators.histogram` Add config option to push only updated values
- [#10520](https://github.com/influxdata/telegraf/pull/10520) `aggregators.histogram` Add expiration option
- [#10137](https://github.com/influxdata/telegraf/pull/10137) `inputs.bond` Add additional stats to bond collector
- [#10382](https://github.com/influxdata/telegraf/pull/10382) `inputs.docker` Update docker client API version
- [#10575](https://github.com/influxdata/telegraf/pull/10575) `inputs.file` Allow for stateful parser handling
- [#7484](https://github.com/influxdata/telegraf/pull/7484) `inputs.gnmi` add dynamic tagging to gnmi plugin
- [#10220](https://github.com/influxdata/telegraf/pull/10220) `inputs.graylog` Add timeout setting option
- [#10530](https://github.com/influxdata/telegraf/pull/10530) `inputs.internet_speed` Add caching to internet_speed
- [#10243](https://github.com/influxdata/telegraf/pull/10243) `inputs.kibana` Add heap_size_limit field
- [#10641](https://github.com/influxdata/telegraf/pull/10641) `inputs.memcached` gather additional stats from memcached
- [#10642](https://github.com/influxdata/telegraf/pull/10642) `inputs.memcached` Support client TLS origination
- [#9279](https://github.com/influxdata/telegraf/pull/9279) `inputs.modbus` Support multiple slaves with gateway
- [#10231](https://github.com/influxdata/telegraf/pull/10231) `inputs.modbus` Add per-request tags
- [#10625](https://github.com/influxdata/telegraf/pull/10625) `inputs.mongodb` Add FsTotalSize and FsUsedSize fields
- [#10787](https://github.com/influxdata/telegraf/pull/10787) `inputs.nfsclient` Add new rtt per op field
- [#10705](https://github.com/influxdata/telegraf/pull/10705) `inputs.openweathermap` Add feels_like field
- [#9710](https://github.com/influxdata/telegraf/pull/9710) `inputs.postgresql` Add option to disable prepared statements for PostgreSQL
- [#10339](https://github.com/influxdata/telegraf/pull/10339) `inputs.snmp_trap` Deprecate unused snmp_trap timeout configuration option
- [#9671](https://github.com/influxdata/telegraf/pull/9671) `inputs.sql` Add ClickHouse driver to sql inputs/outputs plugins
- [#10466](https://github.com/influxdata/telegraf/pull/10466) `inputs.statsd` Add option to sanitize collected metric names
- [#9432](https://github.com/influxdata/telegraf/pull/9432) `inputs.varnish` Create option to reduce potentially high cardinality
- [#6501](https://github.com/influxdata/telegraf/pull/6501) `inputs.win_perf_counters` Implemented support for reading raw values, added tests and doc
- [#10535](https://github.com/influxdata/telegraf/pull/10535) `inputs.win_perf_counters` Allow errors to be ignored
- [#9822](https://github.com/influxdata/telegraf/pull/9822) `inputs.x509_cert` Add exclude_root_certs option to x509_cert plugin
- [#9963](https://github.com/influxdata/telegraf/pull/9963) `outputs.datadog` Add the option to use compression
- [#10505](https://github.com/influxdata/telegraf/pull/10505) `outputs.elasticsearch` Add elastic pipeline flags
- [#10499](https://github.com/influxdata/telegraf/pull/10499) `outputs.groundwork` Process group tags
- [#10186](https://github.com/influxdata/telegraf/pull/10186) `outputs.http` Add optional list of non retryable http status codes
- [#10202](https://github.com/influxdata/telegraf/pull/10202) `outputs.http` Support AWS managed service for prometheus
- [#8192](https://github.com/influxdata/telegraf/pull/8192) `outputs.kafka` Add socks5 proxy support
- [#10673](https://github.com/influxdata/telegraf/pull/10673) `outputs.sql` Add unsigned style config option
- [#10672](https://github.com/influxdata/telegraf/pull/10672) `outputs.websocket` Add socks5 proxy support
- [#10267](https://github.com/influxdata/telegraf/pull/10267) `parsers.csv` Add option to skip errors during parsing
- [#10749](https://github.com/influxdata/telegraf/pull/10749) `parsers.influx` Add new influx line protocol parser via feature flag
- [#10585](https://github.com/influxdata/telegraf/pull/10585) `parsers.xpath` Add tag batch-processing to XPath parser
- [#10316](https://github.com/influxdata/telegraf/pull/10316) `processors.template` Add more functionality to template processor
- [#10252](https://github.com/influxdata/telegraf/pull/10252) `serializers.wavefront` Add option to disable Wavefront prefix conversion

### Bugfixes

- [#10803](https://github.com/influxdata/telegraf/pull/10803) `agent` Update parsing logic of config.Duration to correctly require time and duration
- [#10814](https://github.com/influxdata/telegraf/pull/10814) `agent` Update the precision parameter default value
- [#10872](https://github.com/influxdata/telegraf/pull/10872) `agent` Change name of agent snmp translator setting
- [#10876](https://github.com/influxdata/telegraf/pull/10876) `inputs.consul_agent` Rename consul_metrics -> consul_agent
- [#10711](https://github.com/influxdata/telegraf/pull/10711) `inputs.docker` Keep data type of tasks_desired field consistent
- [#10083](https://github.com/influxdata/telegraf/pull/10083) `inputs.http` Add metadata support to CSV parser plugin
- [#10701](https://github.com/influxdata/telegraf/pull/10701) `inputs.mdstat` Fix parsing output when when sync is less than 10%
- [#10385](https://github.com/influxdata/telegraf/pull/10385) `inputs.modbus` Re-enable OpenBSD modbus support
- [#10790](https://github.com/influxdata/telegraf/pull/10790) `inputs.ntpq` Correctly read ntpq long poll output with extra characters
- [#10384](https://github.com/influxdata/telegraf/pull/10384) `inputs.opcua` Accept non-standard OPC UA OK status by implementing a configurable workaround
- [#10465](https://github.com/influxdata/telegraf/pull/10465) `inputs.opcua` Add additional data to error messages
- [#10735](https://github.com/influxdata/telegraf/pull/10735) `inputs.snmp` Log err when loading mibs
- [#10748](https://github.com/influxdata/telegraf/pull/10748) `inputs.snmp` Use the correct path when evaluating symlink
- [#10802](https://github.com/influxdata/telegraf/pull/10802) `inputs.snmp` Add option to select translator
- [#10527](https://github.com/influxdata/telegraf/pull/10527) `inputs.system` Remove verbose logging from disk input plugin
- [#10706](https://github.com/influxdata/telegraf/pull/10706) `outputs.influxdb_v2` Include influxdb bucket name in error messages
- [#10623](https://github.com/influxdata/telegraf/pull/10623) `outputs.groundwork` Set NextCheckTime to LastCheckTime to avoid GroundWork to invent a value
- [#10749](https://github.com/influxdata/telegraf/pull/10749) `parsers.influx` Add new influx line protocol parser via feature flag
- [#10777](https://github.com/influxdata/telegraf/pull/10777) `parsers.json_v2` Allow multiple optional objects
- [#10799](https://github.com/influxdata/telegraf/pull/10799) `parsers.json_v2` Check if gpath exists and support optional in fields/tags
- [#10798](https://github.com/influxdata/telegraf/pull/10798) `parsers.xpath` Correctly handling imports in protocol-buffer definitions
- [#10602](https://github.com/influxdata/telegraf/pull/10602) Update github.com/aws/aws-sdk-go-v2/service/sts from 1.7.2 to 1.14.0
- [#10604](https://github.com/influxdata/telegraf/pull/10604) Update github.com/aerospike/aerospike-client-go from 1.27.0 to 5.7.0
- [#10686](https://github.com/influxdata/telegraf/pull/10686) Update github.com/sleepinggenius2/gosmi from v0.4.3 to v0.4.4
- [#10692](https://github.com/influxdata/telegraf/pull/10692) Update github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.5.0 to 1.13.0
- [#10693](https://github.com/influxdata/telegraf/pull/10693) Update github.com/gophercloud/gophercloud from 0.16.0 to 0.24.0
- [#10702](https://github.com/influxdata/telegraf/pull/10702) Update github.com/jackc/pgx/v4 from 4.14.1 to 4.15.0
- [#10704](https://github.com/influxdata/telegraf/pull/10704) Update github.com/sensu/sensu-go/api/core/v2 from 2.12.0 to 2.13.0
- [#10713](https://github.com/influxdata/telegraf/pull/10713) Update k8s.io/api from 0.23.3 to 0.23.4
- [#10714](https://github.com/influxdata/telegraf/pull/10714) Update cloud.google.com/go/pubsub from 1.17.1 to 1.18.0
- [#10715](https://github.com/influxdata/telegraf/pull/10715) Update github.com/newrelic/newrelic-telemetry-sdk-go from 0.5.1 to 0.8.1
- [#10717](https://github.com/influxdata/telegraf/pull/10717) Update github.com/ClickHouse/clickhouse-go from 1.5.1 to 1.5.4
- [#10718](https://github.com/influxdata/telegraf/pull/10718) Update github.com/wavefronthq/wavefront-sdk-go from 0.9.9 to 0.9.10
- [#10719](https://github.com/influxdata/telegraf/pull/10719) Update github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs from 1.12.0 to 1.13.0
- [#10720](https://github.com/influxdata/telegraf/pull/10720) Update github.com/aws/aws-sdk-go-v2/config from 1.8.3 to 1.13.1
- [#10721](https://github.com/influxdata/telegraf/pull/10721) Update github.com/aws/aws-sdk-go-v2/feature/ec2/imds from 1.6.0 to 1.10.0
- [#10728](https://github.com/influxdata/telegraf/pull/10728) Update github.com/testcontainers/testcontainers-go from 0.11.1 to 0.12.0
- [#10751](https://github.com/influxdata/telegraf/pull/10751) Update github.com/aws/aws-sdk-go-v2/service/dynamodb from 1.13.0 to 1.14.0
- [#10752](https://github.com/influxdata/telegraf/pull/10752) Update github.com/nats-io/nats-server/v2 from 2.7.2 to 2.7.3
- [#10757](https://github.com/influxdata/telegraf/pull/10757) Update github.com/miekg/dns from 1.1.43 to 1.1.46
- [#10758](https://github.com/influxdata/telegraf/pull/10758) Update github.com/shirou/gopsutil/v3 from 3.21.12 to 3.22.2
- [#10759](https://github.com/influxdata/telegraf/pull/10759) Update github.com/aws/aws-sdk-go-v2/feature/ec2/imds from 1.10.0 to 1.11.0
- [#10772](https://github.com/influxdata/telegraf/pull/10772) Update github.com/Shopify/sarama from 1.29.1 to 1.32.0
- [#10807](https://github.com/influxdata/telegraf/pull/10807) Update github.com/nats-io/nats-server/v2 from 2.7.3 to 2.7.4

## v1.21.4 [2022-02-16]

### Bugfixes

- [#10491](https://github.com/influxdata/telegraf/pull/10491) `inputs.docker` Update docker memory usage calculation
- [#10636](https://github.com/influxdata/telegraf/pull/10636) `inputs.ecs` Use current time as timestamp
- [#10551](https://github.com/influxdata/telegraf/pull/10551) `inputs.snmp` Ensure folders do not get loaded more than once
- [#10579](https://github.com/influxdata/telegraf/pull/10579) `inputs.win_perf_counters` Add deprecated warning and version to win_perf_counters option
- [#10635](https://github.com/influxdata/telegraf/pull/10635) `outputs.amqp` Check for nil client before closing in amqp
- [#10179](https://github.com/influxdata/telegraf/pull/10179) `outputs.azure_data_explorer` Lower RAM usage
- [#10513](https://github.com/influxdata/telegraf/pull/10513) `outputs.elasticsearch` Add scheme to fix error in sniffing option
- [#10657](https://github.com/influxdata/telegraf/pull/10657) `parsers.json_v2` Fix timestamp change during execution of json_v2 parser
- [#10618](https://github.com/influxdata/telegraf/pull/10618) `parsers.json_v2` Fix incorrect handling of json_v2 timestamp_path
- [#10468](https://github.com/influxdata/telegraf/pull/10468) `parsers.json_v2` Allow optional paths and handle wrong paths correctly
- [#10547](https://github.com/influxdata/telegraf/pull/10547) `serializers.prometheusremotewrite` Use the correct timestamp unit
- [#10647](https://github.com/influxdata/telegraf/pull/10647) Update all go.opentelemetry.io from 0.24.0 to 0.27.0
- [#10652](https://github.com/influxdata/telegraf/pull/10652) Update github.com/signalfx/golib/v3 from 3.3.38 to 3.3.43
- [#10653](https://github.com/influxdata/telegraf/pull/10653) Update github.com/aliyun/alibaba-cloud-sdk-go from 1.61.1004 to 1.61.1483
- [#10503](https://github.com/influxdata/telegraf/pull/10503) Update github.com/denisenkom/go-mssqldb from 0.10.0 to 0.12.0
- [#10626](https://github.com/influxdata/telegraf/pull/10626) Update github.com/gopcua/opcua from 0.2.3 to 0.3.1
- [#10638](https://github.com/influxdata/telegraf/pull/10638) Update github.com/nats-io/nats-server/v2 from 2.6.5 to 2.7.2
- [#10589](https://github.com/influxdata/telegraf/pull/10589) Update k8s.io/client-go from 0.22.2 to 0.23.3
- [#10601](https://github.com/influxdata/telegraf/pull/10601) Update github.com/aws/aws-sdk-go-v2/service/kinesis from 1.6.0 to 1.13.0
- [#10588](https://github.com/influxdata/telegraf/pull/10588) Update github.com/benbjohnson/clock from 1.1.0 to 1.3.0
- [#10598](https://github.com/influxdata/telegraf/pull/10598) Update github.com/Azure/azure-kusto-go from 0.5.0 to 0.5.2
- [#10571](https://github.com/influxdata/telegraf/pull/10571) Update github.com/vmware/govmomi from 0.27.2 to 0.27.3
- [#10572](https://github.com/influxdata/telegraf/pull/10572) Update github.com/prometheus/client_golang from 1.11.0 to 1.12.1
- [#10564](https://github.com/influxdata/telegraf/pull/10564) Update go.mongodb.org/mongo-driver from 1.7.3 to 1.8.3
- [#10563](https://github.com/influxdata/telegraf/pull/10563) Update github.com/google/go-cmp from 0.5.6 to 0.5.7
- [#10562](https://github.com/influxdata/telegraf/pull/10562) Update go.opentelemetry.io/collector/model from 0.39.0 to 0.43.2
- [#10538](https://github.com/influxdata/telegraf/pull/10538) Update github.com/multiplay/go-ts3 from 1.0.0 to 1.0.1
- [#10454](https://github.com/influxdata/telegraf/pull/10454) Update cloud.google.com/go/monitoring from 0.2.0 to 1.2.0
- [#10536](https://github.com/influxdata/telegraf/pull/10536) Update github.com/vmware/govmomi from 0.26.0 to 0.27.2

### New External Plugins

- [apt](https://github.com/x70b1/telegraf-apt) - contributed by @x70b1
- [knot](https://github.com/x70b1/telegraf-knot) - contributed by @x70b1

## v1.21.3 [2022-01-27]

### Bugfixes

- [#10430](https://github.com/influxdata/telegraf/pull/10430) `inputs.snmp_trap` Fix translation of partially resolved OIDs
- [#10529](https://github.com/influxdata/telegraf/pull/10529) Update deprecation notices
- [#10525](https://github.com/influxdata/telegraf/pull/10525) Update grpc module to v1.44.0
- [#10434](https://github.com/influxdata/telegraf/pull/10434) Update google.golang.org/api module from 0.54.0 to 0.65.0
- [#10507](https://github.com/influxdata/telegraf/pull/10507) Update antchfx/xmlquery module from 1.3.6 to 1.3.9
- [#10521](https://github.com/influxdata/telegraf/pull/10521) Update nsqio/go-nsq module from 1.0.8 to 1.1.0
- [#10506](https://github.com/influxdata/telegraf/pull/10506) Update prometheus/common module from 0.31.1 to 0.32.1
- [#10474](https://github.com/influxdata/telegraf/pull/10474) `inputs.ipset` Fix panic when command not found
- [#10504](https://github.com/influxdata/telegraf/pull/10504) Update cloud.google.com/go/pubsub module from 1.17.0 to 1.17.1
- [#10432](https://github.com/influxdata/telegraf/pull/10432) Update influxdata/influxdb-observability/influx2otel module from 0.2.8 to 0.2.10
- [#10478](https://github.com/influxdata/telegraf/pull/10478) `inputs.opcua` Remove duplicate fields
- [#10473](https://github.com/influxdata/telegraf/pull/10473) `parsers.nagios` Log correct errors when executing commands
- [#10463](https://github.com/influxdata/telegraf/pull/10463) `inputs.execd` Add newline in execd for prometheus parsing
- [#10451](https://github.com/influxdata/telegraf/pull/10451) Update shirou/gopsutil/v3 module from 3.21.10 to 3.21.12
- [#10453](https://github.com/influxdata/telegraf/pull/10453) Update jackc/pgx/v4 module from 4.6.0 to 4.14.1
- [#10449](https://github.com/influxdata/telegraf/pull/10449) Update Azure/azure-event-hubs-go/v3 module from 3.3.13 to 3.3.17
- [#10450](https://github.com/influxdata/telegraf/pull/10450) Update gosnmp/gosnmp module from 1.33.0 to 1.34.0
- [#10442](https://github.com/influxdata/telegraf/pull/10442) `parsers.wavefront` Add missing setting wavefront_disable_prefix_conversion
- [#10435](https://github.com/influxdata/telegraf/pull/10435) Update hashicorp/consul/api module from 1.9.1 to 1.12.0
- [#10436](https://github.com/influxdata/telegraf/pull/10436) Update antchfx/xpath module from 1.1.11 to 1.2.0
- [#10433](https://github.com/influxdata/telegraf/pull/10433) Update antchfx/jsonquery module from 1.1.4 to 1.1.5
- [#10414](https://github.com/influxdata/telegraf/pull/10414) Update prometheus/procfs module from 0.6.0 to 0.7.3
- [#10354](https://github.com/influxdata/telegraf/pull/10354) `inputs.snmp` Fix panic when mibs folder doesn't exist (#10346)
- [#10393](https://github.com/influxdata/telegraf/pull/10393) `outputs.syslog` Correctly set ASCII trailer for syslog output
- [#10415](https://github.com/influxdata/telegraf/pull/10415) Update aws/aws-sdk-go-v2/service/cloudwatchlogs module from 1.5.2 to 1.12.0
- [#10416](https://github.com/influxdata/telegraf/pull/10416) Update kardianos/service module from 1.0.0 to 1.2.1
- [#10396](https://github.com/influxdata/telegraf/pull/10396) `inputs.http` Allow empty http body
- [#10417](https://github.com/influxdata/telegraf/pull/10417) Update couchbase/go-couchbase module from 0.1.0 to 0.1.1
- [#10413](https://github.com/influxdata/telegraf/pull/10413) `parsers.json_v2` Fix timestamp precision when using unix_ns format
- [#10418](https://github.com/influxdata/telegraf/pull/10418) Update pion/dtls/v2 module from 2.0.9 to 2.0.13
- [#10402](https://github.com/influxdata/telegraf/pull/10402) Update containerd/containerd module to 1.5.9
- [#8947](https://github.com/influxdata/telegraf/pull/8947) `outputs.timestream` Fix batching logic with write records and introduce concurrent requests
- [#10360](https://github.com/influxdata/telegraf/pull/10360) `outputs.amqp` Avoid connection leak when writing error
- [#10097](https://github.com/influxdata/telegraf/pull/10097) `outputs.stackdriver` Send correct interval start times for counters

## v1.21.2 [2022-01-05]

### Release Notes

Happy New Year!

### Features

- Added arm64 MacOS builds
- Added riscv64 Linux builds
- Numerous changes to CircleCI config to ensure more timely completion and more clear execution flow

### Bugfixes

- [#10318](https://github.com/influxdata/telegraf/pull/10318) `inputs.disk` Fix missing storage in containers
- [#10324](https://github.com/influxdata/telegraf/pull/10324) `inputs.dpdk` Add note about dpdk and socket availability
- [#10296](https://github.com/influxdata/telegraf/pull/10296) `inputs.logparser` Resolve panic in logparser due to missing Log
- [#10322](https://github.com/influxdata/telegraf/pull/10322) `inputs.snmp` Ensure module load order to avoid snmp marshal error
- [#10321](https://github.com/influxdata/telegraf/pull/10321) `inputs.snmp` Do not require networking during tests
- [#10303](https://github.com/influxdata/telegraf/pull/10303) `inputs.snmp` Resolve SNMP panic due to no gosmi module
- [#10295](https://github.com/influxdata/telegraf/pull/10295) `inputs.snmp` Grab MIB table columns more accurately
- [#10299](https://github.com/influxdata/telegraf/pull/10299) `inputs.snmp` Check index before assignment when floating :: exists to avoid panic
- [#10301](https://github.com/influxdata/telegraf/pull/10301) `inputs.snmp` Fix panic if no mibs folder is found
- [#10373](https://github.com/influxdata/telegraf/pull/10373) `inputs.snmp_trap` Document deprecation of timeout parameter
- [#10377](https://github.com/influxdata/telegraf/pull/10377) `parsers.csv` empty import tzdata for Windows binaries to correctly set timezone
- [#10332](https://github.com/influxdata/telegraf/pull/10332) Update github.com/djherbis/times module from v1.2.0 to v1.5.0
- [#10343](https://github.com/influxdata/telegraf/pull/10343) Update github.com/go-ldap/ldap/v3 module from v3.1.0 to v3.4.1
- [#10255](https://github.com/influxdata/telegraf/pull/10255) Update github.com/gwos/tcg/sdk module from v0.0.0-20211130162655-32ad77586ccf to v0.0.0-20211223101342-35fbd1ae683c and improve logging

## v1.21.1 [2021-12-16]

### Bugfixes

- [#10288](https://github.com/influxdata/telegraf/pull/10288) Fix panic in parsers due to missing Log for all plugins using SetParserFunc.
- [#10288](https://github.com/influxdata/telegraf/pull/10288) Fix panic in parsers due to missing Log for all plugins using SetParserFunc
- [#10247](https://github.com/influxdata/telegraf/pull/10247) Update go-sensu module to v2.12.0
- [#10284](https://github.com/influxdata/telegraf/pull/10284) `inputs.openstack` Fix typo in openstack neutron input plugin (newtron)

### Features

- [#10239](https://github.com/influxdata/telegraf/pull/10239) Enable Darwin arm64 build
- [#10150](https://github.com/influxdata/telegraf/pull/10150) `inputs.smart` Add SMART plugin concurrency configuration option, nvme-cli v1.14+ support and lint fixes.
- [#10150](https://github.com/influxdata/telegraf/pull/10150) `inputs.smart` Add SMART plugin concurrency configuration option, nvme-cli v1.14+ support and lint fixes

## v1.21.0 [2021-12-15]

### Release Notes

The signing for RPM digest has changed to use sha256 to improve security. Please see the pull request for more details: [#10272](https://github.com/influxdata/telegraf/pull/10272).

Thank you to @zak-pawel for lots of linter fixes!

### Bugfixes

- [#10268](https://github.com/influxdata/telegraf/pull/10268) `inputs.snmp` Update snmp plugin to respect number of retries configured
- [#10225](https://github.com/influxdata/telegraf/pull/10225) `outputs.wavefront` Flush wavefront output sender on error to clean up broken connections
- [#9970](https://github.com/influxdata/telegraf/pull/9970) Restart Telegraf service if it is already running and upgraded via RPM
- [#10188](https://github.com/influxdata/telegraf/pull/10188) `parsers.xpath` Handle duplicate registration of protocol-buffer files gracefully
- [#10132](https://github.com/influxdata/telegraf/pull/10132) `inputs.http_listener_v2` Fix panic on close to check that Telegraf is closing
- [#10196](https://github.com/influxdata/telegraf/pull/10196) `outputs.elasticsearch` Implement NaN and inf handling for elasticsearch output
- [#10205](https://github.com/influxdata/telegraf/pull/10205) Print loaded plugins and deprecations for once and test flags
- [#10214](https://github.com/influxdata/telegraf/pull/10214) `processors.ifname` Eliminate MIB dependency for ifname processor
- [#10206](https://github.com/influxdata/telegraf/pull/10206) `inputs.snmp` Optimize locking for SNMP MIBs loading
- [#9975](https://github.com/influxdata/telegraf/pull/9975) `inputs.kube_inventory` Set TLS server name config properly
- [#10230](https://github.com/influxdata/telegraf/pull/10230) Sudden close of Telegraf caused by OPC UA input plugin
- [#9913](https://github.com/influxdata/telegraf/pull/9913) Update eclipse/paho.mqtt.golang module from 1.3.0 to 1.3.5
- [#10221](https://github.com/influxdata/telegraf/pull/10221) `parsers.json_v2` Parser timestamp setting order
- [#10209](https://github.com/influxdata/telegraf/pull/10209) `outputs.graylog` Ensure graylog spec fields not prefixed with _
- [#10099](https://github.com/influxdata/telegraf/pull/10099) `inputs.zfs` Pool detection and metrics gathering for ZFS >= 2.1.x
- [#10007](https://github.com/influxdata/telegraf/pull/10007) `processors.ifname` Parallelism fix for ifname processor
- [#10208](https://github.com/influxdata/telegraf/pull/10208) `inputs.mqtt_consumer` Mqtt topic extracting no longer requires all three fields
- [#9616](https://github.com/influxdata/telegraf/pull/9616) Windows Service - graceful shutdown of telegraf
- [#10203](https://github.com/influxdata/telegraf/pull/10203) Revert unintended corruption of the Makefile
- [#10112](https://github.com/influxdata/telegraf/pull/10112) `inputs.cloudwatch` Cloudwatch metrics collection
- [#10178](https://github.com/influxdata/telegraf/pull/10178) `outputs.all` Register bigquery to output plugins
- [#10165](https://github.com/influxdata/telegraf/pull/10165) `inputs.sysstat` Sysstat to use unique temp file vs hard-coded
- [#10046](https://github.com/influxdata/telegraf/pull/10046) Update nats-sever to support openbsd
- [#10091](https://github.com/influxdata/telegraf/pull/10091) `inputs.prometheus` Check error before defer in prometheus k8s
- [#10101](https://github.com/influxdata/telegraf/pull/10101) `inputs.win_perf_counters` Add setting to win_perf_counters input to ignore localization
- [#10136](https://github.com/influxdata/telegraf/pull/10136) `inputs.snmp_trap` Remove snmptranslate from readme and fix default path
- [#10116](https://github.com/influxdata/telegraf/pull/10116) `inputs.statsd` Input plugin statsd parse error
- [#10131](https://github.com/influxdata/telegraf/pull/10131) Skip knxlistener when writing the sample config
- [#10119](https://github.com/influxdata/telegraf/pull/10119) `inputs.cpu` Update shirou/gopsutil from v2 to v3
- [#10074](https://github.com/influxdata/telegraf/pull/10074) `outputs.graylog` Failing test due to port already in use
- [#9865](https://github.com/influxdata/telegraf/pull/9865) `inputs.directory_monitor` Directory monitor input plugin when data format is CSV and csv_skip_rows>0 and csv_header_row_count>=1
- [#9862](https://github.com/influxdata/telegraf/pull/9862) `outputs.graylog` Graylog plugin TLS support and message format
- [#9908](https://github.com/influxdata/telegraf/pull/9908) `parsers.json_v2` Remove dead code
- [#9881](https://github.com/influxdata/telegraf/pull/9881) `outputs.graylog` Mute graylog UDP/TCP tests by marking them as integration
- [#9751](https://github.com/influxdata/telegraf/pull/9751)  Update google.golang.org/grpc module from 1.39.1 to 1.40.0

### Features

- [#10200](https://github.com/influxdata/telegraf/pull/10200) `aggregators.deprecations.go` Implement deprecation infrastructure
- [#9518](https://github.com/influxdata/telegraf/pull/9518) `inputs.snmp` Snmp to use gosmi
- [#10130](https://github.com/influxdata/telegraf/pull/10130) `outputs.influxdb_v2` Add retry to 413 errors with InfluxDB output
- [#10144](https://github.com/influxdata/telegraf/pull/10144) `inputs.win_services` Add exclude filter
- [#9995](https://github.com/influxdata/telegraf/pull/9995) `inputs.mqtt_consumer` Enable extracting tag values from MQTT topics
- [#9419](https://github.com/influxdata/telegraf/pull/9419) `aggregators.all` Add support of aggregator as Starlark script
- [#9561](https://github.com/influxdata/telegraf/pull/9561) `processors.regex` Extend regexp processor do allow renaming of measurements, tags and fields
- [#8184](https://github.com/influxdata/telegraf/pull/8184) `outputs.http` Add use_batch_format for HTTP output plugin
- [#9988](https://github.com/influxdata/telegraf/pull/9988) `inputs.kafka_consumer` Add max_processing_time config to Kafka Consumer input
- [#9841](https://github.com/influxdata/telegraf/pull/9841) `inputs.sqlserver` Add additional metrics to support elastic pool (sqlserver plugin)
- [#9910](https://github.com/influxdata/telegraf/pull/9910) `common.tls` Filter client certificates by DNS names
- [#9942](https://github.com/influxdata/telegraf/pull/9942) `outputs.azure_data_explorer` Add option to skip table creation in azure data explorer output
- [#9984](https://github.com/influxdata/telegraf/pull/9984) `processors.ifname` Add more details to logmessages
- [#9833](https://github.com/influxdata/telegraf/pull/9833) `common.kafka` Add metadata full to config
- [#9876](https://github.com/influxdata/telegraf/pull/9876) Update etc/telegraf.conf and etc/telegraf_windows.conf
- [#9256](https://github.com/influxdata/telegraf/pull/9256) `inputs.modbus` Modbus connection settings (serial)
- [#9860](https://github.com/influxdata/telegraf/pull/9860) `inputs.directory_monitor` Adds the ability to create and name a tag containing the filename using the directory monitor input plugin
- [#9740](https://github.com/influxdata/telegraf/pull/9740) `inputs.prometheus` Add ignore_timestamp option
- [#9513](https://github.com/influxdata/telegraf/pull/9513) `processors.starlark` Starlark processor example for processing sparkplug_b messages
- [#9449](https://github.com/influxdata/telegraf/pull/9449) `parsers.json_v2` Support defining field/tag tables within an object table
- [#9827](https://github.com/influxdata/telegraf/pull/9827) `inputs.elasticsearch_query` Add debug query output to elasticsearch_query
- [#9241](https://github.com/influxdata/telegraf/pull/9241) `inputs.snmp` Telegraf to merge tables with different indexes
- [#9013](https://github.com/influxdata/telegraf/pull/9013) `inputs.opcua` Allow user to select the source for the metric timestamp.
- [#9706](https://github.com/influxdata/telegraf/pull/9706) `inputs.puppetagent` Add measurements from puppet 5
- [#9644](https://github.com/influxdata/telegraf/pull/9644) `outputs.graylog` Add graylog plugin TCP support
- [#8229](https://github.com/influxdata/telegraf/pull/8229) `outputs.azure_data_explorer` Add json_timestamp_layout option

### New Input Plugins

- [#9724](https://github.com/influxdata/telegraf/pull/9724) Add intel_pmu plugin
- [#9771](https://github.com/influxdata/telegraf/pull/9771) Add Linux Volume Manager input plugin
- [#9236](https://github.com/influxdata/telegraf/pull/9236) Openstack input plugin

### New Output Plugins

- [#9891](https://github.com/influxdata/telegraf/pull/9891)  Add new groundwork output plugin
- [#9923](https://github.com/influxdata/telegraf/pull/9923)  Add mongodb output plugin
- [#9346](https://github.com/influxdata/telegraf/pull/9346)  Azure Event Hubs output plugin

## v1.20.4 [2021-11-17]

### Release Notes

- [#10073](https://github.com/influxdata/telegraf/pull/10073) Update go version from 1.17.2 to 1.17.3
- [#10100](https://github.com/influxdata/telegraf/pull/10100) Update deprecated plugin READMEs to better indicate deprecation

Thank you to @zak-pawel for lots of linter fixes!

- [#9986](https://github.com/influxdata/telegraf/pull/9986) Linter fixes for plugins/inputs/[h-j]*
- [#9999](https://github.com/influxdata/telegraf/pull/9999) Linter fixes for plugins/inputs/[k-l]*
- [#10006](https://github.com/influxdata/telegraf/pull/10006) Linter fixes for plugins/inputs/m*
- [#10011](https://github.com/influxdata/telegraf/pull/10011) Linter fixes for plugins/inputs/[n-o]*

### Bugfixes

- [#10089](https://github.com/influxdata/telegraf/pull/10089) Update BurntSushi/toml from 0.3.1 to 0.4.1
- [#10075](https://github.com/influxdata/telegraf/pull/10075) `inputs.mongodb` Update readme with correct connection URI
- [#10076](https://github.com/influxdata/telegraf/pull/10076) Update gosnmp module from 1.32 to 1.33
- [#9966](https://github.com/influxdata/telegraf/pull/9966) `inputs.mysql` Fix type conversion follow-up
- [#10068](https://github.com/influxdata/telegraf/pull/10068) `inputs.proxmox` Changed VM ID from string to int
- [#10047](https://github.com/influxdata/telegraf/pull/10047) `inputs.modbus` Do not build modbus on openbsd
- [#10019](https://github.com/influxdata/telegraf/pull/10019) `inputs.cisco_telemetry_mdt` Move to new protobuf library
- [#10001](https://github.com/influxdata/telegraf/pull/10001) `outputs.loki` Add metric name with label "__name"
- [#9980](https://github.com/influxdata/telegraf/pull/9980) `inputs.nvidia_smi` Set the default path correctly
- [#10010](https://github.com/influxdata/telegraf/pull/10010) Update go.opentelemetry.io/otel from v0.23.0 to v0.24.0
- [#10044](https://github.com/influxdata/telegraf/pull/10044) `inputs.sqlserver` Add elastic pool in supported versions in sqlserver
- [#10029](https://github.com/influxdata/telegraf/pull/10029) `inputs.influxdb` Update influxdb input schema docs
- [#10026](https://github.com/influxdata/telegraf/pull/10026) `inputs.intel_rdt` Correct timezone handling

## v1.20.3 [2021-10-27]

### Release Notes

- [#9873](https://github.com/influxdata/telegraf/pull/9873) Update go to 1.17.2

### Bugfixes

- [#9948](https://github.com/influxdata/telegraf/pull/9948) Update github.com/aws/aws-sdk-go-v2/config module from 1.8.2 to 1.8.3
- [#9997](https://github.com/influxdata/telegraf/pull/9997) `inputs.ipmi_sensor` Redact IPMI password in logs
- [#9978](https://github.com/influxdata/telegraf/pull/9978) `inputs.kube_inventory` Do not skip resources with zero s/ns timestamps
- [#9998](https://github.com/influxdata/telegraf/pull/9998) Update gjson module to v1.10.2
- [#9973](https://github.com/influxdata/telegraf/pull/9973) `inputs.procstat` Revert and fix tag creation
- [#9943](https://github.com/influxdata/telegraf/pull/9943) `inputs.sqlserver` Add sqlserver plugin integration tests
- [#9647](https://github.com/influxdata/telegraf/pull/9647) `inputs.cloudwatch` Use the AWS SDK v2 library
- [#9954](https://github.com/influxdata/telegraf/pull/9954) `processors.starlark` Starlark pop operation for non-existing keys
- [#9956](https://github.com/influxdata/telegraf/pull/9956) `inputs.zfs` Check return code of zfs command for FreeBSD
- [#9585](https://github.com/influxdata/telegraf/pull/9585) `inputs.kube_inventory` Fix segfault in ingress, persistentvolumeclaim, statefulset in kube_inventory
- [#9901](https://github.com/influxdata/telegraf/pull/9901) `inputs.ethtool` Add normalization of tags for ethtool input plugin
- [#9957](https://github.com/influxdata/telegraf/pull/9957) `inputs.internet_speed` Resolve missing latency field
- [#9662](https://github.com/influxdata/telegraf/pull/9662) `inputs.prometheus` Decode Prometheus scrape path from Kubernetes labels
- [#9933](https://github.com/influxdata/telegraf/pull/9933) `inputs.procstat` Correct conversion of int with specific bit size
- [#9940](https://github.com/influxdata/telegraf/pull/9940) `inputs.webhooks` Provide more fields for papertrail event webhook
- [#9892](https://github.com/influxdata/telegraf/pull/9892) `inputs.mongodb` Solve compatibility issue for mongodb inputs when using 5.x relicaset
- [#9768](https://github.com/influxdata/telegraf/pull/9768) Update github.com/Azure/azure-kusto-go module from 0.3.2 to 0.4.0
- [#9904](https://github.com/influxdata/telegraf/pull/9904) Update github.com/golang-jwt/jwt/v4 module from 4.0.0 to 4.1.0
- [#9921](https://github.com/influxdata/telegraf/pull/9921) Update github.com/apache/thrift module from 0.14.2 to 0.15.0
- [#9403](https://github.com/influxdata/telegraf/pull/9403) `inputs.mysql`Fix inconsistent metric types in mysql
- [#9905](https://github.com/influxdata/telegraf/pull/9905) Update github.com/docker/docker module from 20.10.7+incompatible to 20.10.9+incompatible
- [#9920](https://github.com/influxdata/telegraf/pull/9920) `inputs.prometheus` Move err check to correct place
- [#9869](https://github.com/influxdata/telegraf/pull/9869) Update github.com/prometheus/common module from 0.26.0 to 0.31.1
- [#9866](https://github.com/influxdata/telegraf/pull/9866) Update snowflake database driver module to 1.6.2
- [#9527](https://github.com/influxdata/telegraf/pull/9527) `inputs.intel_rdt` Allow sudo usage
- [#9893](https://github.com/influxdata/telegraf/pull/9893) Update github.com/jaegertracing/jaeger module from 1.15.1 to 1.26.0

### New External Plugins

- [IBM DB2](https://github.com/bonitoo-io/telegraf-input-db2) - contributed by @sranka
- [Oracle Database](https://github.com/bonitoo-io/telegraf-input-oracle) - contributed by @sranka

## v1.20.2 [2021-10-07]

### Bugfixes

- [#9878](https://github.com/influxdata/telegraf/pull/9878) `inputs.cloudwatch` Use new session API
- [#9872](https://github.com/influxdata/telegraf/pull/9872) `parsers.json_v2` Duplicate line_protocol when using object and fields
- [#9787](https://github.com/influxdata/telegraf/pull/9787) `parsers.influx` Fix memory leak in influx parser
- [#9880](https://github.com/influxdata/telegraf/pull/9880) `inputs.stackdriver` Migrate to cloud.google.com/go/monitoring/apiv3/v2
- [#9887](https://github.com/influxdata/telegraf/pull/9887) Fix makefile typo that prevented i386 tar and rpm packages from being built

## v1.20.1 [2021-10-06]

### Bugfixes

- [#9776](https://github.com/influxdata/telegraf/pull/9776) Update k8s.io/apimachinery module from 0.21.1 to 0.22.2
- [#9864](https://github.com/influxdata/telegraf/pull/9864) Update containerd module to v1.5.7
- [#9863](https://github.com/influxdata/telegraf/pull/9863) Update consul module to v1.11.0
- [#9846](https://github.com/influxdata/telegraf/pull/9846) `inputs.mongodb` Fix panic due to nil dereference
- [#9850](https://github.com/influxdata/telegraf/pull/9850) `inputs.intel_rdt` Prevent timeout when logging
- [#9848](https://github.com/influxdata/telegraf/pull/9848) `outputs.loki` Update http_headers setting to match sample config
- [#9808](https://github.com/influxdata/telegraf/pull/9808) `inputs.procstat` Add missing tags
- [#9803](https://github.com/influxdata/telegraf/pull/9803) `outputs.mqtt` Add keep alive config option and documentation around issue with eclipse/mosquitto version
- [#9800](https://github.com/influxdata/telegraf/pull/9800) Fix output buffer never completely flushing
- [#9458](https://github.com/influxdata/telegraf/pull/9458) `inputs.couchbase` Fix insecure certificate validation
- [#9797](https://github.com/influxdata/telegraf/pull/9797) `inputs.opentelemetry` Fix error returned to OpenTelemetry client
- [#9789](https://github.com/influxdata/telegraf/pull/9789) Update github.com/testcontainers/testcontainers-go module from 0.11.0 to 0.11.1
- [#9791](https://github.com/influxdata/telegraf/pull/9791) Update github.com/Azure/go-autorest/autorest/adal module
- [#9678](https://github.com/influxdata/telegraf/pull/9678) Update github.com/Azure/go-autorest/autorest/azure/auth module from 0.5.6 to 0.5.8
- [#9769](https://github.com/influxdata/telegraf/pull/9769) Update cloud.google.com/go/pubsub module from 1.15.0 to 1.17.0
- [#9770](https://github.com/influxdata/telegraf/pull/9770) Update github.com/aws/smithy-go module from 1.3.1 to 1.8.0

### Features

- [#9838](https://github.com/influxdata/telegraf/pull/9838) `inputs.elasticsearch_query` Add custom time/date format field

## v1.20.0 [2021-09-17]

### Release Notes

- [#9642](https://github.com/influxdata/telegraf/pull/9642) Build with Golang 1.17

### Bugfixes

- [#9700](https://github.com/influxdata/telegraf/pull/9700) Update thrift module to 0.14.2 and zipkin-go-opentracing to 0.4.5
- [#9587](https://github.com/influxdata/telegraf/pull/9587) `outputs.opentelemetry` Use headers config in grpc requests
- [#9713](https://github.com/influxdata/telegraf/pull/9713) Update runc module to v1.0.0-rc95 to address CVE-2021-30465
- [#9699](https://github.com/influxdata/telegraf/pull/9699) Migrate dgrijalva/jwt-go to golang-jwt/jwt/v4
- [#9139](https://github.com/influxdata/telegraf/pull/9139) `serializers.prometheus` Update timestamps and expiration time as new data arrives
- [#9625](https://github.com/influxdata/telegraf/pull/9625) `outputs.graylog` Output timestamp with fractional seconds
- [#9655](https://github.com/influxdata/telegraf/pull/9655) Update cloud.google.com/go/pubsub module from 1.2.0 to 1.15.0
- [#9674](https://github.com/influxdata/telegraf/pull/9674) `inputs.mongodb` Change command based on server version
- [#9676](https://github.com/influxdata/telegraf/pull/9676) `outputs.dynatrace` Remove hardcoded int value
- [#9619](https://github.com/influxdata/telegraf/pull/9619) `outputs.influxdb_v2` Increase accepted retry-after header values.
- [#9652](https://github.com/influxdata/telegraf/pull/9652) Update tinylib/msgp module from 1.1.5 to 1.1.6
- [#9471](https://github.com/influxdata/telegraf/pull/9471) `inputs.sql` Make timeout apply to single query
- [#9760](https://github.com/influxdata/telegraf/pull/9760) Update shirou/gopsutil module to 3.21.8
- [#9707](https://github.com/influxdata/telegraf/pull/9707) `inputs.logstash` Add additional logstash output plugin stats
- [#9656](https://github.com/influxdata/telegraf/pull/9656) Update miekg/dns module from 1.1.31 to 1.1.43
- [#9750](https://github.com/influxdata/telegraf/pull/9750) Update antchfx/xmlquery module from 1.3.5 to 1.3.6
- [#9757](https://github.com/influxdata/telegraf/pull/9757) `parsers.registry.go` Fix panic for non-existing metric names
- [#9677](https://github.com/influxdata/telegraf/pull/9677) Update Azure/azure-event-hubs-go/v3 module from 3.2.0 to 3.3.13
- [#9653](https://github.com/influxdata/telegraf/pull/9653) Update prometheus/client_golang module from 1.7.1 to 1.11.0
- [#9693](https://github.com/influxdata/telegraf/pull/9693) `inputs.cloudwatch` Fix pagination error
- [#9727](https://github.com/influxdata/telegraf/pull/9727) `outputs.http` Add error message logging
- [#9718](https://github.com/influxdata/telegraf/pull/9718) Update influxdata/influxdb-observability module from 0.2.4 to 0.2.7
- [#9560](https://github.com/influxdata/telegraf/pull/9560) Update gopcua/opcua module
- [#9544](https://github.com/influxdata/telegraf/pull/9544) `inputs.couchbase` Fix memory leak
- [#9588](https://github.com/influxdata/telegraf/pull/9588) `outputs.opentelemetry` Use attributes setting

### Features

- [#9665](https://github.com/influxdata/telegraf/pull/9665) `inputs.systemd_units` feat(plugins/inputs/systemd_units): add pattern support
- [#9598](https://github.com/influxdata/telegraf/pull/9598) `outputs.sql` Add bool datatype
- [#9386](https://github.com/influxdata/telegraf/pull/9386) `inputs.cloudwatch` Pull metrics from multiple AWS CloudWatch namespaces
- [#9411](https://github.com/influxdata/telegraf/pull/9411) `inputs.cloudwatch` Support AWS Web Identity Provider
- [#9570](https://github.com/influxdata/telegraf/pull/9570) `inputs.modbus` Add support for RTU over TCP
- [#9488](https://github.com/influxdata/telegraf/pull/9488) `inputs.procstat` Support cgroup globs and include systemd unit children
- [#9322](https://github.com/influxdata/telegraf/pull/9322) `inputs.suricata` Support alert event type
- [#5464](https://github.com/influxdata/telegraf/pull/5464) `inputs.prometheus` Add ability to query Consul Service catalog
- [#8641](https://github.com/influxdata/telegraf/pull/8641) `outputs.prometheus_client` Add Landing page
- [#9529](https://github.com/influxdata/telegraf/pull/9529) `inputs.http_listener_v2` Allows multiple paths and add path_tag
- [#9395](https://github.com/influxdata/telegraf/pull/9395) Add cookie authentication to HTTP input and output plugins
- [#8454](https://github.com/influxdata/telegraf/pull/8454) `inputs.syslog` Add RFC3164 support
- [#9351](https://github.com/influxdata/telegraf/pull/9351) `inputs.jenkins` Add option to include nodes by name
- [#9277](https://github.com/influxdata/telegraf/pull/9277) Add JSON, MessagePack, and Protocol-buffers format support to the XPath parser
- [#9343](https://github.com/influxdata/telegraf/pull/9343) `inputs.snmp_trap` Improve MIB lookup performance
- [#9342](https://github.com/influxdata/telegraf/pull/9342) `outputs.newrelic` Add option to override metric_url
- [#9306](https://github.com/influxdata/telegraf/pull/9306) `inputs.smart` Add power mode status
- [#9762](https://github.com/influxdata/telegraf/pull/9762) `inputs.bond` Add count of bonded slaves (for easier alerting)
- [#9675](https://github.com/influxdata/telegraf/pull/9675) `outputs.dynatrace` Remove special handling from counters and update dynatrace-oss/dynatrace-metric-utils-go module to 0.3.0

### New Input Plugins

- [#9602](https://github.com/influxdata/telegraf/pull/9602) Add rocm_smi input to monitor AMD GPUs
- [#9101](https://github.com/influxdata/telegraf/pull/9101) Add mdstat input to gather from /proc/mdstat collection
- [#3536](https://github.com/influxdata/telegraf/pull/3536) Add Elasticsearch query input
- [#9623](https://github.com/influxdata/telegraf/pull/9623) Add internet Speed Monitor Input Plugin

### New Output Plugins

- [#9228](https://github.com/influxdata/telegraf/pull/9228) Add OpenTelemetry output
- [#9426](https://github.com/influxdata/telegraf/pull/9426) Add Azure Data Explorer(ADX) output

## v1.19.3 [2021-08-18]

### Bugfixes

- [#9639](https://github.com/influxdata/telegraf/pull/9639) Update sirupsen/logrus module from 1.7.0 to 1.8.1
- [#9638](https://github.com/influxdata/telegraf/pull/9638) Update testcontainers/testcontainers-go module from 0.11.0 to 0.11.1
- [#9637](https://github.com/influxdata/telegraf/pull/9637) Update golang/snappy module from 0.0.3 to 0.0.4
- [#9636](https://github.com/influxdata/telegraf/pull/9636) Update aws/aws-sdk-go-v2 module from 1.3.2 to 1.8.0
- [#9605](https://github.com/influxdata/telegraf/pull/9605) `inputs.prometheus` Fix prometheus kubernetes pod discovery
- [#9606](https://github.com/influxdata/telegraf/pull/9606) `inputs.redis` Improve redis commands documentation
- [#9566](https://github.com/influxdata/telegraf/pull/9566) `outputs.cratedb` Replace dots in tag keys with underscores
- [#9401](https://github.com/influxdata/telegraf/pull/9401) `inputs.clickhouse` Fix panic, improve handling empty result set
- [#9583](https://github.com/influxdata/telegraf/pull/9583) `inputs.opcua` Avoid closing session on a closed connection
- [#9576](https://github.com/influxdata/telegraf/pull/9576) `processors.aws` Refactor ec2 init for config-api
- [#9571](https://github.com/influxdata/telegraf/pull/9571) `outputs.loki` Sort logs by timestamp before writing to Loki
- [#9524](https://github.com/influxdata/telegraf/pull/9524) `inputs.opcua` Fix reconnection regression introduced in 1.19.1
- [#9581](https://github.com/influxdata/telegraf/pull/9581) `inputs.kube_inventory` Fix k8s nodes and pods parsing error
- [#9577](https://github.com/influxdata/telegraf/pull/9577) Update sensu/go module to v2.9.0
- [#9554](https://github.com/influxdata/telegraf/pull/9554) `inputs.postgresql` Normalize unix socket path
- [#9565](https://github.com/influxdata/telegraf/pull/9565) Update hashicorp/consul/api module to 1.9.1
- [#9552](https://github.com/influxdata/telegraf/pull/9552) `inputs.vsphere` Update vmware/govmomi module to v0.26.0 in order to support vSphere 7.0
- [#9550](https://github.com/influxdata/telegraf/pull/9550) `inputs.opcua` Do not skip good quality nodes after a bad quality node is encountered

## v1.19.2 [2021-07-28]

### Release Notes

- [#9542](https://github.com/influxdata/telegraf/pull/9542) Update Go to v1.16.6

### Bugfixes

- [#9363](https://github.com/influxdata/telegraf/pull/9363) `outputs.dynatrace` Update dynatrace output to allow optional default dimensions
- [#9526](https://github.com/influxdata/telegraf/pull/9526) `outputs.influxdb` Fix metrics reported as written but not actually written
- [#9549](https://github.com/influxdata/telegraf/pull/9549) `inputs.kube_inventory` Prevent segfault in persistent volume claims
- [#9503](https://github.com/influxdata/telegraf/pull/9503) `inputs.nsq_consumer` Fix connection error when not using server setting
- [#9540](https://github.com/influxdata/telegraf/pull/9540) `inputs.sql` Fix handling bool column
- [#9387](https://github.com/influxdata/telegraf/pull/9387) Linter fixes for plugins/inputs/[fg]*
- [#9438](https://github.com/influxdata/telegraf/pull/9438) `inputs.kubernetes` Attach the pod labels to kubernetes_pod_volume and kubernetes_pod_network metrics
- [#9519](https://github.com/influxdata/telegraf/pull/9519) `processors.ifname` Fix SNMP empty metric name
- [#8587](https://github.com/influxdata/telegraf/pull/8587) `inputs.sqlserver` Add tempdb troubleshooting stats and missing V2 query metrics
- [#9323](https://github.com/influxdata/telegraf/pull/9323) `inputs.x509_cert` Prevent x509_cert from hanging on UDP connection
- [#9504](https://github.com/influxdata/telegraf/pull/9504) `parsers.json_v2` Simplify how nesting is handled
- [#9493](https://github.com/influxdata/telegraf/pull/9493) `inputs.mongodb` Switch to official mongo-go-driver module to fix SSL auth failure
- [#9491](https://github.com/influxdata/telegraf/pull/9491) `outputs.dynatrace` Fix panic caused by uninitialized loggedMetrics map
- [#9497](https://github.com/influxdata/telegraf/pull/9497) `inputs.prometheus` Fix prometheus cadvisor authentication
- [#9520](https://github.com/influxdata/telegraf/pull/9520) `parsers.json_v2` Add support for large uint64 and int64 numbers
- [#9447](https://github.com/influxdata/telegraf/pull/9447) `inputs.statsd` Fix regression that didn't allow integer percentiles
- [#9466](https://github.com/influxdata/telegraf/pull/9466) `inputs.sqlserver` Provide detailed error message in telegraf log
- [#9399](https://github.com/influxdata/telegraf/pull/9399) Update dynatrace-metric-utils-go module to v0.2.0
- [#8108](https://github.com/influxdata/telegraf/pull/8108) `inputs.cgroup` Allow multiple keys when parsing cgroups
- [#9479](https://github.com/influxdata/telegraf/pull/9479) `parsers.json_v2` Fix json_v2 parser to handle nested objects in arrays properly

### Features

- [#9485](https://github.com/influxdata/telegraf/pull/9485) Add option to automatically reload settings when config file is modified

## v1.19.1 [2021-07-07]

### Bugfixes

- [#9388](https://github.com/influxdata/telegraf/pull/9388) `inputs.sqlserver` Require authentication method to be specified
- [#9456](https://github.com/influxdata/telegraf/pull/9456) `inputs.kube_inventory` Fix segfault in kube_inventory
- [#9448](https://github.com/influxdata/telegraf/pull/9448) `inputs.couchbase` Fix panic
- [#9444](https://github.com/influxdata/telegraf/pull/9444) `inputs.knx_listener` Fix nil pointer panic
- [#9446](https://github.com/influxdata/telegraf/pull/9446) `inputs.procstat` Update gopsutil module to fix panic
- [#9443](https://github.com/influxdata/telegraf/pull/9443) `inputs.rabbitmq` Fix JSON unmarshall regression
- [#9369](https://github.com/influxdata/telegraf/pull/9369) Update nat-server module to v2.2.6
- [#9429](https://github.com/influxdata/telegraf/pull/9429) `inputs.dovecot` Exclude read-timeout from being an error
- [#9423](https://github.com/influxdata/telegraf/pull/9423) `inputs.statsd` Don't stop parsing after parsing error
- [#9370](https://github.com/influxdata/telegraf/pull/9370) Update apimachinary module to v0.21.1
- [#9373](https://github.com/influxdata/telegraf/pull/9373) Update jwt module to v1.2.2 and jwt-go module to v3.2.3
- [#9412](https://github.com/influxdata/telegraf/pull/9412) Update couchbase Module to v0.1.0
- [#9366](https://github.com/influxdata/telegraf/pull/9366) `inputs.snmp` Add a check for oid and name to prevent empty metrics
- [#9413](https://github.com/influxdata/telegraf/pull/9413) `outputs.http` Fix toml error when parsing insecure_skip_verify
- [#9400](https://github.com/influxdata/telegraf/pull/9400) `inputs.x509_cert` Fix 'source' tag for https
- [#9375](https://github.com/influxdata/telegraf/pull/9375) Update signalfx module to v3.3.34
- [#9406](https://github.com/influxdata/telegraf/pull/9406) `parsers.json_v2` Don't require tags to be added to included_keys
- [#9289](https://github.com/influxdata/telegraf/pull/9289) `inputs.x509_cert` Fix SNI support
- [#9372](https://github.com/influxdata/telegraf/pull/9372) Update gjson module to v1.8.0
- [#9379](https://github.com/influxdata/telegraf/pull/9379) Linter fixes for plugins/inputs/[de]*

## v1.19.0 [2021-06-17]

### Release Notes

- Many linter fixes - thanks @zak-pawel and all!
- [#9331](https://github.com/influxdata/telegraf/pull/9331) Update Go to 1.16.5

### Bugfixes

- [#9182](https://github.com/influxdata/telegraf/pull/9182) Update pgx to v4
- [#9275](https://github.com/influxdata/telegraf/pull/9275) Fix reading config files starting with http:
- [#9196](https://github.com/influxdata/telegraf/pull/9196) `serializers.prometheusremotewrite` Update dependency and remove tags with empty values
- [#9051](https://github.com/influxdata/telegraf/pull/9051) `outputs.kafka` Don't prevent telegraf from starting when there's a connection error
- [#8795](https://github.com/influxdata/telegraf/pull/8795) `parsers.prometheusremotewrite` Update prometheus dependency to v2.21.0
- [#9295](https://github.com/influxdata/telegraf/pull/9295) `outputs.dynatrace` Use dynatrace-metric-utils
- [#9368](https://github.com/influxdata/telegraf/pull/9368) `parsers.json_v2` Update json_v2 parser to handle null types
- [#9359](https://github.com/influxdata/telegraf/pull/9359) `inputs.sql` Fix import of sqlite and ignore it on all platforms that require CGO.
- [#9329](https://github.com/influxdata/telegraf/pull/9329) `inputs.kube_inventory` Fix connecting to the wrong url
- [#9358](https://github.com/influxdata/telegraf/pull/9358) upgrade denisenkom go-mssql to v0.10.0
- [#9283](https://github.com/influxdata/telegraf/pull/9283) `processors.parser` Fix segfault
- [#9243](https://github.com/influxdata/telegraf/pull/9243) `inputs.docker` Close all idle connections
- [#9338](https://github.com/influxdata/telegraf/pull/9338) `inputs.suricata` Support new JSON format
- [#9296](https://github.com/influxdata/telegraf/pull/9296) `outputs.influxdb` Fix endless retries

### Features

- [#8987](https://github.com/influxdata/telegraf/pull/8987) Config file environment variable can be a URL
- [#9297](https://github.com/influxdata/telegraf/pull/9297) `outputs.datadog` Add HTTP proxy to datadog output
- [#9087](https://github.com/influxdata/telegraf/pull/9087) Add named timestamp formats
- [#9276](https://github.com/influxdata/telegraf/pull/9276) `inputs.vsphere` Add config option for the historical interval duration
- [#9274](https://github.com/influxdata/telegraf/pull/9274) `inputs.ping` Add an option to specify packet size
- [#9007](https://github.com/influxdata/telegraf/pull/9007) Allow multiple "--config" and "--config-directory" flags
- [#9249](https://github.com/influxdata/telegraf/pull/9249) `outputs.graphite` Allow more characters in graphite tags
- [#8351](https://github.com/influxdata/telegraf/pull/8351) `inputs.sqlserver` Added login_name
- [#9223](https://github.com/influxdata/telegraf/pull/9223) `inputs.dovecot` Add support for unix domain sockets
- [#9118](https://github.com/influxdata/telegraf/pull/9118) `processors.strings` Add UTF-8 sanitizer
- [#9156](https://github.com/influxdata/telegraf/pull/9156) `inputs.aliyuncms` Add config option list of regions to query
- [#9138](https://github.com/influxdata/telegraf/pull/9138) `common.http` Add OAuth2 to HTTP input
- [#8822](https://github.com/influxdata/telegraf/pull/8822) `inputs.sqlserver` Enable Azure Active Directory (AAD) authentication support
- [#9136](https://github.com/influxdata/telegraf/pull/9136) `inputs.cloudwatch` Add wildcard support in dimensions configuration
- [#5517](https://github.com/influxdata/telegraf/pull/5517) `inputs.mysql` Gather all mysql channels
- [#8911](https://github.com/influxdata/telegraf/pull/8911) `processors.enum` Support float64
- [#9105](https://github.com/influxdata/telegraf/pull/9105) `processors.starlark` Support nanosecond resolution timestamp
- [#9080](https://github.com/influxdata/telegraf/pull/9080) `inputs.logstash` Add support for version 7 queue stats
- [#9074](https://github.com/influxdata/telegraf/pull/9074) `parsers.prometheusremotewrite` Add starlark script for renaming metrics
- [#9032](https://github.com/influxdata/telegraf/pull/9032) `inputs.couchbase` Add ~200 more Couchbase metrics via Buckets endpoint
- [#8596](https://github.com/influxdata/telegraf/pull/8596) `inputs.sqlserver` input/sqlserver: Add service and save connection pools
- [#9042](https://github.com/influxdata/telegraf/pull/9042) `processors.starlark` Add math module
- [#6952](https://github.com/influxdata/telegraf/pull/6952) `inputs.x509_cert` Wildcard support for cert filenames
- [#9004](https://github.com/influxdata/telegraf/pull/9004) `processors.starlark` Add time module
- [#8891](https://github.com/influxdata/telegraf/pull/8891) `inputs.kinesis_consumer` Add content_encoding option with gzip and zlib support
- [#8996](https://github.com/influxdata/telegraf/pull/8996) `processors.starlark` Add an example showing how to obtain IOPS from diskio input
- [#8966](https://github.com/influxdata/telegraf/pull/8966) `inputs.http_listener_v2` Add support for snappy compression
- [#8661](https://github.com/influxdata/telegraf/pull/8661) `inputs.cisco_telemetry_mdt` Add support for events and class based query
- [#8861](https://github.com/influxdata/telegraf/pull/8861) `inputs.mongodb` Optionally collect top stats
- [#8979](https://github.com/influxdata/telegraf/pull/8979) `parsers.value` Add custom field name config option
- [#8544](https://github.com/influxdata/telegraf/pull/8544) `inputs.sqlserver` Add an optional health metric

### New Input Plugins

- [Alibaba CloudMonitor Service (Aliyun)](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/aliyuncms) - contributed by @i-prudnikov
- [OpenTelemetry](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/opentelemetry) - contributed by @jacobmarble
- [Intel Data Plane Development Kit (DPDK)](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/dpdk) - contributed by @p-zak
- [KNX](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/knx_listener) - contributed by @DocLambda
- [SQL](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/sql) - contributed by @srebhan

### New Output Plugins

- [Websocket](https://github.com/influxdata/telegraf/tree/master/plugins/outputs/websocket) - contributed by @FZambia
- [SQL](https://github.com/influxdata/telegraf/tree/master/plugins/outputs/sql) - contributed by @illuusio
- [AWS Cloudwatch logs](https://github.com/influxdata/telegraf/tree/master/plugins/outputs/cloudwatch_logs) - contributed by @i-prudnikov

### New Parser Plugins

- [Prometheus Remote Write](https://github.com/influxdata/telegraf/tree/master/plugins/parsers/prometheusremotewrite) - contributed by @helenosheaa
- [JSON V2](https://github.com/influxdata/telegraf/tree/master/plugins/parsers/json_v2) - contributed by @sspaink

### New External Plugins

- [ldap_org and ds389](https://github.com/falon/CSI-telegraf-plugins) - contributed by @falon
- [x509_crl](https://github.com/jcgonnard/telegraf-input-x590crl) - contributed by @jcgonnard
- [dnsmasq](https://github.com/machinly/dnsmasq-telegraf-plugin) - contributed by @machinly
- [Big Blue Button](https://github.com/bigblueswarm/bigbluebutton-telegraf-plugin) - contributed by @SLedunois

## v1.18.3 [2021-05-20]

### Release Notes

- Added FreeBSD armv7 build

### Bugfixes

- [#9271](https://github.com/influxdata/telegraf/pull/9271) `inputs.prometheus` Set user agent when scraping prom metrics
- [#9203](https://github.com/influxdata/telegraf/pull/9203) Migrate from soniah/gosnmp to gosnmp/gosnmp and update to 1.32.0
- [#9169](https://github.com/influxdata/telegraf/pull/9169) `inputs.kinesis_consumer` Fix repeating parser error
- [#9130](https://github.com/influxdata/telegraf/pull/9130) `inputs.sqlserver` Remove disallowed whitespace from sqlServerRingBufferCPU query
- [#9238](https://github.com/influxdata/telegraf/pull/9238) Update hashicorp/consul/api module to v1.8.1
- [#9235](https://github.com/influxdata/telegraf/pull/9235) Migrate from docker/libnetwork/ipvs to moby/ipvs
- [#9224](https://github.com/influxdata/telegraf/pull/9224) Update shirou/gopsutil to 3.21.3
- [#9209](https://github.com/influxdata/telegraf/pull/9209) Update microsoft/ApplicationInsights-Go to 0.4.4
- [#9190](https://github.com/influxdata/telegraf/pull/9190) Update gogo/protobuf to 1.3.2
- [#8746](https://github.com/influxdata/telegraf/pull/8746) Update Azure/go-autorest/autorest/azure/auth to 0.5.6 and Azure/go-autorest/autorest to 0.11.17
- [#8745](https://github.com/influxdata/telegraf/pull/8745) Update collectd.org to 0.5.0
- [#8716](https://github.com/influxdata/telegraf/pull/8716) Update nats-io/nats.go 1.10.0
- [#9039](https://github.com/influxdata/telegraf/pull/9039) Update golang/protobuf to v1.5.1
- [#8937](https://github.com/influxdata/telegraf/pull/8937) Migrate from ericchiang/k8s to kubernetes/client-go

### Features

- [#8913](https://github.com/influxdata/telegraf/pull/8913) `outputs.elasticsearch` Add ability to enable gzip compression

## v1.18.2 [2021-04-28]

### Bugfixes

- [#9160](https://github.com/influxdata/telegraf/pull/9160) `processors.converter` Add support for large hexadecimal strings
- [#9195](https://github.com/influxdata/telegraf/pull/9195) `inputs.apcupsd` Fix apcupsd 'ALARMDEL' bug via forked repo
- [#9110](https://github.com/influxdata/telegraf/pull/9110) `parsers.json` Make JSON format compatible with nulls
- [#9128](https://github.com/influxdata/telegraf/pull/9128) `inputs.nfsclient` Fix nfsclient ops map to allow collection of metrics other than read and write
- [#8917](https://github.com/influxdata/telegraf/pull/8917) `inputs.snmp` Log snmpv3 auth failures
- [#8892](https://github.com/influxdata/telegraf/pull/8892) `common.shim` Accept larger inputs from scanner
- [#9045](https://github.com/influxdata/telegraf/pull/9045) `inputs.vsphere` Add MetricLookback setting to handle reporting delays in vCenter 6.7 and later
- [#9026](https://github.com/influxdata/telegraf/pull/9026) `outputs.sumologic` Carbon2 serializer: sanitize metric name
- [#9086](https://github.com/influxdata/telegraf/pull/9086) `inputs.opcua` Fix error handling

## v1.18.1 [2021-04-07]

### Bugfixes

- [#9082](https://github.com/influxdata/telegraf/pull/9082) `inputs.mysql` Fix 'binary logs' query for MySQL 8
- [#9069](https://github.com/influxdata/telegraf/pull/9069) `inputs.tail` Add configurable option for the 'path' tag override
- [#9067](https://github.com/influxdata/telegraf/pull/9067) `inputs.nfsclient` Fix integer overflow in fields from mountstat
- [#9050](https://github.com/influxdata/telegraf/pull/9050) `inputs.snmp` Fix init when no mibs are installed
- [#9072](https://github.com/influxdata/telegraf/pull/9072) `inputs.ping` Always call SetPrivileged(true) in native mode
- [#9043](https://github.com/influxdata/telegraf/pull/9043) `processors.ifname` Get interface name more efficiently
- [#9056](https://github.com/influxdata/telegraf/pull/9056) `outputs.yandex_cloud_monitoring` Use correct compute metadata URL to get folder-id
- [#9048](https://github.com/influxdata/telegraf/pull/9048) `outputs.azure_monitor` Handle error when initializing the auth object
- [#8549](https://github.com/influxdata/telegraf/pull/8549) `inputs.sqlserver` Fix sqlserver_process_cpu calculation
- [#9035](https://github.com/influxdata/telegraf/pull/9035) `inputs.ipmi_sensor` Fix panic
- [#9009](https://github.com/influxdata/telegraf/pull/9009) `inputs.docker` Fix panic when parsing container stats
- [#8333](https://github.com/influxdata/telegraf/pull/8333) `inputs.exec` Don't truncate messages in debug mode
- [#8769](https://github.com/influxdata/telegraf/pull/8769) `agent` Close running outputs when reloadinlg

## v1.18.0 [2021-03-17]

### Release Notes

- Support Go version 1.16.2
- Added support for code signing in Windows

### Bugfixes

- [#7312](https://github.com/influxdata/telegraf/pull/7312) `inputs.docker` CPU stats respect perdevice
- [#8397](https://github.com/influxdata/telegraf/pull/8397) `outputs.dynatrace` Dynatrace Plugin: Make conversion to counters possible / Changed large bulk handling
- [#8655](https://github.com/influxdata/telegraf/pull/8655) `inputs.sqlserver` SqlServer - fix for default server list
- [#8703](https://github.com/influxdata/telegraf/pull/8703) `inputs.docker` Use consistent container name in docker input plugin
- [#8902](https://github.com/influxdata/telegraf/pull/8902) `inputs.snmp` Fix max_repetitions signedness issues
- [#8817](https://github.com/influxdata/telegraf/pull/8817) `outputs.kinesis` outputs.kinesis - log record error count
- [#8833](https://github.com/influxdata/telegraf/pull/8833) `inputs.sqlserver` Bug Fix - SQL Server HADR queries for SQL Versions
- [#8628](https://github.com/influxdata/telegraf/pull/8628) `inputs.modbus` fix: reading multiple holding registers in modbus input plugin
- [#8885](https://github.com/influxdata/telegraf/pull/8885) `inputs.statsd` Fix statsd concurrency bug
- [#8393](https://github.com/influxdata/telegraf/pull/8393) `inputs.sqlserver` SQL Perfmon counters - synced queries from v2 to all db types
- [#8873](https://github.com/influxdata/telegraf/pull/8873) `processors.ifname` Fix mutex locking around ifname cache
- [#8720](https://github.com/influxdata/telegraf/pull/8720) `parsers.influx` fix: remove ambiguity on '\v' from line-protocol parser
- [#8678](https://github.com/influxdata/telegraf/pull/8678) `inputs.redis` Fix Redis output field type inconsistencies
- [#8953](https://github.com/influxdata/telegraf/pull/8953) `agent` Reset the flush interval timer when flush is requested or batch is ready.
- [#8954](https://github.com/influxdata/telegraf/pull/8954) `common.kafka` Fix max open requests to one if idempotent writes is set to true
- [#8721](https://github.com/influxdata/telegraf/pull/8721) `inputs.kube_inventory` Set $HOSTIP in default URL
- [#8995](https://github.com/influxdata/telegraf/pull/8995) `inputs.sflow` fix segfaults in sflow plugin by checking if protocol headers are set
- [#8986](https://github.com/influxdata/telegraf/pull/8986) `outputs.nats` nats_output: use the configured credentials file

### Features

- [#8887](https://github.com/influxdata/telegraf/pull/8887) `inputs.procstat` Add PPID field to procstat input plugin
- [#8852](https://github.com/influxdata/telegraf/pull/8852) `processors.starlark` Add Starlark script for estimating Line Protocol cardinality
- [#8915](https://github.com/influxdata/telegraf/pull/8915) `inputs.cloudwatch` add proxy
- [#8910](https://github.com/influxdata/telegraf/pull/8910) `agent` Display error message on badly formatted config string array (eg. namepass)
- [#8785](https://github.com/influxdata/telegraf/pull/8785) `inputs.diskio` Non systemd support with unittest
- [#8850](https://github.com/influxdata/telegraf/pull/8850) `inputs.snmp` Support more snmpv3 authentication protocols
- [#8813](https://github.com/influxdata/telegraf/pull/8813) `inputs.redfish` added member_id as tag(as it is a unique value) for redfish plugin and added address of the server when the status is other than 200 for better debugging
- [#8613](https://github.com/influxdata/telegraf/pull/8613) `inputs.phpfpm` Support exclamation mark to create non-matching list in tail plugin
- [#8179](https://github.com/influxdata/telegraf/pull/8179) `inputs.statsd` Add support for datadog distributions metric
- [#8803](https://github.com/influxdata/telegraf/pull/8803) `agent` Add default retry for load config via url
- [#8816](https://github.com/influxdata/telegraf/pull/8816) Code Signing for Windows
- [#8772](https://github.com/influxdata/telegraf/pull/8772) `processors.starlark` Allow to provide constants to a starlark script
- [#8749](https://github.com/influxdata/telegraf/pull/8749) `outputs.newrelic` Add HTTP proxy setting to New Relic output plugin
- [#8543](https://github.com/influxdata/telegraf/pull/8543) `inputs.elasticsearch` Add configurable number of 'most recent' date-stamped indices to gather in Elasticsearch input
- [#8675](https://github.com/influxdata/telegraf/pull/8675) `processors.starlark` Add Starlark parsing example of nested JSON
- [#8762](https://github.com/influxdata/telegraf/pull/8762) `inputs.prometheus` Optimize for bigger kubernetes clusters (500+ pods)
- [#8950](https://github.com/influxdata/telegraf/pull/8950) `inputs.teamspeak` Teamspeak input plugin query clients
- [#8849](https://github.com/influxdata/telegraf/pull/8849) `inputs.sqlserver` Filter data out from system databases for Azure SQL DB only

### New Inputs

- [Beat Input Plugin](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/beat) - Contributed by @nferch
- [CS:GO Input Plugin](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/csgo) - Contributed by @oofdog
- [Directory Monitoring Input Plugin](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/directory_monitor) - Contributed by @InfluxData
- [RavenDB Input Plugin](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/ravendb) - Contributed by @ml054 and @bartoncasey
- [NFS Input Plugin](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/nfsclient) - Contributed by @pmoranga

### New Outputs

- [Grafana Loki Output Plugin](https://github.com/influxdata/telegraf/tree/master/plugins/outputs/loki) - Contributed by @Eraac
- [Google BigQuery Output Plugin](https://github.com/influxdata/telegraf/tree/master/plugins/outputs/loki) - Contributed by @gkatzioura
- [Sensu Output Plugin](https://github.com/influxdata/telegraf/blob/master/plugins/outputs/sensu) - Contributed by @calebhailey
- [SignalFX Output Plugin](https://github.com/influxdata/telegraf/tree/master/plugins/outputs/signalfx) - Contributed by @keitwb

### New Aggregators

- [Derivative Aggregator Plugin](https://github.com/influxdata/telegraf/tree/master/plugins/aggregators/derivative) - Contributed by @KarstenSchnitter
- [Quantile Aggregator Plugin](https://github.com/influxdata/telegraf/tree/master/plugins/aggregators/quantile) - Contributed by @srebhan

### New Processors

- [AWS EC2 Metadata Processor Plugin](https://github.com/influxdata/telegraf/tree/master/plugins/processors/aws/ec2) - Contributed by @pmalek-sumo

### New Parsers

- [XML Parser Plugin](https://github.com/influxdata/telegraf/tree/master/plugins/parsers/xml) - Contributed by @srebhan

### New Serializers

- [MessagePack Serializer Plugin](https://github.com/influxdata/telegraf/tree/master/plugins/serializers/msgpack) - Contributed by @dialogbox

### New External Plugins

- [GeoIP Processor Plugin](https://github.com/a-bali/telegraf-geoip) - Contributed by @a-bali
- [Plex Webhook Input Plugin](https://github.com/russorat/telegraf-webhooks-plex) - Contributed by @russorat
- [SMCIPMITool Input Plugin](https://github.com/jhpope/smc_ipmi) - Contributed by @jhpope

## v1.17.3 [2021-02-17]

### Bugfixes

- [#7316](https://github.com/influxdata/telegraf/pull/7316) `inputs.filestat` plugins/filestat: Skip missing files
- [#8868](https://github.com/influxdata/telegraf/pull/8868) Update to Go 1.15.8
- [#8744](https://github.com/influxdata/telegraf/pull/8744) Bump github.com/gopcua/opcua from 0.1.12 to 0.1.13
- [#8657](https://github.com/influxdata/telegraf/pull/8657) `outputs.warp10` outputs/warp10: url encode comma in tags value
- [#8824](https://github.com/influxdata/telegraf/pull/8824) `inputs.x509_cert` inputs.x509_cert: Fix timeout issue
- [#8821](https://github.com/influxdata/telegraf/pull/8821) `inputs.mqtt_consumer` Fix reconnection issues mqtt
- [#8775](https://github.com/influxdata/telegraf/pull/8775) `outputs.influxdb` Validate the response from InfluxDB after writing/creating a database to avoid json parsing panics/errors
- [#8804](https://github.com/influxdata/telegraf/pull/8804) `inputs.snmp` Expose v4/v6-only connection-schemes through GosnmpWrapper
- [#8838](https://github.com/influxdata/telegraf/pull/8838) `agent` fix issue with reading flush_jitter output from config
- [#8839](https://github.com/influxdata/telegraf/pull/8839) `inputs.ping` fixes Sort and timeout around deadline
- [#8787](https://github.com/influxdata/telegraf/pull/8787) `inputs.ping` Update README for inputs.ping with correct cmd for native ping on Linux
- [#8771](https://github.com/influxdata/telegraf/pull/8771) Update go-ping to latest version

## v1.17.2 [2021-01-28]

### Bugfixes

- [#8770](https://github.com/influxdata/telegraf/pull/8770) `inputs.ping` Set interface for native
- [#8764](https://github.com/influxdata/telegraf/pull/8764) `inputs.ping` Resolve regression, re-add missing function

## v1.17.1 [2021-01-27]

### Release Notes

Included a few more changes that add configuration options to plugins as it's been while since the last release

- [#8335](https://github.com/influxdata/telegraf/pull/8335) `inputs.ipmi_sensor` Add setting to enable caching in ipmitool
- [#8616](https://github.com/influxdata/telegraf/pull/8616) Add Event Log support for Windows
- [#8602](https://github.com/influxdata/telegraf/pull/8602) `inputs.postgresql_extensible` Add timestamp column support to postgresql_extensible
- [#8627](https://github.com/influxdata/telegraf/pull/8627) `parsers.csv` Added ability to define skip values in csv parser
- [#8055](https://github.com/influxdata/telegraf/pull/8055) `outputs.http` outputs/http: add option to control idle connection timeout
- [#7897](https://github.com/influxdata/telegraf/pull/7897) `common.tls` common/tls: Allow specifying SNI hostnames
- [#8541](https://github.com/influxdata/telegraf/pull/8541) `inputs.snmp` Extended the internal snmp wrapper to support AES192, AES192C, AES256, and AES256C
- [#6165](https://github.com/influxdata/telegraf/pull/6165) `inputs.procstat` Provide method to include core count when reporting cpu_usage in procstat input
- [#8287](https://github.com/influxdata/telegraf/pull/8287) `inputs.jenkins` Add support for an inclusive job list in Jenkins plugin
- [#8524](https://github.com/influxdata/telegraf/pull/8524) `inputs.ipmi_sensor` Add hex_key parameter for IPMI input plugin connection

### Bugfixes

- [#8662](https://github.com/influxdata/telegraf/pull/8662) `outputs.influxdb_v2` [outputs.influxdb_v2] add exponential backoff, and respect client error responses
- [#8748](https://github.com/influxdata/telegraf/pull/8748) `outputs.elasticsearch` Fix issue with elasticsearch output being really noisy about some errors
- [#7533](https://github.com/influxdata/telegraf/pull/7533) `inputs.zookeeper` improve mntr regex to match user specific keys.
- [#7967](https://github.com/influxdata/telegraf/pull/7967) `inputs.lustre2` Fix crash in lustre2 input plugin, when field name and value
- [#8673](https://github.com/influxdata/telegraf/pull/8673) Update grok-library to v1.0.1 with dots and dash-patterns fixed.
- [#8679](https://github.com/influxdata/telegraf/pull/8679) `inputs.ping` Use go-ping for "native" execution in Ping plugin
- [#8741](https://github.com/influxdata/telegraf/pull/8741) `inputs.x509_cert` fix x509 cert timeout issue
- [#8714](https://github.com/influxdata/telegraf/pull/8714) Bump github.com/nsqio/go-nsq from 1.0.7 to 1.0.8
- [#8715](https://github.com/influxdata/telegraf/pull/8715) Bump github.com/Shopify/sarama from 1.27.1 to 1.27.2
- [#8712](https://github.com/influxdata/telegraf/pull/8712) Bump github.com/newrelic/newrelic-telemetry-sdk-go from 0.2.0 to 0.5.1
- [#8659](https://github.com/influxdata/telegraf/pull/8659) `inputs.gnmi` GNMI plugin should not take off the first character of field keys when no 'alias path' exists.
- [#8609](https://github.com/influxdata/telegraf/pull/8609) `inputs.webhooks` Use the 'measurement' json field from the particle webhook as the measurement name, or if it's blank, use the 'name' field of the event's json.
- [#8658](https://github.com/influxdata/telegraf/pull/8658) `inputs.procstat` Procstat input plugin should use the same timestamp in all metrics in the same Gather() cycle.
- [#8391](https://github.com/influxdata/telegraf/pull/8391) `aggregators.merge` Optimize SeriesGrouper & aggregators.merge
- [#8545](https://github.com/influxdata/telegraf/pull/8545) `inputs.prometheus` Using mime-type in prometheus parser to handle protocol-buffer responses
- [#8588](https://github.com/influxdata/telegraf/pull/8588) `inputs.snmp` Input SNMP plugin - upgrade gosnmp library to version 1.29.0
- [#8502](https://github.com/influxdata/telegraf/pull/8502) `inputs.http_listener_v2` Fix Stop() bug when plugin fails to start

### New External Plugins

- [#8646](https://github.com/influxdata/telegraf/pull/8646) [Open Hardware Monitoring](https://github.com/marianob85/open_hardware_monitor-telegraf-plugin) Input Plugin

## v1.17.0 [2020-12-18]

### Release Notes

- Starlark plugins can now store state between runs using a global state variable. This lets you make custom aggregators as well as custom processors that are state-aware.
- New input plugins: Riemann-Protobuff Listener, Intel PowerStat
- New output plugins: Yandex.Cloud monitoring, Logz.io
- New parser plugin: Prometheus
- New serializer: Prometheus remote write

### Bugfixes

- [#8505](https://github.com/influxdata/telegraf/pull/8505) `inputs.vsphere` Fixed misspelled check for datacenter
- [#8499](https://github.com/influxdata/telegraf/pull/8499) `processors.execd` Adding support for new lines in influx line protocol fields.
- [#8254](https://github.com/influxdata/telegraf/pull/8254) `serializers.carbon2` Fix carbon2 tests
- [#8498](https://github.com/influxdata/telegraf/pull/8498) `inputs.http_response` fixed network test
- [#8414](https://github.com/influxdata/telegraf/pull/8414) `inputs.bcache` Fix tests for Windows - part 1
- [#8577](https://github.com/influxdata/telegraf/pull/8577) `inputs.ping` fix potential issue with race condition
- [#8562](https://github.com/influxdata/telegraf/pull/8562) `inputs.mqtt_consumer` fix issue with mqtt concurrent map write
- [#8574](https://github.com/influxdata/telegraf/pull/8574) `inputs.ecs` Remove duplicated field "revision" from ecs_task because it's already defined as a tag there
- [#8551](https://github.com/influxdata/telegraf/pull/8551) `inputs.socket_listener` fix crash when socket_listener receiving invalid data
- [#8564](https://github.com/influxdata/telegraf/pull/8564) `parsers.graphite` Graphite tags parser
- [#8472](https://github.com/influxdata/telegraf/pull/8472) `inputs.kube_inventory` Fixing issue with missing metrics when pod has only pending containers
- [#8542](https://github.com/influxdata/telegraf/pull/8542) `inputs.aerospike` fix edge case in aerospike plugin where an expected hex string was converted to integer if all digits
- [#8512](https://github.com/influxdata/telegraf/pull/8512) `inputs.kube_inventory` Update string parsing of allocatable cpu cores in kube_inventory

### Features

- [#8038](https://github.com/influxdata/telegraf/pull/8038) `inputs.jenkins` feat: add build number field to jenkins_job measurement
- [#7345](https://github.com/influxdata/telegraf/pull/7345) `inputs.ping` Add percentiles to the ping plugin
- [#8369](https://github.com/influxdata/telegraf/pull/8369) `inputs.sqlserver` Added tags for monitoring readable secondaries for Azure SQL MI
- [#8379](https://github.com/influxdata/telegraf/pull/8379) `inputs.sqlserver` SQL Server HA/DR Availability Group queries
- [#8520](https://github.com/influxdata/telegraf/pull/8520) Add initialization example to mock-plugin.
- [#8426](https://github.com/influxdata/telegraf/pull/8426) `inputs.snmp` Add support to convert snmp hex strings to integers
- [#8509](https://github.com/influxdata/telegraf/pull/8509) `inputs.statsd` Add configurable Max TTL duration for statsd input plugin entries
- [#8508](https://github.com/influxdata/telegraf/pull/8508) `inputs.bind` Add configurable timeout to bind input plugin http call
- [#8368](https://github.com/influxdata/telegraf/pull/8368) `inputs.sqlserver` Added is_primary_replica for monitoring readable secondaries for Azure SQL DB
- [#8462](https://github.com/influxdata/telegraf/pull/8462) `inputs.sqlserver` sqlAzureMIRequests - remove duplicate column [session_db_name]
- [#8464](https://github.com/influxdata/telegraf/pull/8464) `inputs.sqlserver` Add column measurement_db_type to output of all queries if not empty
- [#8389](https://github.com/influxdata/telegraf/pull/8389) `inputs.opcua` Add node groups to opcua input plugin
- [#8432](https://github.com/influxdata/telegraf/pull/8432) add support for linux/ppc64le
- [#8474](https://github.com/influxdata/telegraf/pull/8474) `inputs.modbus` Add FLOAT64-IEEE support to inputs.modbus (#8361) (by @Nemecsek)
- [#8447](https://github.com/influxdata/telegraf/pull/8447) `processors.starlark` Add the shared state to the global scope to get previous data
- [#8383](https://github.com/influxdata/telegraf/pull/8383) `inputs.zfs` Add dataset metrics to zfs input
- [#8429](https://github.com/influxdata/telegraf/pull/8429) `outputs.nats` Added "name" parameter to NATS output plugin
- [#8477](https://github.com/influxdata/telegraf/pull/8477) `inputs.http` proxy support for http input
- [#8466](https://github.com/influxdata/telegraf/pull/8466) `inputs.snmp` Translate snmp field values
- [#8435](https://github.com/influxdata/telegraf/pull/8435) `common.kafka` Enable kafka zstd compression and idempotent writes
- [#8056](https://github.com/influxdata/telegraf/pull/8056) `inputs.monit` Add response_time to monit plugin
- [#8446](https://github.com/influxdata/telegraf/pull/8446) update to go 1.15.5
- [#8428](https://github.com/influxdata/telegraf/pull/8428) `aggregators.basicstats` Add rate and interval to the basicstats aggregator plugin
- [#8575](https://github.com/influxdata/telegraf/pull/8575) `inputs.win_services` Added Glob pattern matching for "Windows Services" plugin
- [#6132](https://github.com/influxdata/telegraf/pull/6132) `inputs.mysql` Add per user metrics to mysql input
- [#8500](https://github.com/influxdata/telegraf/pull/8500) `inputs.github` [inputs.github] Add query of pull-request statistics
- [#8598](https://github.com/influxdata/telegraf/pull/8598) `processors.enum` Allow globs (wildcards) in config for tags/fields in enum processor
- [#8590](https://github.com/influxdata/telegraf/pull/8590) `inputs.ethtool` [ethtool] interface_up field added
- [#8579](https://github.com/influxdata/telegraf/pull/8579) `parsers.json` Add wildcard tags json parser support

### New Parser Plugins

- [#7778](https://github.com/influxdata/telegraf/pull/7778) `parsers.prometheus` Add a parser plugin for prometheus

### New Serializer Plugins

- [#8360](https://github.com/influxdata/telegraf/pull/8360) `serializers.prometheusremotewrite` Add prometheus remote write serializer

### New Input Plugins

- [#8163](https://github.com/influxdata/telegraf/pull/8163) `inputs.riemann` Support Riemann-Protobuff Listener
- [#8488](https://github.com/influxdata/telegraf/pull/8488) `inputs.intel_powerstat` New Intel PowerStat input plugin

### New Output Plugins

- [#8296](https://github.com/influxdata/telegraf/pull/8296) `outputs.yandex_cloud_monitoring` #8295 Initial Yandex.Cloud monitoring
- [#8202](https://github.com/influxdata/telegraf/pull/8202) `outputs.logzio` A new Logz.io output plugin

## v1.16.3 [2020-12-01]

### Bugfixes

- [#8483](https://github.com/influxdata/telegraf/pull/8483) `inputs.gnmi` Log SubscribeResponse_Error message and code. #8482
- [#7987](https://github.com/influxdata/telegraf/pull/7987) update godirwalk to v1.16.1
- [#8438](https://github.com/influxdata/telegraf/pull/8438) `processors.starlark` Starlark example dropbytype
- [#8468](https://github.com/influxdata/telegraf/pull/8468) `inputs.sqlserver` Fix typo in column name
- [#8461](https://github.com/influxdata/telegraf/pull/8461) `inputs.phpfpm` [php-fpm] Fix possible "index out of range"
- [#8444](https://github.com/influxdata/telegraf/pull/8444) `inputs.apcupsd` Update mdlayher/apcupsd dependency
- [#8439](https://github.com/influxdata/telegraf/pull/8439) `processors.starlark` Show how to return a custom error with the Starlark processor
- [#8440](https://github.com/influxdata/telegraf/pull/8440) `parsers.csv` keep field name as is for csv timestamp column
- [#8436](https://github.com/influxdata/telegraf/pull/8436) `inputs.nvidia_smi` Add DriverVersion and CUDA Version to output
- [#8423](https://github.com/influxdata/telegraf/pull/8423) `processors.starlark` Show how to return several metrics with the Starlark processor
- [#8408](https://github.com/influxdata/telegraf/pull/8408) `processors.starlark` Support logging in starlark
- [#8315](https://github.com/influxdata/telegraf/pull/8315) add kinesis output to external plugins list
- [#8406](https://github.com/influxdata/telegraf/pull/8406) `outputs.wavefront` #8405 add non-retryable debug logging
- [#8404](https://github.com/influxdata/telegraf/pull/8404) `outputs.wavefront` Wavefront output should distinguish between retryable and non-retryable errors
- [#8401](https://github.com/influxdata/telegraf/pull/8401) `processors.starlark` Allow to catch errors that occur in the apply function

## v1.16.2 [2020-11-13]

### Bugfixes

- [#8400](https://github.com/influxdata/telegraf/pull/8400) `parsers.csv` Fix parsing of multiple files with different headers (#6318).
- [#8326](https://github.com/influxdata/telegraf/pull/8326) `inputs.proxmox` proxmox: ignore QEMU templates and iron out a few bugs
- [#7991](https://github.com/influxdata/telegraf/pull/7991) `inputs.systemd_units` systemd_units: add --plain to command invocation (#7990)
- [#8307](https://github.com/influxdata/telegraf/pull/8307) fix links in external plugins readme
- [#8370](https://github.com/influxdata/telegraf/pull/8370) `inputs.redis` Fix minor typos in readmes
- [#8374](https://github.com/influxdata/telegraf/pull/8374) `inputs.smart` Fix SMART plugin to recognize all devices from config
- [#8288](https://github.com/influxdata/telegraf/pull/8288) `inputs.redfish` Add OData-Version header to requests
- [#8357](https://github.com/influxdata/telegraf/pull/8357) `inputs.vsphere` Prydin issue 8169
- [#8356](https://github.com/influxdata/telegraf/pull/8356) `inputs.sqlserver` On-prem fix for #8324
- [#8165](https://github.com/influxdata/telegraf/pull/8165) `outputs.wavefront` [output.wavefront] Introduced "immediate_flush" flag
- [#7938](https://github.com/influxdata/telegraf/pull/7938) `inputs.gnmi` added support for bytes encoding
- [#8337](https://github.com/influxdata/telegraf/pull/8337) `inputs.dcos` Update jwt-go module to address CVE-2020-26160
- [#8350](https://github.com/influxdata/telegraf/pull/8350) `inputs.ras` fix plugins/input/ras test
- [#8329](https://github.com/influxdata/telegraf/pull/8329) `outputs.dynatrace` #8328 Fixed a bug with the state map in Dynatrace Plugin

## v1.16.1 [2020-10-28]

### Release Notes

- [#8318](https://github.com/influxdata/telegraf/pull/8318) `common.kafka` kafka sasl-mechanism auth support for SCRAM-SHA-256, SCRAM-SHA-512, GSSAPI

### Bugfixes

- [#8331](https://github.com/influxdata/telegraf/pull/8331) `inputs.sqlserver` SQL Server Azure PerfCounters Fix
- [#8325](https://github.com/influxdata/telegraf/pull/8325) `inputs.sqlserver` SQL Server - PerformanceCounters - removed synthetic counters
- [#8324](https://github.com/influxdata/telegraf/pull/8324) `inputs.sqlserver` SQL Server - server_properties added sql_version_desc
- [#8317](https://github.com/influxdata/telegraf/pull/8317) `inputs.ras` Disable RAS input plugin on specific Linux architectures: mips64, mips64le, ppc64le, riscv64
- [#8309](https://github.com/influxdata/telegraf/pull/8309) `inputs.processes` processes: fix issue with stat no such file/dir
- [#8308](https://github.com/influxdata/telegraf/pull/8308) `inputs.win_perf_counters` fix issue with PDH_CALC_NEGATIVE_DENOMINATOR error
- [#8306](https://github.com/influxdata/telegraf/pull/8306) `inputs.ras` RAS plugin - fix for too many open files handlers

## v1.16.0 [2020-10-21]

### Release Notes

- New [code examples](/plugins/processors/starlark/testdata) for the [Starlark processor](/plugins/processors/starlark/README.md)
- [#7920](https://github.com/influxdata/telegraf/pull/7920) `inputs.rabbitmq` remove deprecated healthcheck
- [#7953](https://github.com/influxdata/telegraf/pull/7953) Add details to connect to InfluxDB OSS 2 and Cloud 2
- [#8054](https://github.com/influxdata/telegraf/pull/8054) add guidelines run to external plugins with execd
- [#8198](https://github.com/influxdata/telegraf/pull/8198) `inputs.influxdb_v2_listener` change default influxdb port from 9999 to 8086 to match OSS 2.0 release
- [starlark](https://github.com/influxdata/telegraf/tree/release-1.16/plugins/processors/starlark/testdata) `processors.starlark` add various code examples for the Starlark processor

### Features

- [#7814](https://github.com/influxdata/telegraf/pull/7814) `agent` Send metrics in FIFO order
- [#7869](https://github.com/influxdata/telegraf/pull/7869) `inputs.modbus` extend support of fixed point values on input
- [#7870](https://github.com/influxdata/telegraf/pull/7870) `inputs.mongodb` Added new metric "pages written from cache"
- [#7875](https://github.com/influxdata/telegraf/pull/7875) `inputs.consul` input consul - added metric_version flag
- [#7894](https://github.com/influxdata/telegraf/pull/7894) `inputs.cloudwatch` Implement AWS CloudWatch Input Plugin ListMetrics API calls to use Active Metric Filter
- [#7904](https://github.com/influxdata/telegraf/pull/7904) `inputs.clickhouse` add additional metrics to clickhouse input plugin
- [#7934](https://github.com/influxdata/telegraf/pull/7934) `inputs.sqlserver` Database_type config to Split up sql queries by engine type
- [#8018](https://github.com/influxdata/telegraf/pull/8018) `processors.ifname` Add addTag debugging in ifname plugin
- [#8019](https://github.com/influxdata/telegraf/pull/8019) `outputs.elasticsearch` added force_document_id option to ES output enable resend data and avoiding duplicated ES documents
- [#8025](https://github.com/influxdata/telegraf/pull/8025) `inputs.aerospike` Add set, and histogram reporting to aerospike telegraf plugin
- [#8082](https://github.com/influxdata/telegraf/pull/8082) `inputs.snmp` Add agent host tag configuration option
- [#8113](https://github.com/influxdata/telegraf/pull/8113) `inputs.smart` Add more missing NVMe attributes to smart plugin
- [#8120](https://github.com/influxdata/telegraf/pull/8120) `inputs.sqlserver` Added more performance counters to SqlServer input plugin
- [#8127](https://github.com/influxdata/telegraf/pull/8127) `agent` Sort plugin name lists for output
- [#8132](https://github.com/influxdata/telegraf/pull/8132) `outputs.sumologic` Sumo Logic output plugin: carbon2 default to include field in metric
- [#8133](https://github.com/influxdata/telegraf/pull/8133) `inputs.influxdb_v2_listener` influxdb_v2_listener - add /ready route
- [#8168](https://github.com/influxdata/telegraf/pull/8168) `processors.starlark` add json parsing support to starlark
- [#8186](https://github.com/influxdata/telegraf/pull/8186) `inputs.sqlserver` New sql server queries (Azure)
- [#8189](https://github.com/influxdata/telegraf/pull/8189) `inputs.snmp_trap` If the community string is available, add it as a tag
- [#8190](https://github.com/influxdata/telegraf/pull/8190) `inputs.tail` Semigroupoid multiline (#8167)
- [#8196](https://github.com/influxdata/telegraf/pull/8196) `inputs.redis` add functionality to get values from redis commands
- [#8220](https://github.com/influxdata/telegraf/pull/8220) `build` update to Go 1.15
- [#8032](https://github.com/influxdata/telegraf/pull/8032) `inputs.http_response` http_response: match on status code
- [#8172](https://github.com/influxdata/telegraf/pull/8172) `inputs.sqlserver` New sql server queries (on-prem) - refactoring and formatting

### Bugfixes

- [#7816](https://github.com/influxdata/telegraf/pull/7816) `shim` fix bug with loading plugins in shim with no config
- [#7818](https://github.com/influxdata/telegraf/pull/7818) `build` Fix darwin package build flags
- [#7819](https://github.com/influxdata/telegraf/pull/7819) `inputs.tail` Close file to ensure it has been flushed
- [#7853](https://github.com/influxdata/telegraf/pull/7853) Initialize aggregation processors
- [#7865](https://github.com/influxdata/telegraf/pull/7865) `common.shim` shim logger improvements
- [#7867](https://github.com/influxdata/telegraf/pull/7867) `inputs.execd` fix issue with execd restart_delay being ignored
- [#7872](https://github.com/influxdata/telegraf/pull/7872) `inputs.gnmi` Recv next message after send returns EOF
- [#7877](https://github.com/influxdata/telegraf/pull/7877) Fix arch name in deb/rpm builds
- [#7909](https://github.com/influxdata/telegraf/pull/7909) fixes issue with rpm /var/log/telegraf permissions
- [#7918](https://github.com/influxdata/telegraf/pull/7918) `inputs.net` fix broken link to proc.c
- [#7927](https://github.com/influxdata/telegraf/pull/7927) `inputs.tail` Fix tail following on EOF
- [#8005](https://github.com/influxdata/telegraf/pull/8005) Fix docker-image make target
- [#8039](https://github.com/influxdata/telegraf/pull/8039) `serializers.splunkmetric` Remove Event field as it is causing issues with pre-trained source types
- [#8048](https://github.com/influxdata/telegraf/pull/8048) `inputs.jenkins` Multiple escaping occurs on Jenkins URLs at certain folder depth
- [#8071](https://github.com/influxdata/telegraf/pull/8071) `inputs.kubernetes` add missing error check for HTTP req failure
- [#8145](https://github.com/influxdata/telegraf/pull/8145) `processors.execd` Increased the maximum serialized metric size in line protocol
- [#8159](https://github.com/influxdata/telegraf/pull/8159) `outputs.dynatrace` Dynatrace Output: change handling of monotonic counters
- [#8176](https://github.com/influxdata/telegraf/pull/8176) fix panic on streaming processers using logging
- [#8177](https://github.com/influxdata/telegraf/pull/8177) `parsers.influx` fix: plugins/parsers/influx: avoid ParseError.Error panic
- [#8199](https://github.com/influxdata/telegraf/pull/8199) `inputs.docker` Fix vulnerabilities found in BDBA scan
- [#8200](https://github.com/influxdata/telegraf/pull/8200) `inputs.sqlserver` Fixed Query mapping
- [#8201](https://github.com/influxdata/telegraf/pull/8201) `outputs.sumologic` Fix carbon2 serializer not falling through to field separate when carbon2_format field is unset
- [#8210](https://github.com/influxdata/telegraf/pull/8210) update gopsutil: fix procstat performance regression
- [#8162](https://github.com/influxdata/telegraf/pull/8162) Fix bool serialization when using carbon2
- [#8240](https://github.com/influxdata/telegraf/pull/8240) Fix bugs found by LGTM analysis platform
- [#8251](https://github.com/influxdata/telegraf/pull/8251) `outputs.dynatrace` Dynatrace Output Plugin: Fixed behaviour when state map is cleared
- [#8274](https://github.com/influxdata/telegraf/pull/8274) `common.shim` fix issue with loading processor config from execd

### New Input Plugins

- [influxdb_v2_listener](/plugins/inputs/influxdb_v2_listener/README.md) Influxdb v2 listener - Contributed by @magichair
- [intel_rdt](/plugins/inputs/intel_rdt/README.md) New input plugin for Intel RDT (Intel Resource Director Technology) - Contributed by @p-zak
- [nsd](/plugins/inputs/nsd/README.md) add nsd input plugin - Contributed by @gearnode
- [opcua](/plugins/inputs/opcua/README.md) Add OPC UA input plugin - Contributed by InfluxData
- [proxmox](/plugins/inputs/proxmox/README.md) Proxmox plugin - Contributed by @effitient
- [ras](/plugins/inputs/ras/README.md) New input plugin for RAS (Reliability, Availability and Serviceability) - Contributed by @p-zak
- [win_eventlog](/plugins/inputs/win_eventlog/README.md) Windows eventlog input plugin - Contributed by @simnv

### New Output Plugins

- [dynatrace](/plugins/outputs/dynatrace/README.md) Dynatrace output plugin - Contributed by @thschue
- [sumologic](/plugins/outputs/sumologic/README.md) Sumo Logic output plugin - Contributed by @pmalek-sumo
- [timestream](/plugins/outputs/timestream) Timestream Output Plugin - Contributed by @piotrwest

### New External Plugins

See [EXTERNAL_PLUGINS.md](/EXTERNAL_PLUGINS.md) for a full list of external plugins

- [awsalarms](https://github.com/vipinvkmenon/awsalarms) - Simple plugin to gather/monitor alarms generated  in AWS.
- [youtube-telegraf-plugin](https://github.com/inabagumi/youtube-telegraf-plugin) - Gather view and subscriber stats from your youtube videos
- [octoprint](https://github.com/BattleBas/octoprint-telegraf-plugin) - Gather 3d print information from the octoprint API.
- [systemd-timings](https://github.com/pdmorrow/telegraf-execd-systemd-timings) - Gather systemd boot and unit timestamp metrics.

## v1.15.4 [2020-10-20]

### Bugfixes

- [#8274](https://github.com/influxdata/telegraf/pull/8274) `common.shim` fix issue with loading processor config from execd
- [#8176](https://github.com/influxdata/telegraf/pull/8176) `agent` fix panic on streaming processers using logging

## v1.15.3 [2020-09-11]

### Release Notes

- Many documentation updates
- New [code examples](https://github.com/influxdata/telegraf/tree/master/plugins/processors/starlark/testdata) for the [Starlark processor](https://github.com/influxdata/telegraf/blob/master/plugins/processors/starlark/README.md)

### Bugfixes

- [#7999](https://github.com/influxdata/telegraf/pull/7999) `agent` fix minor agent error message race condition
- [#8051](https://github.com/influxdata/telegraf/pull/8051) `build` fix docker build. update dockerfiles to Go 1.14
- [#8052](https://github.com/influxdata/telegraf/pull/8052) `shim` fix bug in shim logger affecting AddError
- [#7996](https://github.com/influxdata/telegraf/pull/7996) `shim` fix issue with shim use of config.Duration
- [#8006](https://github.com/influxdata/telegraf/pull/8006) `inputs.eventhub_consumer` Fix string to int conversion in eventhub consumer
- [#7986](https://github.com/influxdata/telegraf/pull/7986) `inputs.http_listener_v2` make http header tags case insensitive
- [#7869](https://github.com/influxdata/telegraf/pull/7869) `inputs.modbus` extend support of fixed point values on input
- [#7861](https://github.com/influxdata/telegraf/pull/7861) `inputs.ping` Fix Ping Input plugin for FreeBSD's ping6
- [#7808](https://github.com/influxdata/telegraf/pull/7808) `inputs.sqlserver` added new counter - Lock Timeouts (timeout > 0)/sec
- [#8026](https://github.com/influxdata/telegraf/pull/8026) `inputs.vsphere` vSphere Fixed missing clustername issue 7878
- [#8020](https://github.com/influxdata/telegraf/pull/8020) `processors.starlark` improve the quality of starlark docs by executing them as tests
- [#7976](https://github.com/influxdata/telegraf/pull/7976) `processors.starlark` add pivot example for starlark processor
- [#7134](https://github.com/influxdata/telegraf/pull/7134) `outputs.application_insights` Added the ability to set the endpoint url
- [#7908](https://github.com/influxdata/telegraf/pull/7908) `outputs.opentsdb` fix JSON handling of values NaN and Inf

## v1.15.2 [2020-07-31]

### Bug Fixes

- [#7905](https://github.com/influxdata/telegraf/issues/7905): Fix RPM /var/log/telegraf permissions
- [#7880](https://github.com/influxdata/telegraf/issues/7880): Fix tail following on EOF

## v1.15.1 [2020-07-22]

### Bug Fixes

- [#7877](https://github.com/influxdata/telegraf/pull/7877): Fix architecture in non-amd64 deb and rpm packages.

## v1.15.0 [2020-07-22]

### Release Notes

- The `logparser` input is deprecated, use the `tail` input with `data_format =
  "grok"` as a replacement.

- The `cisco_telemetry_gnmi` input has been renamed to `gnmi` to better reflect
  its general support for gNMI devices.

- Several fields used primarily for debugging have been removed from the
  `splunkmetric` serializer, if you are making use of these fields they can be
  added back with the `tag` option.

- Telegraf's `--test` mode now runs processors and aggregators before printing
  metrics.

- Official packages now built with Go 1.14.5.

- When updating the Debian package you will no longer be prompted to merge the
  telegraf.conf file, instead the new version will be installed to
  `/etc/telegraf/telegraf.conf.sample`.  The tar and zip packages now include
  the version in the top level directory.

### New Inputs

- [nginx_sts](/plugins/inputs/nginx_sts/README.md) - Contributed by @zdmytriv
- [redfish](/plugins/inputs/redfish/README.md) - Contributed by @sarvanikonda

### New Processors

- [defaults](/plugins/processors/defaults/README.md) - Contributed by @jregistr
- [execd](/plugins/processors/execd/README.md) - Contributed by @influxdata
- [filepath](/plugins/processors/filepath/README.md) - Contributed by @kir4h
- [ifname](/plugins/processors/ifname/README.md) - Contributed by @influxdata
- [port_name](/plugins/processors/port_name/README.md) - Contributed by @influxdata
- [reverse_dns](/plugins/processors/reverse_dns/README.md) - Contributed by @influxdata
- [starlark](/plugins/processors/starlark/README.md) - Contributed by @influxdata

### New Outputs

- [newrelic](/plugins/outputs/newrelic/README.md) - Contributed by @hsinghkalsi
- [execd](/plugins/outputs/execd/README.md) - Contributed by @influxdata

### Features

- [#7634](https://github.com/influxdata/telegraf/pull/7634): Add support for streaming processors.
- [#6905](https://github.com/influxdata/telegraf/pull/6905): Add commands stats to mongodb input plugin.
- [#7193](https://github.com/influxdata/telegraf/pull/7193): Add additional concurrent transaction information.
- [#7223](https://github.com/influxdata/telegraf/pull/7223): Add ability to specify HTTP Headers in http_listener_v2 which will added as tags.
- [#7140](https://github.com/influxdata/telegraf/pull/7140): Apply ping deadline to dns lookup.
- [#7225](https://github.com/influxdata/telegraf/pull/7225): Add support for 64-bit integer types to modbus input.
- [#7231](https://github.com/influxdata/telegraf/pull/7231): Add possibility to specify measurement per register.
- [#7136](https://github.com/influxdata/telegraf/pull/7136): Support multiple templates for graphite serializers.
- [#7250](https://github.com/influxdata/telegraf/pull/7250): Deploy telegraf configuration as a "non config" file.
- [#7214](https://github.com/influxdata/telegraf/pull/7214): Add VolumeSpace query for sqlserver input with metric_version 2.
- [#7304](https://github.com/influxdata/telegraf/pull/7304): Add reading bearer token from a file to http input.
- [#7366](https://github.com/influxdata/telegraf/pull/7366): add support for SIGUSR1 to trigger flush.
- [#7271](https://github.com/influxdata/telegraf/pull/7271): Add retry when slave is busy to modbus input.
- [#7356](https://github.com/influxdata/telegraf/pull/7356): Add option to save retention policy as tag in influxdb_listener.
- [#6915](https://github.com/influxdata/telegraf/pull/6915): Add support for MDS and RGW sockets to ceph input.
- [#7391](https://github.com/influxdata/telegraf/pull/7391): Extract target as a tag for each rule in iptables input.
- [#7434](https://github.com/influxdata/telegraf/pull/7434): Use docker log timestamp as metric time.
- [#7359](https://github.com/influxdata/telegraf/pull/7359): Add cpu query to sqlserver input.
- [#7464](https://github.com/influxdata/telegraf/pull/7464): Add field creation to date processor and integer unix time support.
- [#7483](https://github.com/influxdata/telegraf/pull/7483): Add integer mapping support to enum processor.
- [#7321](https://github.com/influxdata/telegraf/pull/7321): Add additional fields to mongodb input.
- [#7491](https://github.com/influxdata/telegraf/pull/7491): Add authentication support to the http_response input plugin.
- [#7503](https://github.com/influxdata/telegraf/pull/7503): Add truncate_tags setting to wavefront output.
- [#7545](https://github.com/influxdata/telegraf/pull/7545): Add configurable separator graphite serializer and output.
- [#7489](https://github.com/influxdata/telegraf/pull/7489): Add cluster state integer to mongodb input.
- [#7515](https://github.com/influxdata/telegraf/pull/7515): Add option to disable mongodb cluster status.
- [#7319](https://github.com/influxdata/telegraf/pull/7319): Add support for battery level monitoring to the fibaro input.
- [#7405](https://github.com/influxdata/telegraf/pull/7405): Allow collection of HTTP Headers in http_response input.
- [#7540](https://github.com/influxdata/telegraf/pull/7540): Add processor to look up service name by port.
- [#7474](https://github.com/influxdata/telegraf/pull/7474): Add new once mode that write to outputs and exits.
- [#7474](https://github.com/influxdata/telegraf/pull/7474): Run processors and aggregators during test mode.
- [#7294](https://github.com/influxdata/telegraf/pull/7294): Add SNMPv3 trap support to snmp_trap input.
- [#7646](https://github.com/influxdata/telegraf/pull/7646): Add video codec stats to nvidia-smi.
- [#7651](https://github.com/influxdata/telegraf/pull/7651): Fix source field for icinga2 plugin and add tag for server hostname.
- [#7619](https://github.com/influxdata/telegraf/pull/7619): Add timezone configuration to csv input data format.
- [#7596](https://github.com/influxdata/telegraf/pull/7596): Add ability to collect response body as field with http_response.
- [#7267](https://github.com/influxdata/telegraf/pull/7267): Add ability to add selectors as tags in kube_inventory.
- [#7712](https://github.com/influxdata/telegraf/pull/7712): Add counter type to sqlserver perfmon collector.
- [#7575](https://github.com/influxdata/telegraf/pull/7575): Add missing nvme attributes to smart plugin.
- [#7726](https://github.com/influxdata/telegraf/pull/7726): Add laundry to mem plugin on FreeBSD.
- [#7762](https://github.com/influxdata/telegraf/pull/7762): Allow per input overriding of collection_jitter and precision.
- [#7686](https://github.com/influxdata/telegraf/pull/7686): Improve performance of procstat: Up to 40/120x better performance.
- [#7677](https://github.com/influxdata/telegraf/pull/7677): Expand execd shim support for processor and outputs.
- [#7154](https://github.com/influxdata/telegraf/pull/7154): Add v3 metadata support to ecs input.
- [#7792](https://github.com/influxdata/telegraf/pull/7792): Support utf-16 in file and tail inputs.

### Bug Fixes

- [#7371](https://github.com/influxdata/telegraf/issues/7371): Fix unable to write metrics to CloudWatch with IMDSv1 disabled.
- [#7233](https://github.com/influxdata/telegraf/issues/7233): Fix vSphere 6.7 missing data issue.
- [#7448](https://github.com/influxdata/telegraf/issues/7448): Remove debug fields from splunkmetric serializer.
- [#7446](https://github.com/influxdata/telegraf/issues/7446): Fix gzip support in socket_listener with tcp sockets.
- [#7390](https://github.com/influxdata/telegraf/issues/7390): Fix interval drift when round_interval is set in agent.
- [#7524](https://github.com/influxdata/telegraf/pull/7524): Fix typo in total_elapsed_time_ms field of sqlserver input.
- [#7203](https://github.com/influxdata/telegraf/issues/7203): Exclude csv_timestamp_column and csv_measurement_column from fields.
- [#7018](https://github.com/influxdata/telegraf/issues/7018): Fix incorrect uptime when clock is adjusted.
- [#6807](https://github.com/influxdata/telegraf/issues/6807): Fix memory leak when using procstat on Windows.
- [#7495](https://github.com/influxdata/telegraf/issues/7495): Improve sqlserver input compatibility with older server versions.
- [#7558](https://github.com/influxdata/telegraf/issues/7558): Remove trailing backslash from tag keys/values in influx serializer.
- [#7715](https://github.com/influxdata/telegraf/issues/7715): Fix incorrect Azure SQL DB server properties.
- [#7431](https://github.com/influxdata/telegraf/issues/7431): Fix json unmarshal error in the kibana input.
- [#5633](https://github.com/influxdata/telegraf/issues/5633): Send metrics in FIFO order.

## v1.14.5 [2020-06-30]

### Bug Fixes

- [#7686](https://github.com/influxdata/telegraf/pull/7686): Improve the performance of the procstat input.
- [#7658](https://github.com/influxdata/telegraf/pull/7658): Fix ping exit code handling on non-Linux.
- [#7718](https://github.com/influxdata/telegraf/pull/7718): Skip overs errors in the output of the sensors command.
- [#7748](https://github.com/influxdata/telegraf/issues/7748): Prevent startup when tags have incorrect type in configuration file.
- [#7699](https://github.com/influxdata/telegraf/issues/7699): Fix panic with GJSON multiselect query in json parser.
- [#7754](https://github.com/influxdata/telegraf/issues/7754): Allow any key usage type on x509 certificate.
- [#7705](https://github.com/influxdata/telegraf/issues/7705): Allow histograms and summary types without buckets or quantiles in prometheus_client output.

## v1.14.4 [2020-06-09]

### Bug Fixes

- [#7325](https://github.com/influxdata/telegraf/issues/7325): Fix "cannot insert the value NULL error" with PerformanceCounters query.
- [#7579](https://github.com/influxdata/telegraf/pull/7579): Fix numeric to bool conversion in converter processor.
- [#7551](https://github.com/influxdata/telegraf/issues/7551): Fix typo in name of gc_cpu_fraction field of the influxdb input.
- [#7617](https://github.com/influxdata/telegraf/issues/7617): Fix issue with influx stream parser blocking when data is in buffer.

## v1.14.3 [2020-05-19]

### Bug Fixes

- [#7412](https://github.com/influxdata/telegraf/pull/7412): Use same timestamp for all objects in arrays in the json parser.
- [#7343](https://github.com/influxdata/telegraf/issues/7343): Handle multiple metrics with the same timestamp in dedup processor.
- [#5905](https://github.com/influxdata/telegraf/issues/5905): Fix reconnection of timed out HTTP2 connections influxdb outputs.
- [#7468](https://github.com/influxdata/telegraf/issues/7468): Fix negative value parsing in impi_sensor input.

## v1.14.2 [2020-04-28]

### Bug Fixes

- [#7241](https://github.com/influxdata/telegraf/issues/7241): Trim whitespace from instance tag in sqlserver input.
- [#7322](https://github.com/influxdata/telegraf/issues/7322): Use increased AWS Cloudwatch GetMetricData limit of 500 metrics per call.
- [#7318](https://github.com/influxdata/telegraf/issues/7318): Fix dimension limit on azure_monitor output.
- [#7407](https://github.com/influxdata/telegraf/pull/7407): Fix 64-bit integer to string conversion in snmp input.
- [#7327](https://github.com/influxdata/telegraf/issues/7327): Fix shard indices reporting in elasticsearch input.
- [#7388](https://github.com/influxdata/telegraf/issues/7388): Ignore fields with NaN or Inf floats in the JSON serializer.
- [#7402](https://github.com/influxdata/telegraf/issues/7402): Fix typo in name of gc_cpu_fraction field of the kapacitor input.
- [#7235](https://github.com/influxdata/telegraf/issues/7235): Don't retry `create database` when using database_tag if forbidden by the server in influxdb output.
- [#7406](https://github.com/influxdata/telegraf/issues/7406): Allow CR and FF inside of string fields in influx parser.

## v1.14.1 [2020-04-14]

### Bug Fixes

- [#7236](https://github.com/influxdata/telegraf/issues/7236): Fix PerformanceCounter query performance degradation in sqlserver input.
- [#7257](https://github.com/influxdata/telegraf/issues/7257): Fix error when using the Name field in template processor.
- [#7289](https://github.com/influxdata/telegraf/pull/7289): Fix export timestamp not working for prometheus on v2.
- [#7310](https://github.com/influxdata/telegraf/issues/7310): Fix exclude database and retention policy tags is shared.
- [#7262](https://github.com/influxdata/telegraf/issues/7262): Fix status path when using globs in phpfpm.

## v1.14 [2020-03-26]

### Release Notes

- In the `sqlserver` input, the `sqlserver_azurestats` measurement has been
  renamed to `sqlserver_azure_db_resource_stats` due to an issue where numeric
  metrics were previously being reported incorrectly as strings.

- The `date` processor now uses the UTC timezone when creating its tag.  In
  previous versions the local time was used.

### New Inputs

- [clickhouse](/plugins/inputs/clickhouse/README.md) - Contributed by @kshvakov
- [execd](/plugins/inputs/execd/README.md) - Contributed by @jgraichen
- [eventhub_consumer](/plugins/inputs/eventhub_consumer/README.md) - Contributed by @R290
- [infiniband](/plugins/inputs/infiniband/README.md) - Contributed by @willfurnell
- [lanz](/plugins/inputs/lanz/README.md): Contributed by @timhughes
- [modbus](/plugins/inputs/modbus/README.md) - Contributed by @garciaolais
- [monit](/plugins/inputs/monit/README.md) - Contributed by @SirishaGopigiri
- [sflow](/plugins/inputs/sflow/README.md) - Contributed by @influxdata
- [wireguard](/plugins/inputs/wireguard/README.md) - Contributed by @LINKIWI

### New Processors

- [dedup](/plugins/processors/dedup/README.md) - Contributed by @igomura
- [template](/plugins/processors/template/README.md) - Contributed by @RobMalvern
- [s2geo](/plugins/processors/s2geo/README.md) - Contributed by @alespour

### New Outputs

- [warp10](/plugins/outputs/warp10/README.md) - Contributed by @aurrelhebert

### Features

- [#6730](https://github.com/influxdata/telegraf/pull/6730): Add page_faults for mongodb wired tiger.
- [#6798](https://github.com/influxdata/telegraf/pull/6798): Add use_sudo option to ipmi_sensor input.
- [#6764](https://github.com/influxdata/telegraf/pull/6764): Add ability to collect pod labels to kubernetes input.
- [#6770](https://github.com/influxdata/telegraf/pull/6770): Expose unbound-control config file option.
- [#6508](https://github.com/influxdata/telegraf/pull/6508): Add support for new nginx plus api endpoints.
- [#6342](https://github.com/influxdata/telegraf/pull/6342): Add kafka SASL version control to support Azure Event Hub.
- [#6869](https://github.com/influxdata/telegraf/pull/6869): Add RBPEX IO statistics to DatabaseIO query in sqlserver input.
- [#6869](https://github.com/influxdata/telegraf/pull/6869): Add space on disk for each file to DatabaseIO query in the sqlserver input.
- [#6869](https://github.com/influxdata/telegraf/pull/6869): Calculate DB Name instead of GUID in physical_db_name in the sqlserver input.
- [#6733](https://github.com/influxdata/telegraf/pull/6733): Add latency stats to mongo input.
- [#6844](https://github.com/influxdata/telegraf/pull/6844): Add source and port tags to jenkins_job metrics.
- [#6886](https://github.com/influxdata/telegraf/pull/6886): Add date offset and timezone options to date processor.
- [#6859](https://github.com/influxdata/telegraf/pull/6859): Exclude resources by inventory path in vsphere input.
- [#6700](https://github.com/influxdata/telegraf/pull/6700): Allow a user defined field to be used as the graylog short_message.
- [#6917](https://github.com/influxdata/telegraf/pull/6917): Add server_name override for x509_cert plugin.
- [#6921](https://github.com/influxdata/telegraf/pull/6921): Add udp internal metrics for the statsd input.
- [#6914](https://github.com/influxdata/telegraf/pull/6914): Add replica set tag to mongodb input.
- [#6935](https://github.com/influxdata/telegraf/pull/6935): Add counters for merged reads and writes to diskio input.
- [#6982](https://github.com/influxdata/telegraf/pull/6982): Add support for titlecase transformation to strings processor.
- [#6993](https://github.com/influxdata/telegraf/pull/6993): Add support for MDB database information to openldap input.
- [#6957](https://github.com/influxdata/telegraf/pull/6957): Add new fields for Jenkins total and busy executors.
- [#7035](https://github.com/influxdata/telegraf/pull/7035): Fix dash to underscore replacement when handling embedded tags in Cisco MDT.
- [#7039](https://github.com/influxdata/telegraf/pull/7039): Add process created_at time to procstat input.
- [#7022](https://github.com/influxdata/telegraf/pull/7022): Add support for credentials file to nats_consumer and nats output.
- [#7065](https://github.com/influxdata/telegraf/pull/7065): Add additional tags and fields to apcupsd.
- [#7084](https://github.com/influxdata/telegraf/pull/7084): Add RabbitMQ slave_nodes and synchronized_slave_nodes metrics.
- [#7089](https://github.com/influxdata/telegraf/pull/7089): Allow globs in FPM unix socket paths.
- [#7071](https://github.com/influxdata/telegraf/pull/7071): Add non-cumulative histogram to histogram aggregator.
- [#6969](https://github.com/influxdata/telegraf/pull/6969): Add label and field selectors to prometheus input k8s discovery.
- [#7049](https://github.com/influxdata/telegraf/pull/7049): Add support for converting tag or field to measurement in converter processor.
- [#7103](https://github.com/influxdata/telegraf/pull/7103): Add volume_mount_point to DatabaseIO query in sqlserver input.
- [#7142](https://github.com/influxdata/telegraf/pull/7142): Add topic tag options to kafka output.
- [#7141](https://github.com/influxdata/telegraf/pull/7141): Add support for setting InfluxDB retention policy using tag.
- [#7163](https://github.com/influxdata/telegraf/pull/7163): Add Database IO Tempdb per Azure DB to sqlserver input.
- [#7150](https://github.com/influxdata/telegraf/pull/7150): Add option for explicitly including queries in sqlserver input.
- [#7173](https://github.com/influxdata/telegraf/pull/7173): Add support for GNMI DecimalVal type to cisco_telemetry_gnmi.

### Bug Fixes

- [#6397](https://github.com/influxdata/telegraf/issues/6397): Fix conversion to floats in AzureDBResourceStats query in the sqlserver input.
- [#6867](https://github.com/influxdata/telegraf/issues/6867): Fix case sensitive collation in sqlserver input.
- [#7005](https://github.com/influxdata/telegraf/pull/7005): Search for chronyc only when chrony input plugin is enabled.
- [#2280](https://github.com/influxdata/telegraf/issues/2280): Fix request to InfluxDB Listener failing with EOF.
- [#6124](https://github.com/influxdata/telegraf/issues/6124): Fix InfluxDB listener to continue parsing after error.
- [#7133](https://github.com/influxdata/telegraf/issues/7133): Fix log rotation to use actual file size instead of bytes written.
- [#7103](https://github.com/influxdata/telegraf/pull/7103): Fix several issues with DatabaseIO query in sqlserver input.
- [#7119](https://github.com/influxdata/telegraf/pull/7119): Fix internal metrics for output split into multiple lines.
- [#7021](https://github.com/influxdata/telegraf/pull/7021): Fix schedulers query compatibility with pre SQL-2016.
- [#7182](https://github.com/influxdata/telegraf/pull/7182): Set headers on influxdb_listener ping URL.
- [#7165](https://github.com/influxdata/telegraf/issues/7165): Fix url encoding of job names in jenkins input plugin.
