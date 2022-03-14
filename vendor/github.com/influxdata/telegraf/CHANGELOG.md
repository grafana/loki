<!-- markdownlint-disable MD024 -->

# Changelog

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

- [#10288](https://github.com/influxdata/telegraf/pull/10288) Fix panic in parsers due to missing Log for all plugins using SetParserFunc
- [#10247](https://github.com/influxdata/telegraf/pull/10247) Update go-sensu module to v2.12.0
- [#10284](https://github.com/influxdata/telegraf/pull/10284) `inputs.openstack` Fix typo in openstack neutron input plugin (newtron)

### Features

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
- [#10203](https://github.com/influxdata/telegraf/pull/10203) Revert unintented corruption of the Makefile
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
- [#9662](https://github.com/influxdata/telegraf/pull/9662) `inputs.prometheus` Decode Prometheus scrape path from Kuberentes labels
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
- [Big Blue Button](https://github.com/SLedunois/bigbluebutton-telegraf-plugin) - contributed by @SLedunois

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
- [#9043](https://github.com/influxdata/telegraf/pull/9043) `processors.ifname` Get interface name more effeciently
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
- [#8609](https://github.com/influxdata/telegraf/pull/8609) `inputs.webhooks` Use the 'measurement' json field from the particle webhook as the measurment name, or if it's blank, use the 'name' field of the event's json.
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
- [starlark](https://github.com/influxdata/telegraf/tree/release-1.16/plugins/processors/starlark/testdata) `processors.starlark` add various code exampels for the Starlark processor

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

## v1.13.4 [2020-02-25]

### Release Notes

- Official packages now built with Go 1.13.8.

### Bug Fixes

- [#6988](https://github.com/influxdata/telegraf/issues/6988): Parse NaN values from summary types in prometheus input.
- [#6820](https://github.com/influxdata/telegraf/issues/6820): Fix pgbouncer input when used with newer pgbouncer versions.
- [#6913](https://github.com/influxdata/telegraf/issues/6913): Support up to 8192 stats in the ethtool input.
- [#7060](https://github.com/influxdata/telegraf/issues/7060): Fix perf counters collection on named instances in sqlserver input.
- [#6926](https://github.com/influxdata/telegraf/issues/6926): Use add time for prometheus expiration calculation.
- [#7057](https://github.com/influxdata/telegraf/issues/7057): Fix inconsistency with input error counting in internal input.
- [#7063](https://github.com/influxdata/telegraf/pull/7063): Use the same timestamp per call if no time is provided in prometheus input.

## v1.13.3 [2020-02-04]

### Bug Fixes

- [#5744](https://github.com/influxdata/telegraf/issues/5744): Fix kibana input with Kibana versions greater than 6.4.
- [#6960](https://github.com/influxdata/telegraf/issues/6960): Fix duplicate TrackingIDs can be returned in queue consumer plugins.
- [#6913](https://github.com/influxdata/telegraf/issues/6913): Support up to 4096 stats in the ethtool input.
- [#6973](https://github.com/influxdata/telegraf/issues/6973): Expire metrics on query in addition to on add.

## v1.13.2 [2020-01-21]

### Bug Fixes

- [#2652](https://github.com/influxdata/telegraf/issues/2652): Warn without error when processes input is started on Windows.
- [#6890](https://github.com/influxdata/telegraf/issues/6890): Only parse certificate blocks in x509_cert input.
- [#6883](https://github.com/influxdata/telegraf/issues/6883): Add custom attributes for all resource types in vsphere input.
- [#6899](https://github.com/influxdata/telegraf/pull/6899): Fix URL agent address form with udp in snmp input.
- [#6619](https://github.com/influxdata/telegraf/issues/6619): Change logic to allow recording of device fields when attributes is false.
- [#6903](https://github.com/influxdata/telegraf/issues/6903): Do not add invalid timestamps to kafka messages.
- [#6906](https://github.com/influxdata/telegraf/issues/6906): Fix json_strict option and set default of true.

## v1.13.1 [2020-01-08]

### Bug Fixes

- [#6788](https://github.com/influxdata/telegraf/issues/6788): Fix ServerProperty query stops working on Azure after failover.
- [#6803](https://github.com/influxdata/telegraf/pull/6803): Add leading period to OID in SNMP v1 generic traps.
- [#6823](https://github.com/influxdata/telegraf/pull/6823): Fix missing config fields in prometheus serializer.
- [#6694](https://github.com/influxdata/telegraf/issues/6694): Fix panic on connection loss with undelivered messages in mqtt_consumer.
- [#6679](https://github.com/influxdata/telegraf/issues/6679): Encode query hash fields as hex strings in sqlserver input.
- [#6345](https://github.com/influxdata/telegraf/issues/6345): Invalidate diskio cache if the metadata mtime has changed.
- [#6800](https://github.com/influxdata/telegraf/issues/6800): Show platform not supported warning only on plugin creation.
- [#6814](https://github.com/influxdata/telegraf/issues/6814): Fix rabbitmq cannot complete gather after request error.
- [#6846](https://github.com/influxdata/telegraf/issues/6846): Fix /sbin/init --version executed on Telegraf startup.
- [#6847](https://github.com/influxdata/telegraf/issues/6847): Use last path element as field key if path fully specified in cisco_telemetry_gnmi input.

## v1.13 [2019-12-12]

### Release Notes

- Official packages built with Go 1.13.5.  This affects the minimum supported
  version on several platforms, most notably requiring Windows 7 (2008 R2) or
  later.  For details, check the release notes for Go
  [ports](https://golang.org/doc/go1.13#ports).
- The `prometheus` input and `prometheus_client` output have a new mapping to
  and from Telegraf metrics, which can be enabled by setting `metric_version = 2`.
  The original mapping is deprecated.  When both plugins have the same setting,
  passthrough metrics will be unchanged.  Refer to the `prometheus` input for
  details about the mapping.

### New Inputs

- [azure_storage_queue](/plugins/inputs/azure_storage_queue/README.md) - Contributed by @mjiderhamn
- [ethtool](/plugins/inputs/ethtool/README.md) - Contributed by @philippreston
- [snmp_trap](/plugins/inputs/snmp_trap/README.md) - Contributed by @influxdata
- [suricata](/plugins/inputs/suricata/README.md) - Contributed by @satta
- [synproxy](/plugins/inputs/synproxy/README.md) - Contributed by @rfrenayworldstream
- [systemd_units](/plugins/inputs/systemd_units/README.md) - Contributed by @benschweizer

### New Processors

- [clone](/plugins/processors/clone/README.md) - Contributed by @adrianlzt

### New Aggregators

- [merge](/plugins/aggregators/merge/README.md) - Contributed by @influxdata

### Features

- [#6326](https://github.com/influxdata/telegraf/pull/5842): Add per node memory stats to rabbitmq input.
- [#6361](https://github.com/influxdata/telegraf/pull/6361): Add ability to read query from file to postgresql_extensible input.
- [#5921](https://github.com/influxdata/telegraf/pull/5921): Add replication metrics to the redis input.
- [#6177](https://github.com/influxdata/telegraf/pull/6177): Support NX-OS telemetry extensions in cisco_telemetry_mdt.
- [#6415](https://github.com/influxdata/telegraf/pull/6415): Allow graphite parser to create Inf and NaN values.
- [#6434](https://github.com/influxdata/telegraf/pull/6434): Use prefix base detection for ints in grok parser.
- [#6465](https://github.com/influxdata/telegraf/pull/6465): Add more performance counter metrics to sqlserver input.
- [#6476](https://github.com/influxdata/telegraf/pull/6476): Add millisecond unix time support to grok parser.
- [#6473](https://github.com/influxdata/telegraf/pull/6473): Add container id as optional source tag to docker and docker_log input.
- [#6504](https://github.com/influxdata/telegraf/pull/6504): Add lang parameter to OpenWeathermap input plugin.
- [#6540](https://github.com/influxdata/telegraf/pull/6540): Log file open errors at debug level in tail input.
- [#6553](https://github.com/influxdata/telegraf/pull/6553): Add timeout option to cloudwatch input.
- [#6549](https://github.com/influxdata/telegraf/pull/6549): Support custom success codes in http input.
- [#6530](https://github.com/influxdata/telegraf/pull/6530): Improve ipvs input error strings and logging.
- [#6532](https://github.com/influxdata/telegraf/pull/6532): Add strict mode to JSON parser that can be disable to ignore invalid items.
- [#6543](https://github.com/influxdata/telegraf/pull/6543): Add support for Kubernetes 1.16 and remove deprecated API usage.
- [#6283](https://github.com/influxdata/telegraf/pull/6283): Add gathering of RabbitMQ federation link metrics.
- [#6356](https://github.com/influxdata/telegraf/pull/6356): Add bearer token defaults for Kubernetes plugins.
- [#5870](https://github.com/influxdata/telegraf/pull/5870): Add support for SNMP over TCP.
- [#6603](https://github.com/influxdata/telegraf/pull/6603): Add support for per output flush jitter.
- [#6650](https://github.com/influxdata/telegraf/pull/6650): Add a nameable file tag to file input plugin.
- [#6640](https://github.com/influxdata/telegraf/pull/6640): Add Splunk MultiMetric support.
- [#6680](https://github.com/influxdata/telegraf/pull/6668): Add support for sending HTTP Basic Auth in influxdb input
- [#5767](https://github.com/influxdata/telegraf/pull/5767): Add ability to configure the url tag in the prometheus input.
- [#5767](https://github.com/influxdata/telegraf/pull/5767): Add prometheus metric_version=2 mapping to internal metrics/line protocol.
- [#6703](https://github.com/influxdata/telegraf/pull/6703): Add prometheus metric_version=2 support to prometheus_client output.
- [#6660](https://github.com/influxdata/telegraf/pull/6660): Add content_encoding compression support to socket_listener.
- [#6689](https://github.com/influxdata/telegraf/pull/6689): Add high resolution metrics support to CloudWatch output.
- [#6716](https://github.com/influxdata/telegraf/pull/6716): Add SReclaimable and SUnreclaim to mem input.
- [#6695](https://github.com/influxdata/telegraf/pull/6695): Allow multiple certificates per file in x509_cert input.
- [#6686](https://github.com/influxdata/telegraf/pull/6686): Add additional tags to the x509 input.
- [#6703](https://github.com/influxdata/telegraf/pull/6703): Add batch data format support to file output.
- [#6688](https://github.com/influxdata/telegraf/pull/6688): Support partition assignment strategy configuration in kafka_consumer.
- [#6731](https://github.com/influxdata/telegraf/pull/6731): Add node type tag to mongodb input.
- [#6669](https://github.com/influxdata/telegraf/pull/6669): Add uptime_ns field to mongodb input.
- [#6735](https://github.com/influxdata/telegraf/pull/6735): Support resolution of symlinks in filecount input.
- [#6746](https://github.com/influxdata/telegraf/pull/6746): Set message timestamp to the metric time in kafka output.
- [#6740](https://github.com/influxdata/telegraf/pull/6740): Add base64decode operation to string processor.
- [#6790](https://github.com/influxdata/telegraf/pull/6790): Add option to control collecting global variables to mysql input.

### Bug Fixes

- [#6484](https://github.com/influxdata/telegraf/issues/6484): Show correct default settings in mysql sample config.
- [#6583](https://github.com/influxdata/telegraf/issues/6583): Use 1h or 3h rain values as appropriate in openweathermap input.
- [#6573](https://github.com/influxdata/telegraf/issues/6573): Fix not a valid field error in Windows with nvidia input.
- [#6614](https://github.com/influxdata/telegraf/issues/6614): Fix influxdb output serialization on connection closed.
- [#6690](https://github.com/influxdata/telegraf/issues/6690): Fix ping skips remaining hosts after dns lookup error.
- [#6684](https://github.com/influxdata/telegraf/issues/6684): Log mongodb oplog auth errors at debug level.
- [#6705](https://github.com/influxdata/telegraf/issues/6705): Remove trailing underscore trimming from json flattener.
- [#6421](https://github.com/influxdata/telegraf/issues/6421): Revert change causing cpu usage to be capped at 100 percent.
- [#6523](https://github.com/influxdata/telegraf/issues/6523): Accept any media type in the prometheus input.
- [#6769](https://github.com/influxdata/telegraf/issues/6769): Fix unix socket dial arguments in uwsgi input.
- [#6757](https://github.com/influxdata/telegraf/issues/6757): Replace colon chars in prometheus output labels with metric_version=1.
- [#6773](https://github.com/influxdata/telegraf/issues/6773): Set TrimLeadingSpace when TrimSpace is on in csv parser.

## v1.12.6 [2019-11-19]

### Bug Fixes

- [#6666](https://github.com/influxdata/telegraf/issues/6666): Fix many plugin errors are logged at debug logging level.
- [#6652](https://github.com/influxdata/telegraf/issues/6652): Use nanosecond precision in docker_log input.
- [#6642](https://github.com/influxdata/telegraf/issues/6642): Fix interface option with method = native in ping input.
- [#6680](https://github.com/influxdata/telegraf/pull/6680): Fix panic in mongodb input if shard connection pool stats are unreadable.

## v1.12.5 [2019-11-12]

### Bug Fixes

- [#6576](https://github.com/influxdata/telegraf/issues/6576): Fix incorrect results in ping input plugin.
- [#6610](https://github.com/influxdata/telegraf/pull/6610): Add missing character replacement to sql_instance tag.
- [#6337](https://github.com/influxdata/telegraf/issues/6337): Change no metric error message to debug level in cloudwatch input.
- [#6602](https://github.com/influxdata/telegraf/issues/6602): Add missing ServerProperties query to sqlserver input docs.
- [#6643](https://github.com/influxdata/telegraf/pull/6643): Fix mongodb connections_total_created field loading.
- [#6627](https://github.com/influxdata/telegraf/issues/6578): Fix metric creation when node is offline in jenkins input.
- [#6649](https://github.com/influxdata/telegraf/issues/6615): Fix docker uptime_ns calculation when container has been restarted.
- [#6647](https://github.com/influxdata/telegraf/issues/6646): Fix mysql field type conflict in conversion of gtid_mode to an integer.
- [#5529](https://github.com/influxdata/telegraf/issues/5529): Fix mysql field type conflict with ssl_verify_depth and ssl_ctx_verify_depth.

## v1.12.4 [2019-10-23]

### Release Notes

- Official packages built with Go 1.12.12.

### Bug Fixes

- [#6521](https://github.com/influxdata/telegraf/issues/6521): Fix metric generation with ping input native method.
- [#6541](https://github.com/influxdata/telegraf/issues/6541): Exclude alias tag if unset from plugin internal stats.
- [#6564](https://github.com/influxdata/telegraf/issues/6564): Fix socket_mode option in powerdns_recursor input.

## v1.12.3 [2019-10-07]

### Bug Fixes

- [#6445](https://github.com/influxdata/telegraf/issues/6445): Use batch serialization format in exec output.
- [#6455](https://github.com/influxdata/telegraf/issues/6455): Build official packages with Go 1.12.10.
- [#6464](https://github.com/influxdata/telegraf/pull/6464): Use case insensitive serial number match in smart input.
- [#6469](https://github.com/influxdata/telegraf/pull/6469): Add auth header only when env var is set.
- [#6468](https://github.com/influxdata/telegraf/pull/6468): Fix running multiple mysql and sqlserver plugin instances.
- [#6471](https://github.com/influxdata/telegraf/issues/6471): Fix database routing on retry with exclude_database_tag.
- [#6488](https://github.com/influxdata/telegraf/issues/6488): Fix logging panic in exec input with nagios data format.

## v1.12.2 [2019-09-24]

### Bug Fixes

- [#6386](https://github.com/influxdata/telegraf/issues/6386): Fix detection of layout timestamps in csv and json parser.
- [#6394](https://github.com/influxdata/telegraf/issues/6394): Fix parsing of BATTDATE in apcupsd input.
- [#6398](https://github.com/influxdata/telegraf/issues/6398): Keep boolean values listed in json_string_fields.
- [#6393](https://github.com/influxdata/telegraf/issues/6393): Disable Go plugin support in official builds.
- [#6391](https://github.com/influxdata/telegraf/issues/6391): Fix path handling issues in cisco_telemetry_gnmi.

## v1.12.1 [2019-09-10]

### Bug Fixes

- [#6344](https://github.com/influxdata/telegraf/issues/6344): Fix depends on GLIBC_2.14 symbol version.
- [#6329](https://github.com/influxdata/telegraf/issues/6329): Fix filecount for paths with trailing slash.
- [#6331](https://github.com/influxdata/telegraf/issues/6331): Convert check state to an integer in icinga2 input.
- [#6354](https://github.com/influxdata/telegraf/issues/6354): Fix could not mark message delivered error in kafka_consumer.
- [#6362](https://github.com/influxdata/telegraf/issues/6362): Skip collection stats when disabled in mongodb input.
- [#6366](https://github.com/influxdata/telegraf/issues/6366): Fix error reading closed response body on redirect in http_response.
- [#6373](https://github.com/influxdata/telegraf/issues/6373): Fix apcupsd documentation to reflect plugin.
- [#6375](https://github.com/influxdata/telegraf/issues/6375): Display retry log message only when retry after is received.

## v1.12 [2019-09-03]

### Release Notes

- The cluster health related fields in the elasticsearch input have been split
  out from the `elasticsearch_indices` measurement into the new
  `elasticsearch_cluster_health_indices` measurement as they were originally
  combined by error.

### New Inputs

- [apcupsd](/plugins/inputs/apcupsd/README.md) - Contributed by @jonaz
- [docker_log](/plugins/inputs/docker_log/README.md) - Contributed by @prashanthjbabu
- [fireboard](/plugins/inputs/fireboard/README.md) - Contributed by @ronnocol
- [logstash](/plugins/inputs/logstash/README.md) - Contributed by @lkmcs @dmitryilyin @arkady-emelyanov
- [marklogic](/plugins/inputs/marklogic/README.md) - Contributed by @influxdata
- [openntpd](/plugins/inputs/openntpd/README.md) - Contributed by @aromeyer
- [uwsgi](/plugins/inputs/uwsgi/README.md) - Contributed by @blaggacao

### New Parsers

- [form_urlencoded](/plugins/parsers/form_urlencoded/README.md) - Contributed by @byonchev

### New Processors

- [date](/plugins/processors/date/README.md) - Contributed by @influxdata
- [pivot](/plugins/processors/pivot/README.md) - Contributed by @influxdata
- [tag_limit](/plugins/processors/tag_limit/README.md) - Contributed by @memory
- [unpivot](/plugins/processors/unpivot/README.md) - Contributed by @influxdata

### New Outputs

- [exec](/plugins/outputs/exec/README.md) - Contributed by @Jaeyo

### Features

- [#5842](https://github.com/influxdata/telegraf/pull/5842): Improve performance of wavefront serializer.
- [#5863](https://github.com/influxdata/telegraf/pull/5863): Allow regex processor to append tag values.
- [#5997](https://github.com/influxdata/telegraf/pull/5997): Add starttime field to phpfpm input.
- [#5998](https://github.com/influxdata/telegraf/pull/5998): Add cluster name tag to elasticsearch indices.
- [#6006](https://github.com/influxdata/telegraf/pull/6006): Add support for interface field in http_response input plugin.
- [#5996](https://github.com/influxdata/telegraf/pull/5996): Add container uptime_ns in docker input plugin.
- [#6016](https://github.com/influxdata/telegraf/pull/6016): Add better user-facing errors for API timeouts in docker input.
- [#6027](https://github.com/influxdata/telegraf/pull/6027): Add TLS mutual auth support to jti_openconfig_telemetry input.
- [#6053](https://github.com/influxdata/telegraf/pull/6053): Add support for ES 7.x to elasticsearch output.
- [#6062](https://github.com/influxdata/telegraf/pull/6062): Add basic auth to prometheus input plugin.
- [#6064](https://github.com/influxdata/telegraf/pull/6064): Add node roles tag to elasticsearch input.
- [#5572](https://github.com/influxdata/telegraf/pull/5572): Support floats in statsd percentiles.
- [#6050](https://github.com/influxdata/telegraf/pull/6050): Add native Go ping method to ping input plugin.
- [#6074](https://github.com/influxdata/telegraf/pull/6074): Resume from last known offset in tail inputwhen reloading Telegraf.
- [#6111](https://github.com/influxdata/telegraf/pull/6111): Add improved support for Azure SQL Database to sqlserver input.
- [#6079](https://github.com/influxdata/telegraf/pull/6079): Add extra attributes for NVMe devices to smart input.
- [#6084](https://github.com/influxdata/telegraf/pull/6084): Add docker_devicemapper measurement to docker input plugin.
- [#6122](https://github.com/influxdata/telegraf/pull/6122): Add basic auth support to elasticsearch input.
- [#6102](https://github.com/influxdata/telegraf/pull/6102): Support string field glob matching in json parser.
- [#6101](https://github.com/influxdata/telegraf/pull/6101): Update gjson to allow multipath syntax in json parser.
- [#6144](https://github.com/influxdata/telegraf/pull/6144): Add support for collecting SQL Requests to identify waits and blocking to sqlserver input.
- [#6105](https://github.com/influxdata/telegraf/pull/6105): Collect k8s endpoints, ingress, and services in kube_inventory plugin.
- [#6129](https://github.com/influxdata/telegraf/pull/6129): Add support for field/tag keys to strings processor.
- [#6143](https://github.com/influxdata/telegraf/pull/6143): Add certificate verification status to x509_cert input.
- [#6163](https://github.com/influxdata/telegraf/pull/6163): Support percentage value parsing in redis input.
- [#6024](https://github.com/influxdata/telegraf/pull/6024): Load external Go plugins from --plugin-directory.
- [#6184](https://github.com/influxdata/telegraf/pull/6184): Add ability to exclude db/bucket tag from influxdb outputs.
- [#6137](https://github.com/influxdata/telegraf/pull/6137): Gather per collections stats in mongodb input plugin.
- [#6195](https://github.com/influxdata/telegraf/pull/6195): Add TLS & credentials configuration for nats_consumer input plugin.
- [#6194](https://github.com/influxdata/telegraf/pull/6194): Add support for enterprise repos to github plugin.
- [#6060](https://github.com/influxdata/telegraf/pull/6060): Add Indices stats to elasticsearch input.
- [#6189](https://github.com/influxdata/telegraf/pull/6189): Add left function to string processor.
- [#6049](https://github.com/influxdata/telegraf/pull/6049): Add grace period for metrics late for aggregation.
- [#4435](https://github.com/influxdata/telegraf/pull/4435): Add diff and non_negative_diff to basicstats aggregator.
- [#6201](https://github.com/influxdata/telegraf/pull/6201): Add device tags to smart_attributes.
- [#5719](https://github.com/influxdata/telegraf/pull/5719): Collect framework_offers and allocator metrics in mesos input.
- [#6216](https://github.com/influxdata/telegraf/pull/6216): Add telegraf and go version to the internal input plugin.
- [#6214](https://github.com/influxdata/telegraf/pull/6214): Update the number of logical CPUs dynamically in system plugin.
- [#6259](https://github.com/influxdata/telegraf/pull/6259): Add darwin (macOS) builds to the release.
- [#6241](https://github.com/influxdata/telegraf/pull/6241): Add configurable timeout setting to smart input.
- [#6249](https://github.com/influxdata/telegraf/pull/6249): Add memory_usage field to procstat input plugin.
- [#5971](https://github.com/influxdata/telegraf/pull/5971): Add support for custom attributes to vsphere input.
- [#5926](https://github.com/influxdata/telegraf/pull/5926): Add cmdstat metrics to redis input.
- [#6261](https://github.com/influxdata/telegraf/pull/6261): Add content_length metric to http_response input plugin.
- [#6257](https://github.com/influxdata/telegraf/pull/6257): Add database_tag option to influxdb_listener to add database from query string.
- [#6246](https://github.com/influxdata/telegraf/pull/6246): Add capability to limit TLS versions and cipher suites.
- [#6266](https://github.com/influxdata/telegraf/pull/6266): Add topic_tag option to mqtt_consumer.
- [#6207](https://github.com/influxdata/telegraf/pull/6207): Add ability to label inputs for logging.
- [#6300](https://github.com/influxdata/telegraf/pull/6300): Add TLS support to nginx_plus, nginx_plus_api and nginx_vts.

### Bug Fixes

- [#5692](https://github.com/influxdata/telegraf/issues/5692): Fix sensor read error stops reporting of all sensors in temp input.
- [#4356](https://github.com/influxdata/telegraf/issues/4356): Fix double pct replacement in sysstat input.
- [#6004](https://github.com/influxdata/telegraf/issues/6004): Fix race in master node detection in elasticsearch input.
- [#6100](https://github.com/influxdata/telegraf/issues/6100): Fix SSPI authentication not working in sqlserver input.
- [#6142](https://github.com/influxdata/telegraf/issues/6142): Fix memory error panic in mqtt input.
- [#6136](https://github.com/influxdata/telegraf/issues/6136): Support Kafka 2.3.0 consumer groups.
- [#6232](https://github.com/influxdata/telegraf/issues/6232): Fix persistent session in mqtt_consumer.
- [#6235](https://github.com/influxdata/telegraf/issues/6235): Fix finder inconsistencies in vsphere input.
- [#6138](https://github.com/influxdata/telegraf/issues/6138): Fix parsing multiple metrics on the first line of tailed file.
- [#2526](https://github.com/influxdata/telegraf/issues/2526): Send TERM to exec processes before sending KILL signal.
- [#5326](https://github.com/influxdata/telegraf/issues/5326): Query oplog only when connected to a replica set.
- [#6317](https://github.com/influxdata/telegraf/pull/6317): Use environment variables to locate Program Files on Windows.

## v1.11.5 [2019-08-27]

### Bug Fixes

- [#6250](https://github.com/influxdata/telegraf/pull/6250): Update go-sql-driver/mysql driver to 1.4.1 to address auth issues.
- [#6279](https://github.com/influxdata/telegraf/issues/6279): Return error status from --test if input plugins produce an error.
- [#6309](https://github.com/influxdata/telegraf/issues/6309): Fix with multiple instances only last configuration is used in smart input.
- [#6303](https://github.com/influxdata/telegraf/pull/6303): Build official packages with Go 1.12.9.
- [#6234](https://github.com/influxdata/telegraf/issues/6234): Split out -w argument in iptables input.
- [#6270](https://github.com/influxdata/telegraf/issues/6270): Add support for parked process state on Linux.
- [#6287](https://github.com/influxdata/telegraf/issues/6287): Remove leading slash from rcon command.
- [#6313](https://github.com/influxdata/telegraf/pull/6313): Allow jobs with dashes in the name in lustre2 input.

## v1.11.4 [2019-08-06]

### Bug Fixes

- [#6200](https://github.com/influxdata/telegraf/pull/6200): Correct typo in kubernetes logsfs_available_bytes field.
- [#6191](https://github.com/influxdata/telegraf/issues/6191): Skip floats that are NaN or Inf in Datadog output.
- [#6209](https://github.com/influxdata/telegraf/issues/6209): Fix reload panic in socket_listener input plugin.

## v1.11.3 [2019-07-23]

### Bug Fixes

- [#6054](https://github.com/influxdata/telegraf/issues/6054): Fix unable to reconnect after vCenter reboot in vsphere input.
- [#6073](https://github.com/influxdata/telegraf/issues/6073): Handle unknown error in nvidia-smi output.
- [#6121](https://github.com/influxdata/telegraf/pull/6121): Fix panic in statd input when processing datadog events.
- [#6125](https://github.com/influxdata/telegraf/issues/6125): Treat empty array as successful parse in json parser.
- [#6094](https://github.com/influxdata/telegraf/issues/6094): Add missing rcode and zonestat to bind input.
- [#6114](https://github.com/influxdata/telegraf/issues/6114): Fix lustre2 input plugin config parse regression.
- [#5894](https://github.com/influxdata/telegraf/issues/5894): Fix template pattern partial wildcard matching.
- [#6151](https://github.com/influxdata/telegraf/issues/6151): Fix panic in github input.

## v1.11.2 [2019-07-09]

### Bug Fixes

- [#6056](https://github.com/influxdata/telegraf/pull/6056): Fix source address ping flag on BSD.
- [#6059](https://github.com/influxdata/telegraf/issues/6059): Fix value out of range error on 32-bit systems in bind input.
- [#3573](https://github.com/influxdata/telegraf/issues/3573): Fix tail and logparser stop working after reload.
- [#6077](https://github.com/influxdata/telegraf/pull/6077): Fix filecount path separator handling in Windows.
- [#6075](https://github.com/influxdata/telegraf/issues/6075): Fix panic with empty datadog tag string.
- [#6069](https://github.com/influxdata/telegraf/issues/6069): Apply topic filter to partition metrics in burrow input.

## v1.11.1 [2019-06-25]

### Bug Fixes

- [#5980](https://github.com/influxdata/telegraf/issues/5980): Cannot set mount_points option in disk input.
- [#5983](https://github.com/influxdata/telegraf/issues/5983): Omit keys when creating measurement names for GNMI telemetry.
- [#5972](https://github.com/influxdata/telegraf/issues/5972): Don't consider pid of 0 when using systemd lookup in procstat.
- [#5807](https://github.com/influxdata/telegraf/issues/5807): Skip 404 error reporting in nginx_plus_api input.
- [#5999](https://github.com/influxdata/telegraf/issues/5999): Fix panic if pool_mode column does not exist.
- [#6019](https://github.com/influxdata/telegraf/issues/6019): Add missing container_id field to docker_container_status metrics.
- [#5742](https://github.com/influxdata/telegraf/issues/5742): Ignore error when utmp is missing in system input.
- [#6032](https://github.com/influxdata/telegraf/issues/6032): Add device, serial_no, and wwn tags to synthetic attributes.
- [#6012](https://github.com/influxdata/telegraf/issues/6012): Fix parsing of remote tcp address in statsd input.

## v1.11 [2019-06-11]

### Release Notes

- The `uptime_format` field in the system input has been deprecated, use the
  `uptime` field instead.
- The `cloudwatch` input has been updated to use a more efficient API, it now
  requires `GetMetricData` permissions instead of `GetMetricStatistics`.  The
  `units` tag is not available from this API and is no longer collected.

### New Inputs

- [bind](/plugins/inputs/bind/README.md) - Contributed by @dswarbrick & @danielllek
- [cisco_telemetry_gnmi](/plugins/inputs/cisco_telemetry_gnmi/README.md) - Contributed by @sbyx
- [cisco_telemetry_mdt](/plugins/inputs/cisco_telemetry_mdt/README.md) - Contributed by @sbyx
- [ecs](/plugins/inputs/ecs/README.md) - Contributed by @rbtr
- [github](/plugins/inputs/github/README.md) - Contributed by @influxdata
- [openweathermap](/plugins/inputs/openweathermap/README.md) - Contributed by @regel
- [powerdns_recursor](/plugins/inputs/powerdns_recursor/README.md) - Contributed by @dupondje

### New Aggregators

- [final](/plugins/aggregators/final/README.md) - Contributed by @oplehto

### New Outputs

- [syslog](/plugins/outputs/syslog/README.md) - Contributed by @javicrespo
- [health](/plugins/outputs/health/README.md) - Contributed by @influxdata

### New Serializers

- [wavefront](/plugins/serializers/wavefront/README.md) - Contributed by @puckpuck

### Features

- [#5556](https://github.com/influxdata/telegraf/pull/5556): Add TTL field to ping input.
- [#5569](https://github.com/influxdata/telegraf/pull/5569): Add hexadecimal string to integer conversion to converter processor.
- [#5601](https://github.com/influxdata/telegraf/pull/5601): Add support for multiple line text and perfdata to nagios parser.
- [#5648](https://github.com/influxdata/telegraf/pull/5648): Allow env vars ${} expansion syntax in configuration file.
- [#5641](https://github.com/influxdata/telegraf/pull/5641): Add option to reset buckets on flush to histogram aggregator.
- [#5664](https://github.com/influxdata/telegraf/pull/5664): Add option to use strict sanitization rules to wavefront output.
- [#5697](https://github.com/influxdata/telegraf/pull/5697): Add namespace restriction to prometheus input plugin.
- [#5681](https://github.com/influxdata/telegraf/pull/5681): Add cmdline tag to procstat input.
- [#5704](https://github.com/influxdata/telegraf/pull/5704): Support verbose query param in ping endpoint of influxdb_listener.
- [#5713](https://github.com/influxdata/telegraf/pull/5713): Enhance HTTP connection options for phpfpm input plugin.
- [#5544](https://github.com/influxdata/telegraf/pull/5544): Use more efficient GetMetricData API to collect cloudwatch metrics.
- [#5544](https://github.com/influxdata/telegraf/pull/5544): Allow selection of collected statistic types in cloudwatch input.
- [#5757](https://github.com/influxdata/telegraf/pull/5757): Speed up interface stat collection in net input.
- [#5769](https://github.com/influxdata/telegraf/pull/5769): Add pagefault data to procstat input plugin.
- [#5760](https://github.com/influxdata/telegraf/pull/5760): Add option to set permissions for unix domain sockets to socket_listener.
- [#5585](https://github.com/influxdata/telegraf/pull/5585): Add cli support for outputting sections of the config.
- [#5770](https://github.com/influxdata/telegraf/pull/5770): Add service-display-name option for use with Windows service.
- [#5778](https://github.com/influxdata/telegraf/pull/5778): Add support for log rotation.
- [#5765](https://github.com/influxdata/telegraf/pull/5765): Support more drive types in smart input.
- [#5829](https://github.com/influxdata/telegraf/pull/5829): Add support for HTTP basic auth to solr input.
- [#5791](https://github.com/influxdata/telegraf/pull/5791): Add support for datadog events to statsd input.
- [#5817](https://github.com/influxdata/telegraf/pull/5817): Allow devices option to match against devlinks.
- [#5855](https://github.com/influxdata/telegraf/pull/5855): Support tags in enum processor.
- [#5830](https://github.com/influxdata/telegraf/pull/5830): Add support for gzip compression to amqp plugins.
- [#5831](https://github.com/influxdata/telegraf/pull/5831): Support passive queue declaration in amqp_consumer.
- [#5901](https://github.com/influxdata/telegraf/pull/5901): Set user agent in stackdriver output.
- [#5885](https://github.com/influxdata/telegraf/pull/5885): Extend metrics collected from Nvidia GPUs.
- [#5547](https://github.com/influxdata/telegraf/pull/5547): Add file rotation support to the file output.
- [#5955](https://github.com/influxdata/telegraf/pull/5955): Add source tag to hddtemp plugin.

### Bug Fixes

- [#5692](https://github.com/influxdata/telegraf/pull/5692): Temperature input plugin stops working when WiFi is turned off.
- [#5631](https://github.com/influxdata/telegraf/pull/5631): Create Windows service only when specified or in service manager.
- [#5730](https://github.com/influxdata/telegraf/pull/5730): Don't start telegraf when stale pidfile found.
- [#5477](https://github.com/influxdata/telegraf/pull/5477): Support Minecraft server 1.13 and newer in minecraft input.
- [#4098](https://github.com/influxdata/telegraf/issues/4098): Fix inline table support in configuration file.
- [#1598](https://github.com/influxdata/telegraf/issues/1598): Fix multi-line basic strings support in configuration file.
- [#5746](https://github.com/influxdata/telegraf/issues/5746): Verify a process passed by pid_file exists in procstat input.
- [#5455](https://github.com/influxdata/telegraf/issues/5455): Fix unsupported pkt type error in pgbouncer.
- [#5771](https://github.com/influxdata/telegraf/pull/5771): Fix only one job per storage target reported in lustre2 input.
- [#5796](https://github.com/influxdata/telegraf/issues/5796): Set default timeout of 5s in fibaro input.
- [#5835](https://github.com/influxdata/telegraf/issues/5835): Fix docker input does not parse image name correctly.
- [#5661](https://github.com/influxdata/telegraf/issues/5661): Fix direct exchange routing key in amqp output.
- [#5819](https://github.com/influxdata/telegraf/issues/5819): Fix scale set resource id with azure_monitor output.
- [#5883](https://github.com/influxdata/telegraf/issues/5883): Skip invalid power times in apex_neptune input.
- [#3485](https://github.com/influxdata/telegraf/issues/3485): Fix sqlserver connection closing on error.
- [#5917](https://github.com/influxdata/telegraf/issues/5917): Fix toml option name in nginx_upstream_check.
- [#5920](https://github.com/influxdata/telegraf/issues/5920): Fixed datastore name mapping in vsphere input.
- [#5879](https://github.com/influxdata/telegraf/issues/5879): Fix multiple SIGHUP causes Telegraf to shutdown.
- [#5891](https://github.com/influxdata/telegraf/issues/5891): Fix connection leak in influxdb outputs on reload.
- [#5858](https://github.com/influxdata/telegraf/issues/5858): Fix batch fails when single metric is unserializable.
- [#5536](https://github.com/influxdata/telegraf/issues/5536): Log a warning on write if the metric buffer has overflowed.

## v1.10.4 [2019-05-14]

### Bug Fixes

- [#5764](https://github.com/influxdata/telegraf/pull/5764): Fix race condition in the Wavefront parser.
- [#5783](https://github.com/influxdata/telegraf/pull/5783): Create telegraf user in pre-install rpm scriptlet.
- [#5792](https://github.com/influxdata/telegraf/pull/5792): Don't discard metrics on forbidden error in influxdb_v2 output.
- [#5803](https://github.com/influxdata/telegraf/issues/5803): Fix http output cannot set Host header.
- [#5619](https://github.com/influxdata/telegraf/issues/5619): Fix interval estimation in vsphere input.
- [#5782](https://github.com/influxdata/telegraf/pull/5782): Skip lines with missing refid in ntpq input.
- [#5755](https://github.com/influxdata/telegraf/issues/5755): Add support for hex values to ipmi_sensor input.
- [#5824](https://github.com/influxdata/telegraf/issues/5824): Fix parse of unix timestamp with more than ns precision.
- [#5836](https://github.com/influxdata/telegraf/issues/5836): Restore field name case in interrupts input.

## v1.10.3 [2019-04-16]

### Bug Fixes

- [#5680](https://github.com/influxdata/telegraf/pull/5680): Allow colons in metric names in prometheus_client output.
- [#5716](https://github.com/influxdata/telegraf/pull/5716): Set log directory attributes in rpm spec.

## v1.10.2 [2019-04-02]

### Release Notes

- String fields no longer have leading and trailing quotation marks removed in
  the grok parser.  If you are capturing quoted strings you may need to update
  the patterns.

### Bug Fixes

- [#5612](https://github.com/influxdata/telegraf/pull/5612): Fix deadlock when Telegraf is aligning aggregators.
- [#5523](https://github.com/influxdata/telegraf/issues/5523): Fix missing cluster stats in ceph input.
- [#5566](https://github.com/influxdata/telegraf/pull/5566): Fix reading major and minor block devices identifiers in diskio input.
- [#5607](https://github.com/influxdata/telegraf/pull/5607): Add owned directories to rpm package spec.
- [#4998](https://github.com/influxdata/telegraf/issues/4998): Fix last character removed from string field in grok parser.
- [#5632](https://github.com/influxdata/telegraf/pull/5632): Fix drop tracking of metrics removed with aggregator drop_original.
- [#5540](https://github.com/influxdata/telegraf/pull/5540): Fix open file error handling in file output.
- [#5626](https://github.com/influxdata/telegraf/issues/5626): Fix plugin name in influxdb_v2 output logging.
- [#5621](https://github.com/influxdata/telegraf/issues/5621): Fix basedir check and parent dir extraction in filecount input.
- [#5618](https://github.com/influxdata/telegraf/issues/5618): Listen before leaving start in statsd.
- [#5595](https://github.com/influxdata/telegraf/issues/5595): Fix aggregator window alignment.
- [#5637](https://github.com/influxdata/telegraf/issues/5637): Fix panic during shutdown of multiple aggregators.
- [#5642](https://github.com/influxdata/telegraf/issues/5642): Fix parsing of kube config certificate-authority-data in prometheus input.
- [#5636](https://github.com/influxdata/telegraf/issues/5636): Fix tags applied to wrong metric on parse error.
- [#5522](https://github.com/influxdata/telegraf/issues/5522): Remove tags that would create invalid label names in prometheus output.

## v1.10.1 [2019-03-19]

### Bug Fixes

- [#5448](https://github.com/influxdata/telegraf/issues/5448): Show error when TLS configuration cannot be loaded.
- [#5543](https://github.com/influxdata/telegraf/pull/5543): Add Base64-encoding/decoding for Google Cloud PubSub plugins.
- [#5565](https://github.com/influxdata/telegraf/issues/5565): Fix type compatibility in vsphere plugin with use_int_samples option.
- [#5492](https://github.com/influxdata/telegraf/issues/5492): Fix vsphere input shows failed task in vCenter.
- [#5530](https://github.com/influxdata/telegraf/issues/5530): Fix invalid measurement name and skip column in csv parser.
- [#5589](https://github.com/influxdata/telegraf/issues/5589): Fix system input causing high cpu usage on Raspbian.
- [#5575](https://github.com/influxdata/telegraf/issues/5575): Don't add empty healthcheck tags to consul input.

## v1.10 [2019-03-05]

### New Inputs

- [cloud_pubsub](/plugins/inputs/cloud_pubsub/README.md) - Contributed by @emilymye
- [cloud_pubsub_push](/plugins/inputs/cloud_pubsub_push/README.md) - Contributed by @influxdata
- [kinesis_consumer](/plugins/inputs/kinesis_consumer/README.md) - Contributed by @influxdata
- [kube_inventory](/plugins/inputs/kube_inventory/README.md) - Contributed by @influxdata
- [neptune_apex](/plugins/inputs/neptune_apex/README.md) - Contributed by @MaxRenaud
- [nginx_upstream_check](/plugins/inputs/nginx_upstream_check/README.md) - Contributed by @dmitryilyin
- [multifile](/plugins/inputs/multifile/README.md) - Contributed by @martin2250
- [stackdriver](/plugins/inputs/stackdriver/README.md) - Contributed by @WuHan0608

### New Outputs

- [cloud_pubsub](/plugins/outputs/cloud_pubsub/README.md) - Contributed by @emilymye

### New Serializers

- [nowmetric](/plugins/serializers/nowmetric/README.md) - Contributed by @JefMuller
- [carbon2](/plugins/serializers/carbon2/README.md) - Contributed by @frankreno

### Features

- [#4345](https://github.com/influxdata/telegraf/pull/4345): Allow for force gathering ES cluster stats.
- [#5047](https://github.com/influxdata/telegraf/pull/5047): Add support for unix and unix_ms timestamps to csv parser.
- [#5038](https://github.com/influxdata/telegraf/pull/5038): Add ability to tag metrics with topic in kafka_consumer.
- [#5024](https://github.com/influxdata/telegraf/pull/5024): Add option to store cpu as a tag in interrupts input.
- [#5074](https://github.com/influxdata/telegraf/pull/5074): Add support for sending a request body to http input.
- [#5069](https://github.com/influxdata/telegraf/pull/5069): Add running field to procstat_lookup.
- [#5116](https://github.com/influxdata/telegraf/pull/5116): Include DEVLINKS in available diskio udev properties.
- [#5149](https://github.com/influxdata/telegraf/pull/5149): Add micro and nanosecond unix timestamp support to JSON parser.
- [#5160](https://github.com/influxdata/telegraf/pull/5160): Add support for basic auth to couchdb input.
- [#5161](https://github.com/influxdata/telegraf/pull/5161): Add support in wavefront output for the Wavefront Direct Ingestion API.
- [#5168](https://github.com/influxdata/telegraf/pull/5168): Allow counting float values in valuecounter aggregator.
- [#5177](https://github.com/influxdata/telegraf/pull/5177): Add log send and redo queue fields to sqlserver input.
- [#5113](https://github.com/influxdata/telegraf/pull/5113): Improve scalability of vsphere input.
- [#5210](https://github.com/influxdata/telegraf/pull/5210): Add read and write op per second fields to ceph input.
- [#5214](https://github.com/influxdata/telegraf/pull/5214): Add configurable timeout to varnish input.
- [#5273](https://github.com/influxdata/telegraf/pull/5273): Add flush_total_time_ns and additional wired tiger fields to mongodb input.
- [#5295](https://github.com/influxdata/telegraf/pull/5295): Support passing bearer token directly in k8s input.
- [#5294](https://github.com/influxdata/telegraf/pull/5294): Support passing bearer token directly in prometheus input.
- [#5292](https://github.com/influxdata/telegraf/pull/5292): Add option to report input timestamp in prometheus output.
- [#5234](https://github.com/influxdata/telegraf/pull/5234): Add Linux mipsle packages.
- [#5382](https://github.com/influxdata/telegraf/pull/5382): Support unix_us and unix_ns timestamp format in csv parser.
- [#5391](https://github.com/influxdata/telegraf/pull/5391): Add resource type and resource label support to stackdriver output.
- [#5396](https://github.com/influxdata/telegraf/pull/5396): Add internal metric for line too long in influxdb_listener.
- [#4892](https://github.com/influxdata/telegraf/pull/4892): Add option to set retain flag on messages to mqtt output.
- [#5165](https://github.com/influxdata/telegraf/pull/5165): Add resource path based filtering to vsphere input.
- [#5417](https://github.com/influxdata/telegraf/pull/5417): Add rcode tag and field to dns_query input.
- [#5453](https://github.com/influxdata/telegraf/pull/5453): Support Azure Sovereign Environments with endpoint_url option.
- [#5472](https://github.com/influxdata/telegraf/pull/5472): Support configuring a default timezone in JSON parser.
- [#5482](https://github.com/influxdata/telegraf/pull/5482): Add ceph_health metrics to ceph input.
- [#5488](https://github.com/influxdata/telegraf/pull/5488): Add option to disable unique timestamp adjustment in grok parser.
- [#5473](https://github.com/influxdata/telegraf/pull/5473): Add mutual TLS support to prometheus_client output.
- [#4308](https://github.com/influxdata/telegraf/pull/4308): Add additional metrics to rabbitmq input.
- [#5388](https://github.com/influxdata/telegraf/pull/5388): Add multicast support to socket_listener input.
- [#5490](https://github.com/influxdata/telegraf/pull/5490): Add tag based routing in influxdb/influxdb_v2 outputs.
- [#5533](https://github.com/influxdata/telegraf/pull/5533): Allow grok parser to produce metrics with no fields.

### Bug Fixes

- [#4610](https://github.com/influxdata/telegraf/pull/4610): Fix initscript removes pidfile of restarted Telegraf process.
- [#5320](https://github.com/influxdata/telegraf/pull/5320): Use datacenter option spelling in consul input.
- [#5316](https://github.com/influxdata/telegraf/pull/5316): Remove auth from /ping route in influxdb_listener.
- [#5304](https://github.com/influxdata/telegraf/issues/5304): Fix x509_cert input stops checking certs after first error.
- [#5404](https://github.com/influxdata/telegraf/issues/5404): Group stackdriver requests to send one point per timeseries.
- [#5449](https://github.com/influxdata/telegraf/issues/5449): Log permission error and ignore in filecount input.
- [#5497](https://github.com/influxdata/telegraf/pull/5497): Create log file in append mode.
- [#5325](https://github.com/influxdata/telegraf/issues/5325): Ignore tracking for metrics added to aggregator.
- [#5514](https://github.com/influxdata/telegraf/issues/5514): Fix panic when rejecting empty batch.
- [#5518](https://github.com/influxdata/telegraf/pull/5518): Fix conversion from string float to integer.
- [#5431](https://github.com/influxdata/telegraf/pull/5431): Sort metrics by timestamp in prometheus output.

## v1.9.5 [2019-02-26]

### Bug Fixes

- [#5315](https://github.com/influxdata/telegraf/issues/5315): Skip string fields when writing to stackdriver output.
- [#5364](https://github.com/influxdata/telegraf/issues/5364): Send metrics in ascending time order in stackdriver output.
- [#5117](https://github.com/influxdata/telegraf/issues/5117): Use systemd in Amazon Linux 2 rpm.
- [#4988](https://github.com/influxdata/telegraf/issues/4988): Set deadlock priority in sqlserver input.
- [#5403](https://github.com/influxdata/telegraf/issues/5403): Remove error log when snmp6 directory does not exists with nstat input.
- [#5437](https://github.com/influxdata/telegraf/issues/5437): Host not added when using custom arguments in ping plugin.
- [#5438](https://github.com/influxdata/telegraf/issues/5438): Fix InfluxDB output UDP line splitting.
- [#5456](https://github.com/influxdata/telegraf/issues/5456): Disable results by row in azuredb query.
- [#5277](https://github.com/influxdata/telegraf/issues/5277): Add backwards compatibility fields in ceph usage and pool stats.

## v1.9.4 [2019-02-05]

### Bug Fixes

- [#5334](https://github.com/influxdata/telegraf/issues/5334): Fix skip_rows and skip_columns options in csv parser.
- [#5181](https://github.com/influxdata/telegraf/issues/5181): Always send basic auth in jenkins input.
- [#5346](https://github.com/influxdata/telegraf/pull/5346): Build official packages with Go 1.11.5.
- [#5368](https://github.com/influxdata/telegraf/issues/5368): Fix definition of multiple syslog plugins.

## v1.9.3 [2019-01-22]

### Bug Fixes

- [#5261](https://github.com/influxdata/telegraf/pull/5261):  Fix arithmetic overflow in sqlserver input.
- [#5194](https://github.com/influxdata/telegraf/issues/5194): Fix latest metrics not sent first when output fails.
- [#5285](https://github.com/influxdata/telegraf/issues/5285): Fix amqp_consumer stops consuming when it receives unparseable messages.
- [#5281](https://github.com/influxdata/telegraf/issues/5281): Fix prometheus input not detecting added and removed pods.
- [#5215](https://github.com/influxdata/telegraf/issues/5215): Remove userinfo from cluster tag in couchbase.
- [#5298](https://github.com/influxdata/telegraf/issues/5298): Fix internal_write buffer_size not reset on timed writes.

## v1.9.2 [2019-01-08]

### Bug Fixes

- [#5130](https://github.com/influxdata/telegraf/pull/5130): Increase varnishstat timeout.
- [#5135](https://github.com/influxdata/telegraf/pull/5135): Remove storage calculation for non Azure managed instances and add server version.
- [#5083](https://github.com/influxdata/telegraf/pull/5083): Fix error sending empty tag value in azure_monitor output.
- [#5143](https://github.com/influxdata/telegraf/issues/5143): Fix panic with prometheus input plugin on shutdown.
- [#4482](https://github.com/influxdata/telegraf/issues/4482): Support non-transparent framing of syslog messages.
- [#5151](https://github.com/influxdata/telegraf/issues/5151): Apply global and plugin level metric modifications before filtering.
- [#5167](https://github.com/influxdata/telegraf/pull/5167): Fix num_remapped_pgs field in ceph plugin.
- [#5179](https://github.com/influxdata/telegraf/issues/5179): Add PDH_NO_DATA to known counter error codes in win_perf_counters.
- [#5170](https://github.com/influxdata/telegraf/issues/5170): Fix amqp_consumer stops consuming on empty message.
- [#4906](https://github.com/influxdata/telegraf/issues/4906): Fix multiple replace tables not working in strings processor.
- [#5219](https://github.com/influxdata/telegraf/issues/5219): Allow non local udp connections in net_response.
- [#5218](https://github.com/influxdata/telegraf/issues/5218): Fix toml option names in parser processor.
- [#5225](https://github.com/influxdata/telegraf/issues/5225): Fix panic in docker input with bad endpoint.
- [#5209](https://github.com/influxdata/telegraf/issues/5209): Fix original metric modified by aggregator filters.

## v1.9.1 [2018-12-11]

### Bug Fixes

- [#5006](https://github.com/influxdata/telegraf/issues/5006): Fix boolean handling in splunkmetric serializer.
- [#5046](https://github.com/influxdata/telegraf/issues/5046): Set default config values in jenkins input.
- [#4664](https://github.com/influxdata/telegraf/issues/4664): Fix server connection and document stats in mongodb input.
- [#5010](https://github.com/influxdata/telegraf/issues/5010): Add X-Requested-By header to graylog input.
- [#5052](https://github.com/influxdata/telegraf/issues/5052): Fix metric memory not freed from the metric buffer on write.
- [#3817](https://github.com/influxdata/telegraf/issues/3817): Add support for client tls certificates in postgresql inputs.
- [#5082](https://github.com/influxdata/telegraf/issues/5082): Prevent panic when marking the offset in kafka_consumer.
- [#5084](https://github.com/influxdata/telegraf/issues/5084): Add early metrics to aggregator and honor drop_original setting.
- [#5112](https://github.com/influxdata/telegraf/pull/5112): Use -W flag on bsd variants in ping input.
- [#5114](https://github.com/influxdata/telegraf/issues/5114): Allow delta metrics in wavefront parser.

## v1.9 [2018-11-20]

### Release Notes

- The `http_listener` input plugin has been renamed to `influxdb_listener` and
  use of the original name is deprecated.  The new name better describes the
  intended use of the plugin as a InfluxDB relay.  For general purpose
  transfer of metrics in any format via HTTP, it is recommended to use
  `http_listener_v2` instead.

- Input plugins are no longer limited from adding metrics when the output is
  writing, and new metrics will move into the metric buffer as needed.  This
  will provide more robust degradation and recovery when writing to a slow
  output at high throughput.

  To avoid over consumption when reading from queue consumers: `kafka_consumer`,
  `amqp_consumer`, `mqtt_consumer`, `nats_consumer`, and `nsq_consumer` use
  the new option `max_undelivered_messages` to limit the number of outstanding
  unwritten metrics.

### New Inputs

- [http_listener_v2](/plugins/inputs/http_listener_v2/README.md) - Contributed by @jul1u5
- [ipvs](/plugins/inputs/ipvs/README.md) - Contributed by @amoghe
- [jenkins](/plugins/inputs/jenkins/README.md) - Contributed by @influxdata & @lpic10
- [nginx_plus_api](/plugins/inputs/nginx_plus_api/README.md) - Contributed by @Bugagazavr
- [nginx_vts](/plugins/inputs/nginx_vts/README.md) - Contributed by @monder
- [wireless](/plugins/inputs/wireless/README.md) - Contributed by @jamesmaidment

### New Outputs

- [stackdriver](/plugins/outputs/stackdriver/README.md) - Contributed by @jamesmaidment

### Features

- [#4686](https://github.com/influxdata/telegraf/pull/4686): Add replace function to strings processor.
- [#4754](https://github.com/influxdata/telegraf/pull/4754): Query servers in parallel in dns_query input.
- [#4753](https://github.com/influxdata/telegraf/pull/4753): Add ability to define a custom service name when installing as a Windows service.
- [#4703](https://github.com/influxdata/telegraf/pull/4703): Add support for IPv6 in the ping plugin.
- [#4781](https://github.com/influxdata/telegraf/pull/4781): Add new config for csv column explicit type conversion.
- [#4800](https://github.com/influxdata/telegraf/pull/4800): Add an option to specify a custom datadog URL.
- [#4803](https://github.com/influxdata/telegraf/pull/4803): Use non-allocating field and tag accessors in datadog output.
- [#4752](https://github.com/influxdata/telegraf/pull/4752): Add per-directory file counts in the filecount input.
- [#4811](https://github.com/influxdata/telegraf/pull/4811): Add windows service name lookup to procstat input.
- [#4807](https://github.com/influxdata/telegraf/pull/4807): Add entity-body compression to http output.
- [#4838](https://github.com/influxdata/telegraf/pull/4838): Add telegraf version to User-Agent header.
- [#4864](https://github.com/influxdata/telegraf/pull/4864): Use DescribeStreamSummary in place of ListStreams in kinesis output.
- [#4852](https://github.com/influxdata/telegraf/pull/4852): Add ability to specify bytes options as strings with units.
- [#3903](https://github.com/influxdata/telegraf/pull/3903): Add support for TLS configuration in NSQ input.
- [#4914](https://github.com/influxdata/telegraf/pull/4914): Collect additional stats in memcached input.
- [#3847](https://github.com/influxdata/telegraf/pull/3847): Add wireless input plugin.
- [#4934](https://github.com/influxdata/telegraf/pull/4934): Add LUN to datasource translation in vsphere input.
- [#4798](https://github.com/influxdata/telegraf/pull/4798): Allow connecting to prometheus via unix socket.
- [#4920](https://github.com/influxdata/telegraf/pull/4920): Add scraping for Prometheus endpoint in Kubernetes.
- [#4938](https://github.com/influxdata/telegraf/pull/4938): Add per output flush_interval, metric_buffer_limit and metric_batch_size.

### Bug Fixes

- [#4950](https://github.com/influxdata/telegraf/pull/4950): Remove the time_key from the field values in JSON parser.
- [#3968](https://github.com/influxdata/telegraf/issues/3968): Fix input time rounding when using a custom interval.
- [#4938](https://github.com/influxdata/telegraf/pull/4938): Fix potential deadlock or leaked resources on restart/reload.
- [#2919](https://github.com/influxdata/telegraf/pull/2919): Fix outputs block inputs when batch size is reached.
- [#4789](https://github.com/influxdata/telegraf/issues/4789): Fix potential missing datastore metrics in vSphere plugin.
- [#4982](https://github.com/influxdata/telegraf/issues/4982): Log warning when wireless plugin is used on unsupported platform.
- [#4965](https://github.com/influxdata/telegraf/issues/4965): Handle non-tls columns for mysql input.
- [#4983](https://github.com/influxdata/telegraf/issues/4983): Fix panic in influxdb_listener when using gzip encoding.

## v1.8.3 [2018-10-30]

### Bug Fixes

- [#4873](https://github.com/influxdata/telegraf/pull/4873): Add DN attributes as tags in x509_cert input to avoid series overwrite.
- [#4921](https://github.com/influxdata/telegraf/issues/4921): Prevent connection leak by closing unused connections in amqp output.
- [#4904](https://github.com/influxdata/telegraf/issues/4904): Use default partition key when tag does not exist in kinesis output.
- [#4901](https://github.com/influxdata/telegraf/pull/4901): Log the correct error in jti_openconfig.
- [#4937](https://github.com/influxdata/telegraf/pull/4937): Handle panic when ipmi_sensor input gets bad input.
- [#4930](https://github.com/influxdata/telegraf/pull/4930): Don't add unserializable fields to jolokia2 input.
- [#4866](https://github.com/influxdata/telegraf/pull/4866): Fix version check in postgresql_extensible.

## v1.8.2 [2018-10-17]

### Bug Fixes

- [#4844](https://github.com/influxdata/telegraf/pull/4844): Update write path to match updated InfluxDB v2 API.
- [#4840](https://github.com/influxdata/telegraf/pull/4840): Fix missing timeouts in vsphere input.
- [#4851](https://github.com/influxdata/telegraf/pull/4851): Support uint fields in aerospike input.
- [#4854](https://github.com/influxdata/telegraf/pull/4854): Use container name from list if no name in container stats.
- [#4850](https://github.com/influxdata/telegraf/pull/4850): Prevent panic in filecount input on error in file stat.
- [#4846](https://github.com/influxdata/telegraf/pull/4846): Fix mqtt_consumer connect and reconnect.
- [#4849](https://github.com/influxdata/telegraf/pull/4849): Fix panic in logparser input.
- [#4869](https://github.com/influxdata/telegraf/pull/4869): Lower authorization errors to debug level in mongodb input.
- [#4875](https://github.com/influxdata/telegraf/pull/4875): Return correct response code on ping input.
- [#4874](https://github.com/influxdata/telegraf/pull/4874): Fix segfault in x509_cert input.

## v1.8.1 [2018-10-03]

### Bug Fixes

- [#4750](https://github.com/influxdata/telegraf/pull/4750): Fix hardware_type may be truncated in sqlserver input.
- [#4723](https://github.com/influxdata/telegraf/issues/4723): Improve performance in basicstats aggregator.
- [#4747](https://github.com/influxdata/telegraf/pull/4747): Add hostname to TLS config for SNI support.
- [#4675](https://github.com/influxdata/telegraf/issues/4675): Don't add tags with empty values to opentsdb output.
- [#4765](https://github.com/influxdata/telegraf/pull/4765): Fix panic during network error in vsphere input.
- [#4766](https://github.com/influxdata/telegraf/pull/4766): Unify http_listener error response with InfluxDB.
- [#4769](https://github.com/influxdata/telegraf/pull/4769): Add UUID to VMs in vSphere input.
- [#4758](https://github.com/influxdata/telegraf/issues/4758): Skip tags with empty values in cloudwatch output.
- [#4783](https://github.com/influxdata/telegraf/issues/4783): Fix missing non-realtime samples in vSphere input.
- [#4799](https://github.com/influxdata/telegraf/pull/4799): Fix case of timezone/grok_timezone options.

## v1.8 [2018-09-21]

### New Inputs

- [activemq](./plugins/inputs/activemq/README.md) - Contributed by @mlabouardy
- [beanstalkd](./plugins/inputs/beanstalkd/README.md) - Contributed by @44px
- [filecount](./plugins/inputs/filecount/README.md) - Contributed by @sometimesfood
- [file](./plugins/inputs/file/README.md) - Contributed by @maxunt
- [icinga2](./plugins/inputs/icinga2/README.md) - Contributed by @mlabouardy
- [kibana](./plugins/inputs/kibana/README.md) - Contributed by @lpic10
- [pgbouncer](./plugins/inputs/pgbouncer/README.md) - Contributed by @nerzhul
- [temp](./plugins/inputs/temp/README.md) - Contributed by @pytimer
- [tengine](./plugins/inputs/tengine/README.md) - Contributed by @ertaoxu
- [vsphere](./plugins/inputs/vsphere/README.md) - Contributed by @prydin
- [x509_cert](./plugins/inputs/x509_cert/README.md) - Contributed by @jtyr

### New Processors

- [enum](./plugins/processors/enum/README.md) - Contributed by @KarstenSchnitter
- [parser](./plugins/processors/parser/README.md) - Contributed by @Ayrdrie & @maxunt
- [rename](./plugins/processors/rename/README.md) - Contributed by @goldibex
- [strings](./plugins/processors/strings/README.md) - Contributed by @bsmaldon

### New Aggregators

- [valuecounter](./plugins/aggregators/valuecounter/README.md) - Contributed by @piotr1212

### New Outputs

- [azure_monitor](./plugins/outputs/azure_monitor/README.md) - Contributed by @influxdata
- [influxdb_v2](./plugins/outputs/influxdb_v2/README.md) - Contributed by @influxdata

### New Parsers

- [csv](/plugins/parsers/csv/README.md) - Contributed by @maxunt
- [grok](/plugins/parsers/grok/README.md) - Contributed by @maxunt
- [logfmt](/plugins/parsers/logfmt/README.md) - Contributed by @Ayrdrie & @maxunt
- [wavefront](/plugins/parsers/wavefront/README.md) - Contributed by @puckpuck

### New Serializers

- [splunkmetric](/plugins/serializers/splunkmetric/README.md) - Contributed by @ronnocol

### Features

- [#4236](https://github.com/influxdata/telegraf/pull/4236): Add SSL/TLS support to redis input.
- [#4160](https://github.com/influxdata/telegraf/pull/4160): Add tengine input plugin.
- [#4262](https://github.com/influxdata/telegraf/pull/4262): Add power draw field to nvidia_smi plugin.
- [#4271](https://github.com/influxdata/telegraf/pull/4271): Add support for solr 7 to the solr input.
- [#4281](https://github.com/influxdata/telegraf/pull/4281): Add owner tag on partitions in burrow input.
- [#4259](https://github.com/influxdata/telegraf/pull/4259): Add container status tag to docker input.
- [#3523](https://github.com/influxdata/telegraf/pull/3523): Add valuecounter aggregator plugin.
- [#4307](https://github.com/influxdata/telegraf/pull/4307): Add new measurement with results of pgrep lookup to procstat input.
- [#4311](https://github.com/influxdata/telegraf/pull/4311): Add support for comma in logparser timestamp format.
- [#4292](https://github.com/influxdata/telegraf/pull/4292): Add path tag to tail input plugin.
- [#4322](https://github.com/influxdata/telegraf/pull/4322): Add log message when tail is added or removed from a file.
- [#4267](https://github.com/influxdata/telegraf/pull/4267): Add option to use of counter time in win perf counters.
- [#4343](https://github.com/influxdata/telegraf/pull/4343): Add energy and power field and device id tag to fibaro input.
- [#4347](https://github.com/influxdata/telegraf/pull/4347): Add http path configuration for OpenTSDB output.
- [#4352](https://github.com/influxdata/telegraf/pull/4352): Gather IPMI metrics concurrently.
- [#4362](https://github.com/influxdata/telegraf/pull/4362): Add mongo document and connection metrics.
- [#3772](https://github.com/influxdata/telegraf/pull/3772): Add enum processor plugin.
- [#4386](https://github.com/influxdata/telegraf/pull/4386): Add user tag to procstat input.
- [#4403](https://github.com/influxdata/telegraf/pull/4403): Add support for multivalue metrics to collectd parser.
- [#4418](https://github.com/influxdata/telegraf/pull/4418): Add support for setting kafka client id.
- [#4332](https://github.com/influxdata/telegraf/pull/4332): Add file input plugin and grok parser.
- [#4320](https://github.com/influxdata/telegraf/pull/4320): Improve cloudwatch output performance.
- [#3768](https://github.com/influxdata/telegraf/pull/3768): Add x509_cert input plugin.
- [#4471](https://github.com/influxdata/telegraf/pull/4471): Add IPSIpAddress syntax to ipaddr conversion in snmp plugin.
- [#4363](https://github.com/influxdata/telegraf/pull/4363): Add filecount input plugin.
- [#4485](https://github.com/influxdata/telegraf/pull/4485): Add support for configuring an AWS endpoint_url.
- [#4491](https://github.com/influxdata/telegraf/pull/4491): Send all messages before waiting for results in kafka output.
- [#4492](https://github.com/influxdata/telegraf/pull/4492): Add support for lz4 compression to kafka output.
- [#4450](https://github.com/influxdata/telegraf/pull/4450): Split multiple sensor keys in ipmi input.
- [#4364](https://github.com/influxdata/telegraf/pull/4364): Support StatisticValues in cloudwatch output plugin.
- [#4431](https://github.com/influxdata/telegraf/pull/4431): Add ip restriction for the prometheus_client output.
- [#3918](https://github.com/influxdata/telegraf/pull/3918): Add pgbouncer input plugin.
- [#2689](https://github.com/influxdata/telegraf/pull/2689): Add ActiveMQ input plugin.
- [#4402](https://github.com/influxdata/telegraf/pull/4402): Add wavefront parser plugin.
- [#4528](https://github.com/influxdata/telegraf/pull/4528): Add rename processor plugin.
- [#4537](https://github.com/influxdata/telegraf/pull/4537): Add message 'max_bytes' configuration to kafka input.
- [#4546](https://github.com/influxdata/telegraf/pull/4546): Add gopsutil meminfo fields to mem plugin.
- [#4285](https://github.com/influxdata/telegraf/pull/4285): Document how to parse telegraf logs.
- [#4542](https://github.com/influxdata/telegraf/pull/4542): Use dep v0.5.0.
- [#4433](https://github.com/influxdata/telegraf/pull/4433): Add ability to set measurement from matched text in grok parser.
- [#4565](https://github.com/influxdata/telegraf/pull/4465): Drop message batches in kafka output if too large.
- [#4579](https://github.com/influxdata/telegraf/pull/4579): Add support for static and random routing keys in kafka output.
- [#4539](https://github.com/influxdata/telegraf/pull/4539): Add logfmt parser plugin.
- [#4551](https://github.com/influxdata/telegraf/pull/4551): Add parser processor plugin.
- [#4559](https://github.com/influxdata/telegraf/pull/4559): Add Icinga2 input plugin.
- [#4351](https://github.com/influxdata/telegraf/pull/4351): Add name, time, path and string field options to JSON parser.
- [#4571](https://github.com/influxdata/telegraf/pull/4571): Add forwarded records to sqlserver input.
- [#4585](https://github.com/influxdata/telegraf/pull/4585): Add Kibana input plugin.
- [#4439](https://github.com/influxdata/telegraf/pull/4439): Add csv parser plugin.
- [#4598](https://github.com/influxdata/telegraf/pull/4598): Add read_buffer_size option to statsd input.
- [#4089](https://github.com/influxdata/telegraf/pull/4089): Add azure_monitor output plugin.
- [#4628](https://github.com/influxdata/telegraf/pull/4628): Add queue_durability parameter to amqp_consumer input.
- [#4476](https://github.com/influxdata/telegraf/pull/4476): Add strings processor.
- [#4536](https://github.com/influxdata/telegraf/pull/4536): Add OAuth2 support to HTTP output plugin.
- [#4633](https://github.com/influxdata/telegraf/pull/4633): Add Unix epoch timestamp support for JSON parser.
- [#4657](https://github.com/influxdata/telegraf/pull/4657): Add options for basic auth to haproxy input.
- [#4411](https://github.com/influxdata/telegraf/pull/4411): Add temp input plugin.
- [#4272](https://github.com/influxdata/telegraf/pull/4272): Add Beanstalkd input plugin.
- [#4669](https://github.com/influxdata/telegraf/pull/4669): Add means to specify server password for redis input.
- [#4339](https://github.com/influxdata/telegraf/pull/4339): Add Splunk Metrics serializer.
- [#4141](https://github.com/influxdata/telegraf/pull/4141): Add input plugin for VMware vSphere.
- [#4667](https://github.com/influxdata/telegraf/pull/4667): Align metrics window to interval in cloudwatch input.
- [#4642](https://github.com/influxdata/telegraf/pull/4642): Improve Azure Managed Instance support + more in sqlserver input.
- [#4682](https://github.com/influxdata/telegraf/pull/4682): Allow alternate binaries for iptables input plugin.
- [#4645](https://github.com/influxdata/telegraf/pull/4645): Add influxdb_v2 output plugin.

### Bug Fixes

- [#3438](https://github.com/influxdata/telegraf/issues/3438): Fix divide by zero in logparser input.
- [#4499](https://github.com/influxdata/telegraf/issues/4499): Fix instance and object name in performance counters with backslashes.
- [#4646](https://github.com/influxdata/telegraf/issues/4646): Reset/flush saved contents from bad metric.
- [#4520](https://github.com/influxdata/telegraf/issues/4520): Document all supported cli arguments.
- [#4674](https://github.com/influxdata/telegraf/pull/4674): Log access denied opening a service at debug level in win_services.
- [#4588](https://github.com/influxdata/telegraf/issues/4588): Add support for Kafka 2.0.
- [#4087](https://github.com/influxdata/telegraf/issues/4087): Fix nagios parser does not support ranges in performance data.
- [#4088](https://github.com/influxdata/telegraf/issues/4088): Fix nagios parser does not strip quotes from performance data.
- [#4688](https://github.com/influxdata/telegraf/issues/4688): Fix null value crash in postgresql_extensible input.
- [#4681](https://github.com/influxdata/telegraf/pull/4681): Remove the startup authentication check from the cloudwatch output.
- [#4644](https://github.com/influxdata/telegraf/issues/4644): Support tailing files created after startup in tail input.
- [#4706](https://github.com/influxdata/telegraf/issues/4706): Fix csv format configuration loading.

## v1.7.4 [2018-08-29]

### Bug Fixes

- [#4534](https://github.com/influxdata/telegraf/pull/4534): Skip unserializable metric in influxDB UDP output.
- [#4554](https://github.com/influxdata/telegraf/pull/4554): Fix powerdns input tests.
- [#4584](https://github.com/influxdata/telegraf/pull/4584): Fix burrow_group offset calculation for burrow input.
- [#4550](https://github.com/influxdata/telegraf/pull/4550): Add result_code value for errors running ping command.
- [#4605](https://github.com/influxdata/telegraf/pull/4605): Remove timeout deadline for udp syslog input.
- [#4601](https://github.com/influxdata/telegraf/issues/4601): Ensure channel closed if an error occurs in cgroup input.
- [#4544](https://github.com/influxdata/telegraf/issues/4544): Fix sending of basic auth credentials in http output.
- [#4526](https://github.com/influxdata/telegraf/issues/4526): Use the correct GOARM value in the armel package.

## v1.7.3 [2018-08-07]

### Bug Fixes

- [#4434](https://github.com/influxdata/telegraf/issues/4434): Reduce required docker API version.
- [#4498](https://github.com/influxdata/telegraf/pull/4498): Keep leading whitespace for messages in syslog input.
- [#4470](https://github.com/influxdata/telegraf/issues/4470): Skip bad entries on interrupt input.
- [#4501](https://github.com/influxdata/telegraf/issues/4501): Preserve metric type when using filters in output plugins.
- [#3794](https://github.com/influxdata/telegraf/issues/3794): Fix error message if URL is unparseable in influxdb output.
- [#4059](https://github.com/influxdata/telegraf/issues/4059): Use explicit zpool properties to fix parse error on FreeBSD 11.2.
- [#4514](https://github.com/influxdata/telegraf/pull/4514): Lock buffer when adding metrics.

## v1.7.2 [2018-07-18]

### Bug Fixes

- [#4381](https://github.com/influxdata/telegraf/issues/4381): Use localhost as default server tag in zookeeper input.
- [#4374](https://github.com/influxdata/telegraf/issues/4374): Don't set values when pattern doesn't match in regex processor.
- [#4416](https://github.com/influxdata/telegraf/issues/4416): Fix output format of printer processor.
- [#4422](https://github.com/influxdata/telegraf/issues/4422): Fix metric can have duplicate field.
- [#4389](https://github.com/influxdata/telegraf/issues/4389): Return error if NewRequest fails in http output.
- [#4335](https://github.com/influxdata/telegraf/issues/4335): Reset read deadline for syslog input.
- [#4375](https://github.com/influxdata/telegraf/issues/4375): Exclude cached memory on docker input plugin.

## v1.7.1 [2018-07-03]

### Bug Fixes

- [#4277](https://github.com/influxdata/telegraf/pull/4277): Treat sigterm as a clean shutdown signal.
- [#4284](https://github.com/influxdata/telegraf/pull/4284): Fix selection of tags under nested objects in the JSON parser.
- [#4135](https://github.com/influxdata/telegraf/issues/4135): Fix postfix input handling multi-level queues.
- [#4334](https://github.com/influxdata/telegraf/pull/4334): Fix syslog timestamp parsing with single digit day of month.
- [#2910](https://github.com/influxdata/telegraf/issues/2910): Handle mysql input variations in the user_statistics collecting.
- [#4293](https://github.com/influxdata/telegraf/issues/4293): Fix minmax and basicstats aggregators to use uint64.
- [#4290](https://github.com/influxdata/telegraf/issues/4290): Document swap input plugin.
- [#4316](https://github.com/influxdata/telegraf/issues/4316): Fix incorrect precision being applied to metric in http_listener.

## v1.7 [2018-06-12]

### Release Notes

- The `cassandra` input plugin has been deprecated in favor of the `jolokia2`
  input plugin which is much more configurable and more performant.  There is
  an [example configuration](./plugins/inputs/jolokia2/examples) to help you
  get started.

- For plugins supporting TLS, you can now specify the certificate and keys
  using `tls_ca`, `tls_cert`, `tls_key`.  These options behave the same as
  the, now deprecated, `ssl` forms.

### New Inputs

- [aurora](./plugins/inputs/aurora/README.md) - Contributed by @influxdata
- [burrow](./plugins/inputs/burrow/README.md) - Contributed by @arkady-emelyanov
- [fibaro](./plugins/inputs/fibaro/README.md) - Contributed by @dynek
- [jti_openconfig_telemetry](./plugins/inputs/jti_openconfig_telemetry/README.md) - Contributed by @ajhai
- [mcrouter](./plugins/inputs/mcrouter/README.md) - Contributed by @cthayer
- [nvidia_smi](./plugins/inputs/nvidia_smi/README.md) - Contributed by @jackzampolin
- [syslog](./plugins/inputs/syslog/README.md) - Contributed by @influxdata

### New Processors

- [converter](./plugins/processors/converter/README.md) - Contributed by @influxdata
- [regex](./plugins/processors/regex/README.md) - Contributed by @44px
- [topk](./plugins/processors/topk/README.md) - Contributed by @mirath

### New Outputs

- [http](./plugins/outputs/http/README.md) - Contributed by @Dark0096
- [application_insights](./plugins/outputs/application_insights/README.md): Contribute by @karolz-ms

### Features

- [#3964](https://github.com/influxdata/telegraf/pull/3964): Add repl_oplog_window_sec metric to mongodb input.
- [#3819](https://github.com/influxdata/telegraf/pull/3819): Add per-host shard metrics in mongodb input.
- [#3999](https://github.com/influxdata/telegraf/pull/3999): Skip files with leading `..` in config directory.
- [#4021](https://github.com/influxdata/telegraf/pull/4021): Add TLS support to socket_writer and socket_listener plugins.
- [#4025](https://github.com/influxdata/telegraf/pull/4025): Add snmp input option to strip non fixed length index suffixes.
- [#4035](https://github.com/influxdata/telegraf/pull/4035): Add server version tag to docker input.
- [#4044](https://github.com/influxdata/telegraf/pull/4044): Add support for LeoFS 1.4 to leofs input.
- [#4068](https://github.com/influxdata/telegraf/pull/4068): Add parameter to force the interval of gather for sysstat.
- [#3877](https://github.com/influxdata/telegraf/pull/3877): Support busybox ping in the ping input.
- [#4077](https://github.com/influxdata/telegraf/pull/4077): Add input plugin for McRouter.
- [#4096](https://github.com/influxdata/telegraf/pull/4096): Add topk processor plugin.
- [#4114](https://github.com/influxdata/telegraf/pull/4114): Add cursor metrics to mongodb input.
- [#3455](https://github.com/influxdata/telegraf/pull/3455): Add tag/integer pair for result to net_response.
- [#4010](https://github.com/influxdata/telegraf/pull/3455): Add application_insights output plugin.
- [#4167](https://github.com/influxdata/telegraf/pull/4167): Added several important elasticsearch cluster health metrics.
- [#4094](https://github.com/influxdata/telegraf/pull/4094): Add batch mode to mqtt output.
- [#4158](https://github.com/influxdata/telegraf/pull/4158): Add aurora input plugin.
- [#3839](https://github.com/influxdata/telegraf/pull/3839): Add regex processor plugin.
- [#4165](https://github.com/influxdata/telegraf/pull/4165): Add support for Graphite 1.1 tags.
- [#4162](https://github.com/influxdata/telegraf/pull/4162): Add timeout option to sensors input.
- [#3489](https://github.com/influxdata/telegraf/pull/3489): Add burrow input plugin.
- [#3969](https://github.com/influxdata/telegraf/pull/3969): Add option to unbound module to use threads as tags.
- [#4183](https://github.com/influxdata/telegraf/pull/4183): Add support for TLS and username/password auth to aerospike input.
- [#4190](https://github.com/influxdata/telegraf/pull/4190): Add special syslog timestamp parser to grok parser that uses current year.
- [#4181](https://github.com/influxdata/telegraf/pull/4181): Add syslog input plugin.
- [#4212](https://github.com/influxdata/telegraf/pull/4212): Print the enabled aggregator and processor plugins on startup.
- [#3994](https://github.com/influxdata/telegraf/pull/3994): Add static routing_key option to amqp output.
- [#3995](https://github.com/influxdata/telegraf/pull/3995): Add passive mode exchange declaration option to amqp consumer input.
- [#4216](https://github.com/influxdata/telegraf/pull/4216): Add counter fields to pf input.

### Bug Fixes

- [#4018](https://github.com/influxdata/telegraf/pull/4018): Write to working file outputs if any files are not writeable.
- [#4036](https://github.com/influxdata/telegraf/pull/4036): Add all win_perf_counters fields for a series in a single metric.
- [#4118](https://github.com/influxdata/telegraf/pull/4118): Report results of dns_query instead of 0ms on timeout.
- [#4155](https://github.com/influxdata/telegraf/pull/4155): Add consul service tags to metric.
- [#2879](https://github.com/influxdata/telegraf/issues/2879): Fix wildcards and multi instance processes in win_perf_counters.
- [#2468](https://github.com/influxdata/telegraf/issues/2468): Fix crash on 32-bit Windows in win_perf_counters.
- [#4198](https://github.com/influxdata/telegraf/issues/4198): Fix win_perf_counters not collecting at every interval.
- [#4227](https://github.com/influxdata/telegraf/issues/4227): Use same flags for all BSD family ping variants.
- [#4266](https://github.com/influxdata/telegraf/issues/4266): Remove tags with empty values from Wavefront output.

## v1.6.4 [2018-06-05]

### Bug Fixes

- [#4203](https://github.com/influxdata/telegraf/issues/4203): Fix snmp overriding of auto-configured table fields.
- [#4218](https://github.com/influxdata/telegraf/issues/4218): Fix uint support in cloudwatch output.
- [#4188](https://github.com/influxdata/telegraf/pull/4188): Fix documentation of instance_name option in varnish input.
- [#4195](https://github.com/influxdata/telegraf/pull/4195): Revert to previous aerospike library version due to memory leak.

## v1.6.3 [2018-05-21]

### Bug Fixes

- [#4127](https://github.com/influxdata/telegraf/issues/4127): Fix intermittent panic in aerospike input.
- [#4130](https://github.com/influxdata/telegraf/issues/4130): Fix connection leak in jolokia2_agent.
- [#4136](https://github.com/influxdata/telegraf/pull/4130): Fix jolokia2 timeout parsing.
- [#4142](https://github.com/influxdata/telegraf/pull/4142): Fix error parsing dropwizard metrics.
- [#4149](https://github.com/influxdata/telegraf/issues/4149): Fix librato output support for uint and bool.
- [#4176](https://github.com/influxdata/telegraf/pull/4176): Fix waitgroup deadlock if url is incorrect in apache input.

## v1.6.2 [2018-05-08]

### Bug Fixes

- [#4078](https://github.com/influxdata/telegraf/pull/4078): Use same timestamp for fields in system input.
- [#4091](https://github.com/influxdata/telegraf/pull/4091): Fix handling of uint64 in datadog output.
- [#4099](https://github.com/influxdata/telegraf/pull/4099): Ignore UTF8 BOM in JSON parser.
- [#4104](https://github.com/influxdata/telegraf/issues/4104): Fix case for slave metrics in mysql input.
- [#4110](https://github.com/influxdata/telegraf/issues/4110): Fix uint support in cratedb output.

## v1.6.1 [2018-04-23]

### Bug Fixes

- [#3835](https://github.com/influxdata/telegraf/issues/3835): Report mem input fields as gauges instead counters.
- [#4030](https://github.com/influxdata/telegraf/issues/4030): Fix graphite outputs unsigned integers in wrong format.
- [#4043](https://github.com/influxdata/telegraf/issues/4043): Report available fields if utmp is unreadable.
- [#4039](https://github.com/influxdata/telegraf/issues/4039): Fix potential "no fields" error writing to outputs.
- [#4037](https://github.com/influxdata/telegraf/issues/4037): Fix uptime reporting in system input when ran inside docker.
- [#3750](https://github.com/influxdata/telegraf/issues/3750): Fix mem input "cannot allocate memory" error on FreeBSD based systems.
- [#4056](https://github.com/influxdata/telegraf/pull/4056): Fix duplicate tags when overriding an existing tag.
- [#4062](https://github.com/influxdata/telegraf/pull/4062): Add server argument as first argument in unbound input.
- [#4063](https://github.com/influxdata/telegraf/issues/4063): Fix handling of floats with multiple leading zeroes.
- [#4064](https://github.com/influxdata/telegraf/issues/4064): Return errors in mongodb SSL/TLS configuration.

## v1.6 [2018-04-16]

### Release Notes

- The `mysql` input plugin has been updated fix a number of type conversion
  issues.  This may cause a `field type error` when inserting into InfluxDB due
  the change of types.

  To address this we have introduced a new `metric_version` option to control
  enabling the new format.  For in depth recommendations on upgrading please
  reference the [mysql plugin documentation](./plugins/inputs/mysql/README.md#metric-version).

  It is encouraged to migrate to the new model when possible as the old version
  is deprecated and will be removed in a future version.

- The `postgresql` plugins now defaults to using a persistent connection to the database.
  In environments where TCP connections are terminated the `max_lifetime`
  setting should be set less than the collection `interval` to prevent errors.

- The `sqlserver` input plugin has a new query and data model that can be enabled
  by setting `query_version = 2`.  It is encouraged to migrate to the new
  model when possible as the old version is deprecated and will be removed in
  a future version.

- An option has been added to the `openldap` input plugin that reverses metric
  name to improve grouping.  This change is enabled when `reverse_metric_names = true`
  is set.  It is encouraged to enable this option when possible as the old
  ordering is deprecated.

- The new `http` input configured with `data_format = "json"` can perform the
  same task as the, now deprecated, `httpjson` input.

### New Inputs

- [http](./plugins/inputs/http/README.md) - Thanks to @grange74
- [ipset](./plugins/inputs/ipset/README.md) - Thanks to @sajoupa
- [nats](./plugins/inputs/nats/README.md) - Thanks to @mjs & @levex

### New Processors

- [override](./plugins/processors/override/README.md) - Thanks to @KarstenSchnitter

### New Parsers

- [dropwizard](./docs/DATA_FORMATS_INPUT.md#dropwizard) - Thanks to @atzoum

### Features

- [#3551](https://github.com/influxdata/telegraf/pull/3551): Add health status mapping from string to int in elasticsearch input.
- [#3580](https://github.com/influxdata/telegraf/pull/3580): Add control over which stats to gather in basicstats aggregator.
- [#3596](https://github.com/influxdata/telegraf/pull/3596): Add messages_delivered_get to rabbitmq input.
- [#3632](https://github.com/influxdata/telegraf/pull/3632): Add wired field to mem input.
- [#3619](https://github.com/influxdata/telegraf/pull/3619): Add support for gathering exchange metrics to the rabbitmq input.
- [#3565](https://github.com/influxdata/telegraf/pull/3565): Add support for additional metrics on Linux in zfs input.
- [#3524](https://github.com/influxdata/telegraf/pull/3524): Add available_entropy field to kernel input plugin.
- [#3643](https://github.com/influxdata/telegraf/pull/3643): Add user privilege level setting to IPMI sensors.
- [#2701](https://github.com/influxdata/telegraf/pull/2701): Use persistent connection to postgresql database.
- [#2846](https://github.com/influxdata/telegraf/pull/2846): Add support for dropwizard input format.
- [#3666](https://github.com/influxdata/telegraf/pull/3666): Add container health metrics to docker input.
- [#3687](https://github.com/influxdata/telegraf/pull/3687): Add support for using globs in devices list of diskio input plugin.
- [#2754](https://github.com/influxdata/telegraf/pull/2754): Allow running as console application on Windows.
- [#3703](https://github.com/influxdata/telegraf/pull/3703): Add listener counts and node running status to rabbitmq input.
- [#3674](https://github.com/influxdata/telegraf/pull/3674): Add NATS Monitoring Input Plugin.
- [#3702](https://github.com/influxdata/telegraf/pull/3702): Add ability to select which queues will be gathered in rabbitmq input.
- [#3726](https://github.com/influxdata/telegraf/pull/3726): Add support for setting bsd source address to the ping input.
- [#3346](https://github.com/influxdata/telegraf/pull/3346): Add Ipset input plugin.
- [#3719](https://github.com/influxdata/telegraf/pull/3719): Add TLS and HTTP basic auth to prometheus_client output.
- [#3618](https://github.com/influxdata/telegraf/pull/3618): Add new sqlserver output data model.
- [#3559](https://github.com/influxdata/telegraf/pull/3559): Add native Go method for finding pids to procstat.
- [#3722](https://github.com/influxdata/telegraf/pull/3722): Add additional metrics and reverse metric names option to openldap.
- [#3769](https://github.com/influxdata/telegraf/pull/3769): Add TLS support to the mesos input plugin.
- [#3546](https://github.com/influxdata/telegraf/pull/3546): Add http input plugin.
- [#3781](https://github.com/influxdata/telegraf/pull/3781): Add keep alive support to the TCP mode of statsd.
- [#3783](https://github.com/influxdata/telegraf/pull/3783): Support deadline in ping plugin.
- [#3765](https://github.com/influxdata/telegraf/pull/3765): Add option to disable labels in prometheus output for string fields.
- [#3808](https://github.com/influxdata/telegraf/pull/3808): Add shard server stats to the mongodb input plugin.
- [#3713](https://github.com/influxdata/telegraf/pull/3713): Add server option to unbound plugin.
- [#3804](https://github.com/influxdata/telegraf/pull/3804): Convert boolean metric values to float in datadog output.
- [#3799](https://github.com/influxdata/telegraf/pull/3799): Add Solr 3 compatibility.
- [#3797](https://github.com/influxdata/telegraf/pull/3797): Add sum stat to basicstats aggregator.
- [#3626](https://github.com/influxdata/telegraf/pull/3626): Add ability to override proxy from environment in http response.
- [#3853](https://github.com/influxdata/telegraf/pull/3853): Add host to ping timeout log message.
- [#3773](https://github.com/influxdata/telegraf/pull/3773): Add override processor.
- [#3814](https://github.com/influxdata/telegraf/pull/3814): Add status_code and result tags and result_type field to http_response input.
- [#3880](https://github.com/influxdata/telegraf/pull/3880): Added config flag to skip collection of network protocol metrics.
- [#3927](https://github.com/influxdata/telegraf/pull/3927): Add TLS support to kapacitor input.
- [#3496](https://github.com/influxdata/telegraf/pull/3496): Add HTTP basic auth support to the http_listener input.
- [#3452](https://github.com/influxdata/telegraf/issues/3452): Tags in output InfluxDB Line Protocol are now sorted.
- [#3631](https://github.com/influxdata/telegraf/issues/3631): InfluxDB Line Protocol parser now accepts DOS line endings.
- [#2496](https://github.com/influxdata/telegraf/issues/2496): An option has been added to skip database creation in the InfluxDB output.
- [#3366](https://github.com/influxdata/telegraf/issues/3366): Add support for connecting to InfluxDB over a unix domain socket.
- [#3946](https://github.com/influxdata/telegraf/pull/3946): Add optional unsigned integer support to the influx data format.
- [#3811](https://github.com/influxdata/telegraf/pull/3811): Add TLS support to zookeeper input.
- [#2737](https://github.com/influxdata/telegraf/issues/2737): Add filters for container state to docker input.

### Bug Fixes

- [#1896](https://github.com/influxdata/telegraf/issues/1896): Fix various mysql data type conversions.
- [#3810](https://github.com/influxdata/telegraf/issues/3810): Fix metric buffer limit in internal plugin after reload.
- [#3801](https://github.com/influxdata/telegraf/issues/3801): Fix panic in http_response on invalid regex.
- [#3973](https://github.com/influxdata/telegraf/issues/3873): Fix socket_listener setting ReadBufferSize on tcp sockets.
- [#1575](https://github.com/influxdata/telegraf/issues/1575): Add tag for target url to phpfpm input.
- [#3868](https://github.com/influxdata/telegraf/issues/3868): Fix cannot unmarshal object error in DC/OS input.
- [#3648](https://github.com/influxdata/telegraf/issues/3648): Fix InfluxDB output not able to reconnect when server address changes.
- [#3957](https://github.com/influxdata/telegraf/issues/3957): Fix parsing of dos line endings in the smart input.
- [#3754](https://github.com/influxdata/telegraf/issues/3754): Fix precision truncation when no timestamp included.
- [#3655](https://github.com/influxdata/telegraf/issues/3655): Fix SNMPv3 connection with Cisco ASA 5515 in snmp input.
- [#3981](https://github.com/influxdata/telegraf/pull/3981): Export all vars defined in /etc/default/telegraf.
- [#4004](https://github.com/influxdata/telegraf/issues/4004): Allow grok pattern to contain newlines.

## v1.5.3 [2018-03-14]

### Bug Fixes

- [#3729](https://github.com/influxdata/telegraf/issues/3729): Set path to / if HOST_MOUNT_PREFIX matches full path.
- [#3739](https://github.com/influxdata/telegraf/issues/3739): Remove userinfo from url tag in prometheus input.
- [#3778](https://github.com/influxdata/telegraf/issues/3778): Fix ping plugin not reporting zero durations.
- [#3697](https://github.com/influxdata/telegraf/issues/3697): Disable keepalive in mqtt output to prevent deadlock.
- [#3786](https://github.com/influxdata/telegraf/pull/3786): Fix collation difference in sqlserver input.
- [#3871](https://github.com/influxdata/telegraf/pull/3871): Fix uptime metric in passenger input plugin.
- [#3851](https://github.com/influxdata/telegraf/issues/3851): Add output of stderr in case of error to exec log message.

## v1.5.2 [2018-01-30]

### Bug Fixes

- [#3684](https://github.com/influxdata/telegraf/pull/3684): Ignore empty lines in Graphite plaintext.
- [#3604](https://github.com/influxdata/telegraf/issues/3604): Fix index out of bounds error in solr input plugin.
- [#3680](https://github.com/influxdata/telegraf/pull/3680): Reconnect before sending graphite metrics if disconnected.
- [#3693](https://github.com/influxdata/telegraf/pull/3693): Align aggregator period with internal ticker to avoid skipping metrics.
- [#3629](https://github.com/influxdata/telegraf/issues/3629): Fix a potential deadlock when using aggregators.
- [#3697](https://github.com/influxdata/telegraf/issues/3697): Limit wait time for writes in mqtt output.
- [#3698](https://github.com/influxdata/telegraf/issues/3698): Revert change in graphite output where dot in field key was replaced by underscore.
- [#3710](https://github.com/influxdata/telegraf/issues/3710): Add timeout to wavefront output write.
- [#3725](https://github.com/influxdata/telegraf/issues/3725): Exclude master_replid fields from redis input.

## v1.5.1 [2018-01-10]

### Bug Fixes

- [#3624](https://github.com/influxdata/telegraf/pull/3624): Fix name error in jolokia2_agent sample config.
- [#3625](https://github.com/influxdata/telegraf/pull/3625): Fix DC/OS login expiration time.
- [#3593](https://github.com/influxdata/telegraf/pull/3593): Set Content-Type charset in influxdb output and allow it be overridden.
- [#3594](https://github.com/influxdata/telegraf/pull/3594): Document permissions setup for postfix input.
- [#3633](https://github.com/influxdata/telegraf/pull/3633): Fix deliver_get field in rabbitmq input.
- [#3607](https://github.com/influxdata/telegraf/issues/3607): Escape environment variables during config toml parsing.

## v1.5 [2017-12-14]

### New Plugins

- [basicstats](./plugins/aggregators/basicstats/README.md) - Thanks to @toni-moreno
- [bond](./plugins/inputs/bond/README.md) - Thanks to @ildarsv
- [cratedb](./plugins/outputs/cratedb/README.md) - Thanks to @felixge
- [dcos](./plugins/inputs/dcos/README.md) - Thanks to @influxdata
- [jolokia2](./plugins/inputs/jolokia2/README.md) - Thanks to @dylanmei
- [nginx_plus](./plugins/inputs/nginx_plus/README.md) - Thanks to @mplonka & @poblahblahblah
- [opensmtpd](./plugins/inputs/opensmtpd/README.md) - Thanks to @aromeyer
- [particle](./plugins/inputs/webhooks/particle/README.md) - Thanks to @davidgs
- [pf](./plugins/inputs/pf/README.md) - Thanks to @nferch
- [postfix](./plugins/inputs/postfix/README.md) - Thanks to @phemmer
- [smart](./plugins/inputs/smart/README.md) - Thanks to @rickard-von-essen
- [solr](./plugins/inputs/solr/README.md) - Thanks to @ljagiello
- [teamspeak](./plugins/inputs/teamspeak/README.md) - Thanks to @p4ddy1
- [unbound](./plugins/inputs/unbound/README.md) - Thanks to @aromeyer
- [wavefront](./plugins/outputs/wavefront/README.md) - Thanks to @puckpuck

### Release Notes

- In the `kinesis` output, use of the `partition_key` and
  `use_random_partitionkey` options has been deprecated in favor of the
  `partition` subtable.  This allows for more flexible methods to set the
  partition key such as by metric name or by tag.

- With the release of the new improved `jolokia2` input, the legacy `jolokia`
  plugin is deprecated and will be removed in a future release.  Users of this
  plugin are encouraged to update to the new `jolokia2` plugin.

- In the `postgresql` and `postgresql_extensible` plugins, the type of the oid
  data type has changed from string to integer.  It is recommended to drop
  affected fields until a new shard is started. For details on how to
  workaround this issue please see [#3622](https://github.com/influxdata/telegraf/issues/3622).

### Features

- [#3170](https://github.com/influxdata/telegraf/pull/3170): Add support for sharding based on metric name.
- [#3196](https://github.com/influxdata/telegraf/pull/3196): Add Kafka output plugin topic_suffix option.
- [#3027](https://github.com/influxdata/telegraf/pull/3027): Include mount mode option in disk metrics.
- [#3191](https://github.com/influxdata/telegraf/pull/3191): TLS and MTLS enhancements to HTTPListener input plugin.
- [#3213](https://github.com/influxdata/telegraf/pull/3213): Add polling method to logparser and tail inputs.
- [#3211](https://github.com/influxdata/telegraf/pull/3211): Add timeout option for kubernetes input.
- [#3234](https://github.com/influxdata/telegraf/pull/3234): Add support for timing sums in statsd input.
- [#2617](https://github.com/influxdata/telegraf/issues/2617): Add resource limit monitoring to procstat.
- [#3236](https://github.com/influxdata/telegraf/pull/3236): Add support for k8s service DNS discovery to prometheus input.
- [#3245](https://github.com/influxdata/telegraf/pull/3245): Add configurable metrics endpoint to prometheus output.
- [#3214](https://github.com/influxdata/telegraf/pull/3214): Add new nginx_plus input plugin.
- [#3215](https://github.com/influxdata/telegraf/pull/3215): Add support for NSQLookupd to nsq_consumer.
- [#2278](https://github.com/influxdata/telegraf/pull/2278): Add redesigned Jolokia input plugin.
- [#3106](https://github.com/influxdata/telegraf/pull/3106): Add configurable separator for metrics and fields in opentsdb output.
- [#1692](https://github.com/influxdata/telegraf/pull/1692): Add support for the rollbar occurrence webhook event.
- [#3160](https://github.com/influxdata/telegraf/pull/3160): Add Wavefront output plugin.
- [#3281](https://github.com/influxdata/telegraf/pull/3281): Add extra wired tiger cache metrics to mongodb input.
- [#3141](https://github.com/influxdata/telegraf/pull/3141): Collect Docker Swarm service metrics in docker input plugin.
- [#2449](https://github.com/influxdata/telegraf/pull/2449): Add smart input plugin for collecting S.M.A.R.T. data.
- [#3269](https://github.com/influxdata/telegraf/pull/3269): Add cluster health level configuration to elasticsearch input.
- [#3304](https://github.com/influxdata/telegraf/pull/3304): Add ability to limit node stats in elasticsearch input.
- [#2167](https://github.com/influxdata/telegraf/pull/2167): Add new basicstats aggregator.
- [#3344](https://github.com/influxdata/telegraf/pull/3344): Add UDP IPv6 support to statsd input.
- [#3350](https://github.com/influxdata/telegraf/pull/3350): Use labels in prometheus output for string fields.
- [#3358](https://github.com/influxdata/telegraf/pull/3358): Add support for decimal timestamps to ts-epoch modifier.
- [#3337](https://github.com/influxdata/telegraf/pull/3337): Add histogram and summary types and use in prometheus plugins.
- [#3365](https://github.com/influxdata/telegraf/pull/3365): Gather concurrently from snmp agents.
- [#3333](https://github.com/influxdata/telegraf/issues/3333): Perform DNS lookup before ping and report result.
- [#3398](https://github.com/influxdata/telegraf/issues/3398): Add instance name option to varnish plugin.
- [#3406](https://github.com/influxdata/telegraf/pull/3406):  Add support for SSL settings to ElasticSearch output plugin.
- [#3315](https://github.com/influxdata/telegraf/pull/3315): Add Teamspeak 3 input plugin.
- [#3305](https://github.com/influxdata/telegraf/pull/3305): Add modification_time field to filestat input plugin.
- [#2019](https://github.com/influxdata/telegraf/pull/2019): Add Solr input plugin.
- [#3210](https://github.com/influxdata/telegraf/pull/3210): Add CrateDB output plugin.
- [#3459](https://github.com/influxdata/telegraf/pull/3459): Add systemd unit pid and cgroup matching to procstat.
- [#3477](https://github.com/influxdata/telegraf/pull/3477): Add Particle Webhook Plugin.
- [#3471](https://github.com/influxdata/telegraf/pull/3471): Use MAX() instead of SUM() for latency measurements in sqlserver.
- [#3490](https://github.com/influxdata/telegraf/pull/3490): Add index by week number to Elasticsearch output.
- [#3434](https://github.com/influxdata/telegraf/pull/3434): Add unbound input plugin.
- [#3449](https://github.com/influxdata/telegraf/pull/3449): Add opensmtpd input plugin.
- [#3470](https://github.com/influxdata/telegraf/pull/3470): Add support for tags in the index name in elasticsearch output.
- [#2553](https://github.com/influxdata/telegraf/pull/2553): Add postfix input plugin.
- [#3424](https://github.com/influxdata/telegraf/pull/3424): Add bond input plugin.
- [#3518](https://github.com/influxdata/telegraf/pull/3518): Add slab to mem plugin.
- [#3519](https://github.com/influxdata/telegraf/pull/3519): Add input plugin for DC/OS.
- [#3140](https://github.com/influxdata/telegraf/pull/3140): Add support for glob patterns in net input plugin.
- [#3405](https://github.com/influxdata/telegraf/pull/3405): Add input plugin for OpenBSD/FreeBSD pf.
- [#3528](https://github.com/influxdata/telegraf/pull/3528): Add option to amqp output to publish persistent messages.
- [#3530](https://github.com/influxdata/telegraf/pull/3530): Support I (idle) process state on procfs+Linux.

### Bug Fixes

- [#3136](https://github.com/influxdata/telegraf/issues/3136): Fix webhooks input address in use during reload.
- [#3258](https://github.com/influxdata/telegraf/issues/3258): Unlock Statsd when stopping to prevent deadlock.
- [#3319](https://github.com/influxdata/telegraf/issues/3319): Fix cloudwatch output requires unneeded permissions.
- [#3351](https://github.com/influxdata/telegraf/issues/3351): Fix prometheus passthrough for existing value types.
- [#3430](https://github.com/influxdata/telegraf/issues/3430): Always ignore autofs filesystems in disk input.
- [#3326](https://github.com/influxdata/telegraf/issues/3326): Fail metrics parsing on unescaped quotes.
- [#3473](https://github.com/influxdata/telegraf/pull/3473): Whitelist allowed char classes for graphite output.
- [#3488](https://github.com/influxdata/telegraf/pull/3488): Use hexadecimal ids and lowercase names in zipkin input.
- [#3263](https://github.com/influxdata/telegraf/issues/3263): Fix snmp-tools output parsing with Windows EOLs.
- [#3447](https://github.com/influxdata/telegraf/issues/3447): Add shadow-utils dependency to rpm package.
- [#3448](https://github.com/influxdata/telegraf/issues/3448): Use deb-systemd-invoke to restart service.
- [#3553](https://github.com/influxdata/telegraf/issues/3553): Fix kafka_consumer outside range of offsets error.
- [#3568](https://github.com/influxdata/telegraf/issues/3568): Fix separation of multiple prometheus_client outputs.
- [#3577](https://github.com/influxdata/telegraf/issues/3577): Don't add system input uptime_format as a counter.

## v1.4.5 [2017-12-01]

### Bug Fixes

- [#3500](https://github.com/influxdata/telegraf/issues/3500): Fix global variable collection when using interval_slow option in mysql input.
- [#3486](https://github.com/influxdata/telegraf/issues/3486): Fix error getting net connections info in netstat input.
- [#3529](https://github.com/influxdata/telegraf/issues/3529): Fix HOST_MOUNT_PREFIX in docker with disk input.

## v1.4.4 [2017-11-08]

### Bug Fixes

- [#3401](https://github.com/influxdata/telegraf/pull/3401): Use schema specified in mqtt_consumer input.
- [#3419](https://github.com/influxdata/telegraf/issues/3419): Redact datadog API key in log output.
- [#3311](https://github.com/influxdata/telegraf/issues/3311): Fix error getting pids in netstat input.
- [#3339](https://github.com/influxdata/telegraf/issues/3339): Support HOST_VAR envvar to locate /var in system input.
- [#3383](https://github.com/influxdata/telegraf/issues/3383): Use current time if docker container read time is zero value.

## v1.4.3 [2017-10-25]

### Bug Fixes

- [#3327](https://github.com/influxdata/telegraf/issues/3327): Fix container name filters in docker input.
- [#3321](https://github.com/influxdata/telegraf/issues/3321): Fix snmpwalk address format in leofs input.
- [#3329](https://github.com/influxdata/telegraf/issues/3329): Fix case sensitivity issue in sqlserver query.
- [#3342](https://github.com/influxdata/telegraf/pull/3342): Fix CPU input plugin stuck after suspend on Linux.
- [#3013](https://github.com/influxdata/telegraf/issues/3013): Fix mongodb input panic when restarting mongodb.
- [#3224](https://github.com/influxdata/telegraf/pull/3224): Preserve url path prefix in influx output.
- [#3354](https://github.com/influxdata/telegraf/pull/3354): Fix TELEGRAF_OPTS expansion in systemd service unit.
- [#3357](https://github.com/influxdata/telegraf/issues/3357): Remove warning when JSON contains null value.
- [#3375](https://github.com/influxdata/telegraf/issues/3375): Fix ACL token usage in consul input plugin.
- [#3369](https://github.com/influxdata/telegraf/issues/3369): Fix unquoting error with Tomcat 6.
- [#3373](https://github.com/influxdata/telegraf/issues/3373): Fix syscall panic in diskio on some Linux systems.

## v1.4.2 [2017-10-10]

### Bug Fixes

- [#3259](https://github.com/influxdata/telegraf/issues/3259): Fix error if int larger than 32-bit in /proc/vmstat.
- [#3265](https://github.com/influxdata/telegraf/issues/3265): Fix parsing of JSON with a UTF8 BOM in httpjson.
- [#2887](https://github.com/influxdata/telegraf/issues/2887): Allow JSON data format to contain zero metrics.
- [#3284](https://github.com/influxdata/telegraf/issues/3284): Fix format of connection_timeout in mqtt_consumer.
- [#3081](https://github.com/influxdata/telegraf/issues/3081): Fix case sensitivity error in sqlserver input.
- [#3297](https://github.com/influxdata/telegraf/issues/3297): Add support for proxy environment variables to http_response.
- [#1588](https://github.com/influxdata/telegraf/issues/1588): Add support for standard proxy env vars in outputs.
- [#3282](https://github.com/influxdata/telegraf/issues/3282): Fix panic in cpu input if number of cpus changes.
- [#2854](https://github.com/influxdata/telegraf/issues/2854): Use chunked transfer encoding in InfluxDB output.

## v1.4.1 [2017-09-26]

### Bug Fixes

- [#3167](https://github.com/influxdata/telegraf/issues/3167): Fix MQTT input exits if Broker is not available on startup.
- [#3217](https://github.com/influxdata/telegraf/issues/3217): Fix optional field value conversions in fluentd input.
- [#3227](https://github.com/influxdata/telegraf/issues/3227): Whitelist allowed char classes for opentsdb output.
- [#3232](https://github.com/influxdata/telegraf/issues/3232): Fix counter and gauge metric types.
- [#3235](https://github.com/influxdata/telegraf/issues/3235): Fix skipped line with empty target in iptables.
- [#3175](https://github.com/influxdata/telegraf/issues/3175): Fix duplicate keys in perf counters sqlserver query.
- [#3230](https://github.com/influxdata/telegraf/issues/3230): Fix panic in statsd p100 calculation.
- [#3242](https://github.com/influxdata/telegraf/issues/3242): Fix arm64 packages contain 32-bit executable.

## v1.4 [2017-09-05]

### Release Notes

- The `kafka_consumer` input has been updated to support Kafka 0.9 and
  above style consumer offset handling.  The previous version of this plugin
  supporting Kafka 0.8 and below is available as the `kafka_consumer_legacy`
  plugin.

- In the `aerospike` input the `node_name` field has been changed to be a tag
  for both the `aerospike_node` and `aerospike_namespace` measurements.

- The default prometheus_client port has been changed to 9273.

### New Plugins

- [fail2ban](./plugins/inputs/fail2ban/README.md) - Thanks to @grugrut
- [fluentd](./plugins/inputs/fluentd/README.md) - Thanks to @DanKans
- [histogram](./plugins/aggregators/histogram/README.md) - Thanks to @vlamug
- [minecraft](./plugins/inputs/minecraft/README.md) - Thanks to @adamperlin & @Ayrdrie
- [openldap](./plugins/inputs/openldap/README.md) - Thanks to @cobaugh
- [salesforce](./plugins/inputs/salesforce/README.md) - Thanks to @rody
- [tomcat](./plugins/inputs/tomcat/README.md) - Thanks to @mlindes
- [win_services](./plugins/inputs/win_services/README.md) - Thanks to @vlastahajek
- [zipkin](./plugins/inputs/zipkin/README.md) - Thanks to @adamperlin & @Ayrdrie

### Features

- [#2487](https://github.com/influxdata/telegraf/pull/2487): Add Kafka 0.9+ consumer support
- [#2773](https://github.com/influxdata/telegraf/pull/2773): Add support for self-signed certs to InfluxDB input plugin
- [#2293](https://github.com/influxdata/telegraf/pull/2293): Add TCP listener for statsd input
- [#2581](https://github.com/influxdata/telegraf/pull/2581): Add Docker container environment variables as tags. Only whitelisted
- [#2817](https://github.com/influxdata/telegraf/pull/2817): Add timeout option to IPMI sensor plugin
- [#2883](https://github.com/influxdata/telegraf/pull/2883): Add support for an optional SSL/TLS configuration to nginx input plugin
- [#2882](https://github.com/influxdata/telegraf/pull/2882): Add timezone support for logparser timestamps.
- [#2814](https://github.com/influxdata/telegraf/pull/2814): Add result_type field for http_response input.
- [#2734](https://github.com/influxdata/telegraf/pull/2734): Add include/exclude filters for docker containers.
- [#2602](https://github.com/influxdata/telegraf/pull/2602): Add secure connection support to graphite output.
- [#2908](https://github.com/influxdata/telegraf/pull/2908): Add min/max response time on linux/darwin to ping.
- [#2929](https://github.com/influxdata/telegraf/pull/2929): Add HTTP Proxy support to influxdb output.
- [#2933](https://github.com/influxdata/telegraf/pull/2933): Add standard SSL options to mysql input.
- [#2875](https://github.com/influxdata/telegraf/pull/2875): Add input plugin for fail2ban.
- [#2924](https://github.com/influxdata/telegraf/pull/2924): Support HOST_PROC in processes and linux_sysctl_fs inputs.
- [#2960](https://github.com/influxdata/telegraf/pull/2960): Add Minecraft input plugin.
- [#2963](https://github.com/influxdata/telegraf/pull/2963): Add support for RethinkDB 1.0 handshake protocol.
- [#2943](https://github.com/influxdata/telegraf/pull/2943): Add optional usage_active and time_active CPU metrics.
- [#2973](https://github.com/influxdata/telegraf/pull/2973): Change default prometheus_client port.
- [#2661](https://github.com/influxdata/telegraf/pull/2661): Add fluentd input plugin.
- [#2990](https://github.com/influxdata/telegraf/pull/2990): Add result_type field to net_response input plugin.
- [#2571](https://github.com/influxdata/telegraf/pull/2571): Add read timeout to socket_listener
- [#2612](https://github.com/influxdata/telegraf/pull/2612): Add input plugin for OpenLDAP.
- [#3042](https://github.com/influxdata/telegraf/pull/3042): Add network option to dns_query.
- [#3054](https://github.com/influxdata/telegraf/pull/3054): Add redis_version field to redis input.
- [#3063](https://github.com/influxdata/telegraf/pull/3063): Add tls options to docker input.
- [#2387](https://github.com/influxdata/telegraf/pull/2387): Add histogram aggregator plugin.
- [#3080](https://github.com/influxdata/telegraf/pull/3080): Add zipkin input plugin.
- [#3023](https://github.com/influxdata/telegraf/pull/3023): Add Windows Services input plugin.
- [#3098](https://github.com/influxdata/telegraf/pull/3098): Add path tag to logparser containing path of logfile.
- [#3075](https://github.com/influxdata/telegraf/pull/3075): Add salesforce input plugin.
- [#3097](https://github.com/influxdata/telegraf/pull/3097): Add option to run varnish under sudo.
- [#3119](https://github.com/influxdata/telegraf/pull/3119): Add weighted_io_time to diskio input.
- [#2978](https://github.com/influxdata/telegraf/pull/2978): Add gzip content-encoding support to influxdb output.
- [#3127](https://github.com/influxdata/telegraf/pull/3127): Allow using system plugin in Windows.
- [#3112](https://github.com/influxdata/telegraf/pull/3112): Add tomcat input plugin.
- [#3182](https://github.com/influxdata/telegraf/pull/3182): HTTP headers can be added to InfluxDB output.

### Bug Fixes

- [#2607](https://github.com/influxdata/telegraf/issues/2607): Improve logging of errors in Cassandra input.
- [#2819](https://github.com/influxdata/telegraf/pull/2819): [enh] set db_version at 0 if query version fails
- [#2749](https://github.com/influxdata/telegraf/pull/2749): Fixed sqlserver input to work with case sensitive server collation.
- [#2716](https://github.com/influxdata/telegraf/pull/2716): Systemd does not see all shutdowns as failures
- [#2782](https://github.com/influxdata/telegraf/pull/2782): Reuse transports in input plugins
- [#2815](https://github.com/influxdata/telegraf/issues/2815): Inputs processes fails with "no such process".
- [#1137](https://github.com/influxdata/telegraf/issues/1137): Fix multiple plugin loading in win_perf_counters.
- [#2855](https://github.com/influxdata/telegraf/pull/2855):  MySQL input: log and continue on field parse error.
- [#2885](https://github.com/influxdata/telegraf/pull/2885): Fix timeout option in Windows ping input sample configuration.
- [#2911](https://github.com/influxdata/telegraf/issues/2911): Fix Kinesis output plugin in govcloud.
- [#2917](https://github.com/influxdata/telegraf/issues/2917): Fix Aerospike input adds all nodes to a single series.
- [#2452](https://github.com/influxdata/telegraf/pull/2452): Improve Prometheus Client output documentation.
- [#2984](https://github.com/influxdata/telegraf/pull/2984): Display error message if prometheus output fails to listen.
- [#2997](https://github.com/influxdata/telegraf/issues/2997): Fix elasticsearch output content type detection warning.
- [#2914](https://github.com/influxdata/telegraf/issues/2914): Prevent possible deadlock when using aggregators.
- [#2860](https://github.com/influxdata/telegraf/issues/2860): Fix combined tagdrop/tagpass filtering.
- [#3036](https://github.com/influxdata/telegraf/pull/3036): Fix filtering when both pass and drop match an item.
- [#2964](https://github.com/influxdata/telegraf/issues/2964): Only report cpu usage for online cpus in docker input.
- [#3050](https://github.com/influxdata/telegraf/pull/3050): Start first aggregator period at startup time.
- [#2906](https://github.com/influxdata/telegraf/issues/2906): Fix panic in logparser if file cannot be opened.
- [#2886](https://github.com/influxdata/telegraf/issues/2886): Default to localhost if zookeeper has no servers set.
- [#2457](https://github.com/influxdata/telegraf/issues/2457): Fix docker memory and cpu reporting in Windows.
- [#3058](https://github.com/influxdata/telegraf/issues/3058): Allow iptable entries with trailing text.
- [#1680](https://github.com/influxdata/telegraf/issues/1680): Sanitize password from couchbase metric.
- [#3104](https://github.com/influxdata/telegraf/issues/3104): Converge to typed value in prometheus output.
- [#2899](https://github.com/influxdata/telegraf/issues/2899): Skip compilation of logparser and tail on solaris.
- [#2951](https://github.com/influxdata/telegraf/issues/2951): Discard logging from tail library.
- [#3126](https://github.com/influxdata/telegraf/pull/3126): Remove log message on ping timeout.
- [#3144](https://github.com/influxdata/telegraf/issues/3144): Don't retry points beyond retention policy.
- [#3015](https://github.com/influxdata/telegraf/issues/3015): Don't start Telegraf on install in Amazon Linux.
- [#3153](https://github.com/influxdata/telegraf/issues/3053): Enable hddtemp input on all platforms.
- [#3142](https://github.com/influxdata/telegraf/issues/3142): Escape backslash within string fields.
- [#3162](https://github.com/influxdata/telegraf/issues/3162): Fix parsing of SHM remotes in ntpq input
- [#3149](https://github.com/influxdata/telegraf/issues/3149): Don't fail parsing zpool stats if pool health is UNAVAIL on FreeBSD.
- [#2672](https://github.com/influxdata/telegraf/issues/2672): Fix NSQ input plugin when used with version 1.0.0-compat.
- [#2523](https://github.com/influxdata/telegraf/issues/2523): Added CloudWatch metric constraint validation.
- [#3179](https://github.com/influxdata/telegraf/issues/3179): Skip non-numerical values in graphite format.
- [#3187](https://github.com/influxdata/telegraf/issues/3187): Fix panic when handling string fields with escapes.

## v1.3.5 [2017-07-26]

### Bug Fixes

- [#3049](https://github.com/influxdata/telegraf/issues/3049): Fix prometheus output cannot be reloaded.
- [#3037](https://github.com/influxdata/telegraf/issues/3037): Fix filestat reporting exists when cannot list directory.
- [#2386](https://github.com/influxdata/telegraf/issues/2386): Fix ntpq parse issue when using dns_lookup.
- [#2554](https://github.com/influxdata/telegraf/issues/2554): Fix panic when agent.interval = "0s".

## v1.3.4 [2017-07-12]

### Bug Fixes

- [#3001](https://github.com/influxdata/telegraf/issues/3001): Fix handling of escape characters within fields.
- [#2988](https://github.com/influxdata/telegraf/issues/2988): Fix chrony plugin does not track system time offset.
- [#3004](https://github.com/influxdata/telegraf/issues/3004): Do not allow metrics with trailing slashes.
- [#3011](https://github.com/influxdata/telegraf/issues/3011): Prevent Write from being called concurrently.

## v1.3.3 [2017-06-28]

### Bug Fixes

- [#2915](https://github.com/influxdata/telegraf/issues/2915): Allow dos line endings in tail and logparser.
- [#2937](https://github.com/influxdata/telegraf/issues/2937): Remove label value sanitization in prometheus output.
- [#2948](https://github.com/influxdata/telegraf/issues/2948): Fix bug parsing default timestamps with modified precision.
- [#2954](https://github.com/influxdata/telegraf/issues/2954): Fix panic in elasticsearch input if cannot determine master.

## v1.3.2 [2017-06-14]

### Bug Fixes

- [#2862](https://github.com/influxdata/telegraf/issues/2862): Fix InfluxDB UDP metric splitting.
- [#2888](https://github.com/influxdata/telegraf/issues/2888): Fix mongodb/leofs urls without scheme.
- [#2822](https://github.com/influxdata/telegraf/issues/2822): Fix inconsistent label dimensions in prometheus output.

## v1.3.1 [2017-05-31]

### Bug Fixes

- [#2749](https://github.com/influxdata/telegraf/pull/2749): Fixed sqlserver input to work with case sensitive server collation.
- [#2782](https://github.com/influxdata/telegraf/pull/2782): Reuse transports in input plugins
- [#2815](https://github.com/influxdata/telegraf/issues/2815): Inputs processes fails with "no such process".
- [#2851](https://github.com/influxdata/telegraf/pull/2851): Fix InfluxDB output database quoting.
- [#2856](https://github.com/influxdata/telegraf/issues/2856): Fix net input on older Linux kernels.
- [#2848](https://github.com/influxdata/telegraf/pull/2848): Fix panic in mongo input.
- [#2869](https://github.com/influxdata/telegraf/pull/2869): Fix length calculation of split metric buffer.

## v1.3 [2017-05-15]

### Release Notes

- Users of the windows `ping` plugin will need to drop or migrate their
measurements in order to continue using the plugin. The reason for this is that
the windows plugin was outputting a different type than the linux plugin. This
made it impossible to use the `ping` plugin for both windows and linux
machines.

- Ceph: the `ceph_pgmap_state` metric content has been modified to use a unique field `count`, with each state expressed as a `state` tag.

Telegraf < 1.3:

```text
# field_name             value
active+clean             123
active+clean+scrubbing   3
```

Telegraf >= 1.3:

```text
# field_name    value       tag
count           123         state=active+clean
count           3           state=active+clean+scrubbing
```

- The [Riemann output plugin](./plugins/outputs/riemann) has been rewritten
and the previous riemann plugin is _incompatible_ with the new one. The reasons
for this are outlined in issue [#1878](https://github.com/influxdata/telegraf/issues/1878).
The previous riemann output will still be available using
`outputs.riemann_legacy` if needed, but that will eventually be deprecated.
It is highly recommended that all users migrate to the new riemann output plugin.

- Generic [socket_listener](./plugins/inputs/socket_listener) and
[socket_writer](./plugins/outputs/socket_writer) plugins have been implemented
for receiving and sending UDP, TCP, unix, & unix-datagram data. These plugins
will replace udp_listener and tcp_listener, which are still available but will
be deprecated eventually.

### Features

- [#2721](https://github.com/influxdata/telegraf/pull/2721): Added SASL options for kafka output plugin.
- [#2723](https://github.com/influxdata/telegraf/pull/2723): Added SSL configuration for input haproxy.
- [#2494](https://github.com/influxdata/telegraf/pull/2494): Add interrupts input plugin.
- [#2094](https://github.com/influxdata/telegraf/pull/2094): Add generic socket listener & writer.
- [#2204](https://github.com/influxdata/telegraf/pull/2204): Extend http_response to support searching for a substring in response. Return 1 if found, else 0.
- [#2137](https://github.com/influxdata/telegraf/pull/2137): Added userstats to mysql input plugin.
- [#2179](https://github.com/influxdata/telegraf/pull/2179): Added more InnoDB metric to MySQL plugin.
- [#2229](https://github.com/influxdata/telegraf/pull/2229): `ceph_pgmap_state` metric now uses a single field `count`, with PG state published as `state` tag.
- [#2251](https://github.com/influxdata/telegraf/pull/2251): InfluxDB output: use own client for improved through-put and less allocations.
- [#2330](https://github.com/influxdata/telegraf/pull/2330): Keep -config-directory when running as Windows service.
- [#1900](https://github.com/influxdata/telegraf/pull/1900): Riemann plugin rewrite.
- [#1453](https://github.com/influxdata/telegraf/pull/1453): diskio: add support for name templates and udev tags.
- [#2277](https://github.com/influxdata/telegraf/pull/2277): add integer metrics for Consul check health state.
- [#2201](https://github.com/influxdata/telegraf/pull/2201): Add lock option to the IPtables input plugin.
- [#2244](https://github.com/influxdata/telegraf/pull/2244): Support ipmi_sensor plugin querying local ipmi sensors.
- [#2339](https://github.com/influxdata/telegraf/pull/2339): Increment gather_errors for all errors emitted by inputs.
- [#2071](https://github.com/influxdata/telegraf/issues/2071): Use official docker SDK.
- [#1678](https://github.com/influxdata/telegraf/pull/1678): Add AMQP consumer input plugin
- [#2512](https://github.com/influxdata/telegraf/pull/2512): Added pprof tool.
- [#2501](https://github.com/influxdata/telegraf/pull/2501): Support DEAD(X) state in system input plugin.
- [#2522](https://github.com/influxdata/telegraf/pull/2522): Add support for mongodb client certificates.
- [#1948](https://github.com/influxdata/telegraf/pull/1948): Support adding SNMP table indexes as tags.
- [#2332](https://github.com/influxdata/telegraf/pull/2332): Add Elasticsearch 5.x output
- [#2587](https://github.com/influxdata/telegraf/pull/2587): Add json timestamp units configurability
- [#2597](https://github.com/influxdata/telegraf/issues/2597): Add support for Linux sysctl-fs metrics.
- [#2425](https://github.com/influxdata/telegraf/pull/2425): Support to include/exclude docker container labels as tags
- [#1667](https://github.com/influxdata/telegraf/pull/1667): dmcache input plugin
- [#2637](https://github.com/influxdata/telegraf/issues/2637): Add support for precision in http_listener
- [#2636](https://github.com/influxdata/telegraf/pull/2636): Add `message_len_max` option to `kafka_consumer` input
- [#1100](https://github.com/influxdata/telegraf/issues/1100): Add collectd parser
- [#1820](https://github.com/influxdata/telegraf/issues/1820): easier plugin testing without outputs
- [#2493](https://github.com/influxdata/telegraf/pull/2493): Check signature in the GitHub webhook plugin
- [#2038](https://github.com/influxdata/telegraf/issues/2038): Add papertrail support to webhooks
- [#2253](https://github.com/influxdata/telegraf/pull/2253): Change jolokia plugin to use bulk requests.
- [#2575](https://github.com/influxdata/telegraf/issues/2575) Add diskio input for Darwin
- [#2705](https://github.com/influxdata/telegraf/pull/2705): Kinesis output: add use_random_partitionkey option
- [#2635](https://github.com/influxdata/telegraf/issues/2635): add tcp keep-alive to socket_listener & socket_writer
- [#2031](https://github.com/influxdata/telegraf/pull/2031): Add Kapacitor input plugin
- [#2732](https://github.com/influxdata/telegraf/pull/2732): Use go 1.8.1
- [#2712](https://github.com/influxdata/telegraf/issues/2712): Documentation for rabbitmq input plugin
- [#2141](https://github.com/influxdata/telegraf/pull/2141): Logparser handles newly-created files.

### Bug Fixes

- [#2633](https://github.com/influxdata/telegraf/pull/2633): ipmi_sensor: allow @ symbol in password
- [#2077](https://github.com/influxdata/telegraf/issues/2077): SQL Server Input - Arithmetic overflow error converting numeric to data type int.
- [#2262](https://github.com/influxdata/telegraf/issues/2262): Flush jitter can inhibit metric collection.
- [#2318](https://github.com/influxdata/telegraf/issues/2318): haproxy input - Add missing fields.
- [#2287](https://github.com/influxdata/telegraf/issues/2287): Kubernetes input: Handle null startTime for stopped pods.
- [#2356](https://github.com/influxdata/telegraf/issues/2356): cpu input panic when /proc/stat is empty.
- [#2341](https://github.com/influxdata/telegraf/issues/2341): telegraf swallowing panics in --test mode.
- [#2358](https://github.com/influxdata/telegraf/pull/2358): Create pidfile with 644 permissions & defer file deletion.
- [#2360](https://github.com/influxdata/telegraf/pull/2360): Fixed install/remove of telegraf on non-systemd Debian/Ubuntu systems
- [#2282](https://github.com/influxdata/telegraf/issues/2282): Reloading telegraf freezes prometheus output.
- [#2390](https://github.com/influxdata/telegraf/issues/2390): Empty tag value causes error on InfluxDB output.
- [#2380](https://github.com/influxdata/telegraf/issues/2380): buffer_size field value is negative number from "internal" plugin.
- [#2414](https://github.com/influxdata/telegraf/issues/2414): Missing error handling in the MySQL plugin leads to segmentation violation.
- [#2462](https://github.com/influxdata/telegraf/pull/2462): Fix type conflict in windows ping plugin.
- [#2178](https://github.com/influxdata/telegraf/issues/2178): logparser: regexp with lookahead.
- [#2466](https://github.com/influxdata/telegraf/issues/2466): Telegraf can crash in LoadDirectory on 0600 files.
- [#2215](https://github.com/influxdata/telegraf/issues/2215): Iptables input: document better that rules without a comment are ignored.
- [#2483](https://github.com/influxdata/telegraf/pull/2483): Fix win_perf_counters capping values at 100.
- [#2498](https://github.com/influxdata/telegraf/pull/2498): Exporting Ipmi.Path to be set by config.
- [#2500](https://github.com/influxdata/telegraf/pull/2500): Remove warning if parse empty content
- [#2520](https://github.com/influxdata/telegraf/pull/2520): Update default value for Cloudwatch rate limit
- [#2513](https://github.com/influxdata/telegraf/issues/2513): create /etc/telegraf/telegraf.d directory in tarball.
- [#2541](https://github.com/influxdata/telegraf/issues/2541): Return error on unsupported serializer data format.
- [#1827](https://github.com/influxdata/telegraf/issues/1827): Fix Windows Performance Counters multi instance identifier
- [#2576](https://github.com/influxdata/telegraf/pull/2576): Add write timeout to Riemann output
- [#2596](https://github.com/influxdata/telegraf/pull/2596): fix timestamp parsing on prometheus plugin
- [#2610](https://github.com/influxdata/telegraf/pull/2610): Fix deadlock when output cannot write
- [#2410](https://github.com/influxdata/telegraf/issues/2410): Fix connection leak in postgresql.
- [#2628](https://github.com/influxdata/telegraf/issues/2628): Set default measurement name for snmp input.
- [#2649](https://github.com/influxdata/telegraf/pull/2649): Improve performance of diskio with many disks
- [#2671](https://github.com/influxdata/telegraf/issues/2671): The internal input plugin uses the wrong units for `heap_objects`
- [#2684](https://github.com/influxdata/telegraf/pull/2684): Fix ipmi_sensor config is shared between all plugin instances
- [#2450](https://github.com/influxdata/telegraf/issues/2450): Network statistics not collected when system has alias interfaces
- [#1911](https://github.com/influxdata/telegraf/issues/1911): Sysstat plugin needs LANG=C or similar locale
- [#2528](https://github.com/influxdata/telegraf/issues/2528): File output closes standard streams on reload.
- [#2603](https://github.com/influxdata/telegraf/issues/2603): AMQP output disconnect blocks all outputs
- [#2706](https://github.com/influxdata/telegraf/issues/2706): Improve documentation for redis input plugin

## v1.2.1 [2017-02-01]

### Bug Fixes

- [#2317](https://github.com/influxdata/telegraf/issues/2317): Fix segfault on nil metrics with influxdb output.
- [#2324](https://github.com/influxdata/telegraf/issues/2324): Fix negative number handling.

### Features

- [#2348](https://github.com/influxdata/telegraf/pull/2348): Go version 1.7.4 -> 1.7.5

## v1.2 [2017-01-00]

### Release Notes

- The StatsD plugin will now default all "delete_" config options to "true". This
will change te default behavior for users who were not specifying these parameters
in their config file.

- The StatsD plugin will also no longer save it's state on a service reload.
Essentially we have reverted PR [#887](https://github.com/influxdata/telegraf/pull/887).
The reason for this is that saving the state in a global variable is not
thread-safe (see [#1975](https://github.com/influxdata/telegraf/issues/1975) & [#2102](https://github.com/influxdata/telegraf/issues/2102)),
and this creates issues if users want to define multiple instances
of the statsd plugin. Saving state on reload may be considered in the future,
but this would need to be implemented at a higher level and applied to all
plugins, not just statsd.

### Features

- [#2123](https://github.com/influxdata/telegraf/pull/2123): Fix improper calculation of CPU percentages
- [#1564](https://github.com/influxdata/telegraf/issues/1564): Use RFC3339 timestamps in log output.
- [#1997](https://github.com/influxdata/telegraf/issues/1997): Non-default HTTP timeouts for RabbitMQ plugin.
- [#2074](https://github.com/influxdata/telegraf/pull/2074): "discard" output plugin added, primarily for testing purposes.
- [#1965](https://github.com/influxdata/telegraf/pull/1965): The JSON parser can now parse an array of objects using the same configuration.
- [#1807](https://github.com/influxdata/telegraf/pull/1807): Option to use device name rather than path for reporting disk stats.
- [#1348](https://github.com/influxdata/telegraf/issues/1348): Telegraf "internal" plugin for collecting stats on itself.
- [#2127](https://github.com/influxdata/telegraf/pull/2127): Update Go version to 1.7.4.
- [#2126](https://github.com/influxdata/telegraf/pull/2126): Support a metric.Split function.
- [#2026](https://github.com/influxdata/telegraf/pull/2065): elasticsearch "shield" (basic auth) support doc.
- [#1885](https://github.com/influxdata/telegraf/pull/1885): Fix over-querying of cloudwatch metrics
- [#1913](https://github.com/influxdata/telegraf/pull/1913): OpenTSDB basic auth support.
- [#1908](https://github.com/influxdata/telegraf/pull/1908): RabbitMQ Connection metrics.
- [#1937](https://github.com/influxdata/telegraf/pull/1937): HAProxy session limit metric.
- [#2068](https://github.com/influxdata/telegraf/issues/2068): Accept strings for StatsD sets.
- [#1893](https://github.com/influxdata/telegraf/issues/1893): Change StatsD default "reset" behavior.
- [#2079](https://github.com/influxdata/telegraf/pull/2079): Enable setting ClientID in MQTT output.
- [#2001](https://github.com/influxdata/telegraf/pull/2001): MongoDB input plugin: Improve state data.
- [#2078](https://github.com/influxdata/telegraf/pull/2078): Ping input: add standard deviation field.
- [#2121](https://github.com/influxdata/telegraf/pull/2121): Add GC pause metric to InfluxDB input plugin.
- [#2006](https://github.com/influxdata/telegraf/pull/2006): Added response_timeout property to prometheus input plugin.
- [#1763](https://github.com/influxdata/telegraf/issues/1763): Pulling github.com/lxn/win's pdh wrapper into telegraf.
- [#1898](https://github.com/influxdata/telegraf/issues/1898): Support negative statsd counters.
- [#1921](https://github.com/influxdata/telegraf/issues/1921): Elasticsearch cluster stats support.
- [#1942](https://github.com/influxdata/telegraf/pull/1942): Change Amazon Kinesis output plugin to use the built-in serializer plugins.
- [#1980](https://github.com/influxdata/telegraf/issues/1980): Hide username/password from elasticsearch error log messages.
- [#2097](https://github.com/influxdata/telegraf/issues/2097): Configurable HTTP timeouts in Jolokia plugin
- [#2255](https://github.com/influxdata/telegraf/pull/2255): Allow changing jolokia attribute delimiter

### Bug Fixes

- [#2049](https://github.com/influxdata/telegraf/pull/2049): Fix the Value data format not trimming null characters from input.
- [#1949](https://github.com/influxdata/telegraf/issues/1949): Fix windows `net` plugin.
- [#1775](https://github.com/influxdata/telegraf/issues/1775): Cache & expire metrics for delivery to prometheus
- [#1775](https://github.com/influxdata/telegraf/issues/1775): Cache & expire metrics for delivery to prometheus.
- [#2146](https://github.com/influxdata/telegraf/issues/2146): Fix potential panic in aggregator plugin metric maker.
- [#1843](https://github.com/influxdata/telegraf/pull/1843) & [#1668](https://github.com/influxdata/telegraf/issues/1668): Add optional ability to define PID as a tag.
- [#1730](https://github.com/influxdata/telegraf/issues/1730) & [#2261](https://github.com/influxdata/telegraf/pull/2261): Fix win_perf_counters not gathering non-English counters.
- [#2061](https://github.com/influxdata/telegraf/issues/2061): Fix panic when file stat info cannot be collected due to permissions or other issue(s).
- [#2045](https://github.com/influxdata/telegraf/issues/2045): Graylog output should set short_message field.
- [#1904](https://github.com/influxdata/telegraf/issues/1904): Hddtemp always put the value in the field temperature.
- [#1693](https://github.com/influxdata/telegraf/issues/1693): Properly collect nested jolokia struct data.
- [#1917](https://github.com/influxdata/telegraf/pull/1917): fix puppetagent inputs plugin to support string for config variable.
- [#1987](https://github.com/influxdata/telegraf/issues/1987): fix docker input plugin tags when registry has port.
- [#2089](https://github.com/influxdata/telegraf/issues/2089): Fix tail input when reading from a pipe.
- [#1449](https://github.com/influxdata/telegraf/issues/1449): MongoDB plugin always shows 0 replication lag.
- [#1825](https://github.com/influxdata/telegraf/issues/1825): Consul plugin: add check_id as a tag in metrics to avoid overwrites.
- [#1973](https://github.com/influxdata/telegraf/issues/1973): Partial fix: logparser CLF pattern with IPv6 addresses.
- [#1975](https://github.com/influxdata/telegraf/issues/1975) & [#2102](https://github.com/influxdata/telegraf/issues/2102): Fix thread-safety when using multiple instances of the statsd input plugin.
- [#2027](https://github.com/influxdata/telegraf/issues/2027): docker input: interface conversion panic fix.
- [#1814](https://github.com/influxdata/telegraf/issues/1814): snmp: ensure proper context is present on error messages.
- [#2299](https://github.com/influxdata/telegraf/issues/2299): opentsdb: add tcp:// prefix if no scheme provided.
- [#2297](https://github.com/influxdata/telegraf/issues/2297): influx parser: parse line-protocol without newlines.
- [#2245](https://github.com/influxdata/telegraf/issues/2245): influxdb output: fix field type conflict blocking output buffer.

## v1.1.2 [2016-12-12]

### Bug Fixes

- [#2007](https://github.com/influxdata/telegraf/issues/2007): Make snmptranslate not required when using numeric OID.
- [#2104](https://github.com/influxdata/telegraf/issues/2104): Add a global snmp translation cache.

## v1.1.1 [2016-11-14]

### Bug Fixes

- [#2023](https://github.com/influxdata/telegraf/issues/2023): Fix issue parsing toml durations with single quotes.

## v1.1.0 [2016-11-07]

### Release Notes

- Telegraf now supports two new types of plugins: processors & aggregators.

- On systemd Telegraf will no longer redirect it's stdout to /var/log/telegraf/telegraf.log.
On most systems, the logs will be directed to the systemd journal and can be
accessed by `journalctl -u telegraf.service`. Consult the systemd journal
documentation for configuring journald. There is also a [`logfile` config option](https://github.com/influxdata/telegraf/blob/master/etc/telegraf.conf#L70)
available in 1.1, which will allow users to easily configure telegraf to
continue sending logs to /var/log/telegraf/telegraf.log.

### Features

- [#1726](https://github.com/influxdata/telegraf/issues/1726): Processor & Aggregator plugin support.
- [#1861](https://github.com/influxdata/telegraf/pull/1861): adding the tags in the graylog output plugin
- [#1732](https://github.com/influxdata/telegraf/pull/1732): Telegraf systemd service, log to journal.
- [#1782](https://github.com/influxdata/telegraf/pull/1782): Allow numeric and non-string values for tag_keys.
- [#1694](https://github.com/influxdata/telegraf/pull/1694): Adding Gauge and Counter metric types.
- [#1606](https://github.com/influxdata/telegraf/pull/1606): Remove carraige returns from exec plugin output on Windows
- [#1674](https://github.com/influxdata/telegraf/issues/1674): elasticsearch input: configurable timeout.
- [#1607](https://github.com/influxdata/telegraf/pull/1607): Massage metric names in Instrumental output plugin
- [#1572](https://github.com/influxdata/telegraf/pull/1572): mesos improvements.
- [#1513](https://github.com/influxdata/telegraf/issues/1513): Add Ceph Cluster Performance Statistics
- [#1650](https://github.com/influxdata/telegraf/issues/1650): Ability to configure response_timeout in httpjson input.
- [#1685](https://github.com/influxdata/telegraf/issues/1685): Add additional redis metrics.
- [#1539](https://github.com/influxdata/telegraf/pull/1539): Added capability to send metrics through Http API for OpenTSDB.
- [#1471](https://github.com/influxdata/telegraf/pull/1471): iptables input plugin.
- [#1542](https://github.com/influxdata/telegraf/pull/1542): Add filestack webhook plugin.
- [#1599](https://github.com/influxdata/telegraf/pull/1599): Add server hostname for each docker measurements.
- [#1697](https://github.com/influxdata/telegraf/pull/1697): Add NATS output plugin.
- [#1407](https://github.com/influxdata/telegraf/pull/1407) & [#1915](https://github.com/influxdata/telegraf/pull/1915): HTTP service listener input plugin.
- [#1699](https://github.com/influxdata/telegraf/pull/1699): Add database blacklist option for Postgresql
- [#1791](https://github.com/influxdata/telegraf/pull/1791): Add Docker container state metrics to Docker input plugin output
- [#1755](https://github.com/influxdata/telegraf/issues/1755): Add support to SNMP for IP & MAC address conversion.
- [#1729](https://github.com/influxdata/telegraf/issues/1729): Add support to SNMP for OID index suffixes.
- [#1813](https://github.com/influxdata/telegraf/pull/1813): Change default arguments for SNMP plugin.
- [#1686](https://github.com/influxdata/telegraf/pull/1686): Mesos input plugin: very high-cardinality mesos-task metrics removed.
- [#1838](https://github.com/influxdata/telegraf/pull/1838): Logging overhaul to centralize the logger & log levels, & provide a logfile config option.
- [#1700](https://github.com/influxdata/telegraf/pull/1700): HAProxy plugin socket glob matching.
- [#1847](https://github.com/influxdata/telegraf/pull/1847): Add Kubernetes plugin for retrieving pod metrics.

### Bug Fixes

- [#1955](https://github.com/influxdata/telegraf/issues/1955): Fix NATS plug-ins reconnection logic.
- [#1936](https://github.com/influxdata/telegraf/issues/1936): Set required default values in udp_listener & tcp_listener.
- [#1926](https://github.com/influxdata/telegraf/issues/1926): Fix toml unmarshal panic in Duration objects.
- [#1746](https://github.com/influxdata/telegraf/issues/1746): Fix handling of non-string values for JSON keys listed in tag_keys.
- [#1628](https://github.com/influxdata/telegraf/issues/1628): Fix mongodb input panic on version 2.2.
- [#1733](https://github.com/influxdata/telegraf/issues/1733): Fix statsd scientific notation parsing
- [#1716](https://github.com/influxdata/telegraf/issues/1716): Sensors plugin strconv.ParseFloat: parsing "": invalid syntax
- [#1530](https://github.com/influxdata/telegraf/issues/1530): Fix prometheus_client reload panic
- [#1764](https://github.com/influxdata/telegraf/issues/1764): Fix kafka consumer panic when nil error is returned down errs channel.
- [#1768](https://github.com/influxdata/telegraf/pull/1768): Speed up statsd parsing.
- [#1751](https://github.com/influxdata/telegraf/issues/1751): Fix powerdns integer parse error handling.
- [#1752](https://github.com/influxdata/telegraf/issues/1752): Fix varnish plugin defaults not being used.
- [#1517](https://github.com/influxdata/telegraf/issues/1517): Fix windows glob paths.
- [#1137](https://github.com/influxdata/telegraf/issues/1137): Fix issue loading config directory on windows.
- [#1772](https://github.com/influxdata/telegraf/pull/1772): Windows remote management interactive service fix.
- [#1702](https://github.com/influxdata/telegraf/issues/1702): sqlserver, fix issue when case sensitive collation is activated.
- [#1823](https://github.com/influxdata/telegraf/issues/1823): Fix huge allocations in http_listener when dealing with huge payloads.
- [#1833](https://github.com/influxdata/telegraf/issues/1833): Fix translating SNMP fields not in MIB.
- [#1835](https://github.com/influxdata/telegraf/issues/1835): Fix SNMP emitting empty fields.
- [#1854](https://github.com/influxdata/telegraf/pull/1853): SQL Server waitstats truncation bug.
- [#1810](https://github.com/influxdata/telegraf/issues/1810): Fix logparser common log format: numbers in ident.
- [#1793](https://github.com/influxdata/telegraf/pull/1793): Fix JSON Serialization in OpenTSDB output.
- [#1731](https://github.com/influxdata/telegraf/issues/1731): Fix Graphite template ordering, use most specific.
- [#1836](https://github.com/influxdata/telegraf/pull/1836): Fix snmp table field initialization for non-automatic table.
- [#1724](https://github.com/influxdata/telegraf/issues/1724): cgroups path being parsed as metric.
- [#1886](https://github.com/influxdata/telegraf/issues/1886): Fix phpfpm fcgi client panic when URL does not exist.
- [#1344](https://github.com/influxdata/telegraf/issues/1344): Fix config file parse error logging.
- [#1771](https://github.com/influxdata/telegraf/issues/1771): Delete nil fields in the metric maker.
- [#870](https://github.com/influxdata/telegraf/issues/870): Fix MySQL special characters in DSN parsing.
- [#1742](https://github.com/influxdata/telegraf/issues/1742): Ping input odd timeout behavior.
- [#1950](https://github.com/influxdata/telegraf/pull/1950): Switch to github.com/kballard/go-shellquote.

## v1.0.1 [2016-09-26]

### Bug Fixes

- [#1775](https://github.com/influxdata/telegraf/issues/1775): Prometheus output: Fix bug with multi-batch writes.
- [#1738](https://github.com/influxdata/telegraf/issues/1738): Fix unmarshal of influxdb metrics with null tags.
- [#1773](https://github.com/influxdata/telegraf/issues/1773): Add configurable timeout to influxdb input plugin.
- [#1785](https://github.com/influxdata/telegraf/pull/1785): Fix statsd no default value panic.

## v1.0 [2016-09-08]

### Release Notes

**Breaking Change** The SNMP plugin is being deprecated in it's current form.
There is a [new SNMP plugin](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/snmp)
which fixes many of the issues and confusions
of its predecessor. For users wanting to continue to use the deprecated SNMP
plugin, you will need to change your config file from `[[inputs.snmp]]` to
`[[inputs.snmp_legacy]]`. The configuration of the new SNMP plugin is _not_
backwards-compatible.

**Breaking Change**: Aerospike main server node measurements have been renamed
aerospike_node. Aerospike namespace measurements have been renamed to
aerospike_namespace. They will also now be tagged with the node_name
that they correspond to. This has been done to differentiate measurements
that pertain to node vs. namespace statistics.

**Breaking Change**: users of github_webhooks must change to the new
`[[inputs.webhooks]]` plugin.

This means that the default github_webhooks config:

```toml
# A Github Webhook Event collector
[[inputs.github_webhooks]]
  ## Address and port to host Webhook listener on
  service_address = ":1618"
```

should now look like:

```toml
# A Webhooks Event collector
[[inputs.webhooks]]
  ## Address and port to host Webhook listener on
  service_address = ":1618"

  [inputs.webhooks.github]
    path = "/"
```

- Telegraf now supports being installed as an official windows service,
which can be installed via
`> C:\Program Files\Telegraf\telegraf.exe --service install`

- `flush_jitter` behavior has been changed. The random jitter will now be
evaluated at every flush interval, rather than once at startup. This makes it
consistent with the behavior of `collection_jitter`.

- postgresql plugins now handle oid and name typed columns seamlessly, previously they were ignored/skipped.

### Features

- [#1617](https://github.com/influxdata/telegraf/pull/1617): postgresql_extensible now handles name and oid types correctly.
- [#1413](https://github.com/influxdata/telegraf/issues/1413): Separate container_version from container_image tag.
- [#1525](https://github.com/influxdata/telegraf/pull/1525): Support setting per-device and total metrics for Docker network and blockio.
- [#1466](https://github.com/influxdata/telegraf/pull/1466): MongoDB input plugin: adding per DB stats from db.stats()
- [#1503](https://github.com/influxdata/telegraf/pull/1503): Add tls support for certs to RabbitMQ input plugin
- [#1289](https://github.com/influxdata/telegraf/pull/1289): webhooks input plugin. Thanks @francois2metz and @cduez!
- [#1247](https://github.com/influxdata/telegraf/pull/1247): rollbar webhook plugin.
- [#1408](https://github.com/influxdata/telegraf/pull/1408): mandrill webhook plugin.
- [#1402](https://github.com/influxdata/telegraf/pull/1402): docker-machine/boot2docker no longer required for unit tests.
- [#1350](https://github.com/influxdata/telegraf/pull/1350): cgroup input plugin.
- [#1369](https://github.com/influxdata/telegraf/pull/1369): Add input plugin for consuming metrics from NSQD.
- [#1369](https://github.com/influxdata/telegraf/pull/1480): add ability to read redis from a socket.
- [#1387](https://github.com/influxdata/telegraf/pull/1387): **Breaking Change** - Redis `role` tag renamed to `replication_role` to avoid global_tags override
- [#1437](https://github.com/influxdata/telegraf/pull/1437): Fetching Galera status metrics in MySQL
- [#1500](https://github.com/influxdata/telegraf/pull/1500): Aerospike plugin refactored to use official client lib.
- [#1434](https://github.com/influxdata/telegraf/pull/1434): Add measurement name arg to logparser plugin.
- [#1479](https://github.com/influxdata/telegraf/pull/1479): logparser: change resp_code from a field to a tag.
- [#1411](https://github.com/influxdata/telegraf/pull/1411): Implement support for fetching hddtemp data
- [#1340](https://github.com/influxdata/telegraf/issues/1340): statsd: do not log every dropped metric.
- [#1368](https://github.com/influxdata/telegraf/pull/1368): Add precision rounding to all metrics on collection.
- [#1390](https://github.com/influxdata/telegraf/pull/1390): Add support for Tengine
- [#1320](https://github.com/influxdata/telegraf/pull/1320): Logparser input plugin for parsing grok-style log patterns.
- [#1397](https://github.com/influxdata/telegraf/issues/1397): ElasticSearch: now supports connecting to ElasticSearch via SSL
- [#1262](https://github.com/influxdata/telegraf/pull/1261): Add graylog input plugin.
- [#1294](https://github.com/influxdata/telegraf/pull/1294): consul input plugin. Thanks @harnash
- [#1164](https://github.com/influxdata/telegraf/pull/1164): conntrack input plugin. Thanks @robinpercy!
- [#1165](https://github.com/influxdata/telegraf/pull/1165): vmstat input plugin. Thanks @jshim-xm!
- [#1208](https://github.com/influxdata/telegraf/pull/1208): Standardized AWS credentials evaluation & wildcard CloudWatch dimensions. Thanks @johnrengelman!
- [#1264](https://github.com/influxdata/telegraf/pull/1264): Add SSL config options to http_response plugin.
- [#1272](https://github.com/influxdata/telegraf/pull/1272): graphite parser: add ability to specify multiple tag keys, for consistency with influxdb parser.
- [#1265](https://github.com/influxdata/telegraf/pull/1265): Make dns lookups for chrony configurable. Thanks @zbindenren!
- [#1275](https://github.com/influxdata/telegraf/pull/1275): Allow wildcard filtering of varnish stats.
- [#1142](https://github.com/influxdata/telegraf/pull/1142): Support for glob patterns in exec plugin commands configuration.
- [#1278](https://github.com/influxdata/telegraf/pull/1278): RabbitMQ input: made url parameter optional by using DefaultURL `http://localhost:15672` if not specified
- [#1197](https://github.com/influxdata/telegraf/pull/1197): Limit AWS GetMetricStatistics requests to 10 per second.
- [#1278](https://github.com/influxdata/telegraf/pull/1278) & [#1288](https://github.com/influxdata/telegraf/pull/1288) & [#1295](https://github.com/influxdata/telegraf/pull/1295): RabbitMQ/Apache/InfluxDB inputs: made url(s) parameter optional by using reasonable input defaults if not specified
- [#1296](https://github.com/influxdata/telegraf/issues/1296): Refactor of flush_jitter argument.
- [#1213](https://github.com/influxdata/telegraf/issues/1213): Add inactive & active memory to mem plugin.
- [#1543](https://github.com/influxdata/telegraf/pull/1543): Official Windows service.
- [#1414](https://github.com/influxdata/telegraf/pull/1414): Forking sensors command to remove C package dependency.
- [#1389](https://github.com/influxdata/telegraf/pull/1389): Add a new SNMP plugin.

### Bug Fixes

- [#1619](https://github.com/influxdata/telegraf/issues/1619): Fix `make windows` build target
- [#1519](https://github.com/influxdata/telegraf/pull/1519): Fix error race conditions and partial failures.
- [#1477](https://github.com/influxdata/telegraf/issues/1477): nstat: fix inaccurate config panic.
- [#1481](https://github.com/influxdata/telegraf/issues/1481): jolokia: fix handling multiple multi-dimensional attributes.
- [#1430](https://github.com/influxdata/telegraf/issues/1430): Fix prometheus character sanitizing. Sanitize more win_perf_counters characters.
- [#1534](https://github.com/influxdata/telegraf/pull/1534): Add diskio io_time to FreeBSD & report timing metrics as ms (as linux does).
- [#1379](https://github.com/influxdata/telegraf/issues/1379): Fix covering Amazon Linux for post remove flow.
- [#1584](https://github.com/influxdata/telegraf/issues/1584): procstat missing fields: read/write bytes & count
- [#1472](https://github.com/influxdata/telegraf/pull/1472): diskio input plugin: set 'skip_serial_number = true' by default to avoid high cardinality.
- [#1426](https://github.com/influxdata/telegraf/pull/1426): nil metrics panic fix.
- [#1384](https://github.com/influxdata/telegraf/pull/1384): Fix datarace in apache input plugin.
- [#1399](https://github.com/influxdata/telegraf/issues/1399): Add `read_repairs` statistics to riak plugin.
- [#1405](https://github.com/influxdata/telegraf/issues/1405): Fix memory/connection leak in prometheus input plugin.
- [#1378](https://github.com/influxdata/telegraf/issues/1378): Trim BOM from config file for Windows support.
- [#1339](https://github.com/influxdata/telegraf/issues/1339): Prometheus client output panic on service reload.
- [#1461](https://github.com/influxdata/telegraf/pull/1461): Prometheus parser, protobuf format header fix.
- [#1334](https://github.com/influxdata/telegraf/issues/1334): Prometheus output, metric refresh and caching fixes.
- [#1432](https://github.com/influxdata/telegraf/issues/1432): Panic fix for multiple graphite outputs under very high load.
- [#1412](https://github.com/influxdata/telegraf/pull/1412): Instrumental output has better reconnect behavior
- [#1460](https://github.com/influxdata/telegraf/issues/1460): Remove PID from procstat plugin to fix cardinality issues.
- [#1427](https://github.com/influxdata/telegraf/issues/1427): Cassandra input: version 2.x "column family" fix.
- [#1463](https://github.com/influxdata/telegraf/issues/1463): Shared WaitGroup in Exec plugin
- [#1436](https://github.com/influxdata/telegraf/issues/1436): logparser: honor modifiers in "pattern" config.
- [#1418](https://github.com/influxdata/telegraf/issues/1418): logparser: error and exit on file permissions/missing errors.
- [#1499](https://github.com/influxdata/telegraf/pull/1499): Make the user able to specify full path for HAproxy stats
- [#1521](https://github.com/influxdata/telegraf/pull/1521): Fix Redis url, an extra "tcp://" was added.
- [#1330](https://github.com/influxdata/telegraf/issues/1330): Fix exec plugin panic when using single binary.
- [#1336](https://github.com/influxdata/telegraf/issues/1336): Fixed incorrect prometheus metrics source selection.
- [#1112](https://github.com/influxdata/telegraf/issues/1112): Set default Zookeeper chroot to empty string.
- [#1335](https://github.com/influxdata/telegraf/issues/1335): Fix overall ping timeout to be calculated based on per-ping timeout.
- [#1374](https://github.com/influxdata/telegraf/pull/1374): Change "default" retention policy to "".
- [#1377](https://github.com/influxdata/telegraf/issues/1377): Graphite output mangling '%' character.
- [#1396](https://github.com/influxdata/telegraf/pull/1396): Prometheus input plugin now supports x509 certs authentication
- [#1252](https://github.com/influxdata/telegraf/pull/1252) & [#1279](https://github.com/influxdata/telegraf/pull/1279): Fix systemd service. Thanks @zbindenren & @PierreF!
- [#1221](https://github.com/influxdata/telegraf/pull/1221): Fix influxdb n_shards counter.
- [#1258](https://github.com/influxdata/telegraf/pull/1258): Fix potential kernel plugin integer parse error.
- [#1268](https://github.com/influxdata/telegraf/pull/1268): Fix potential influxdb input type assertion panic.
- [#1283](https://github.com/influxdata/telegraf/pull/1283): Still send processes metrics if a process exited during metric collection.
- [#1297](https://github.com/influxdata/telegraf/issues/1297): disk plugin panic when usage grab fails.
- [#1316](https://github.com/influxdata/telegraf/pull/1316): Removed leaked "database" tag on redis metrics. Thanks @PierreF!
- [#1323](https://github.com/influxdata/telegraf/issues/1323): Processes plugin: fix potential error with /proc/net/stat directory.
- [#1322](https://github.com/influxdata/telegraf/issues/1322): Fix rare RHEL 5.2 panic in gopsutil diskio gathering function.
- [#1586](https://github.com/influxdata/telegraf/pull/1586): Remove IF NOT EXISTS from influxdb output database creation.
- [#1600](https://github.com/influxdata/telegraf/issues/1600): Fix quoting with text values in postgresql_extensible plugin.
- [#1425](https://github.com/influxdata/telegraf/issues/1425): Fix win_perf_counter "index out of range" panic.
- [#1634](https://github.com/influxdata/telegraf/issues/1634): Fix ntpq panic when field is missing.
- [#1637](https://github.com/influxdata/telegraf/issues/1637): Sanitize graphite output field names.
- [#1695](https://github.com/influxdata/telegraf/pull/1695): Fix MySQL plugin not sending 0 value fields.

## v0.13.1 [2016-05-24]

### Release Notes

- net_response and http_response plugins timeouts will now accept duration
strings, ie, "2s" or "500ms".
- Input plugin Gathers will no longer be logged by default, but a Gather for
_each_ plugin will be logged in Debug mode.
- Debug mode will no longer print every point added to the accumulator. This
functionality can be duplicated using the `file` output plugin and printing
to "stdout".

### Features

- [#1173](https://github.com/influxdata/telegraf/pull/1173): varnish input plugin. Thanks @sfox-xmatters!
- [#1138](https://github.com/influxdata/telegraf/pull/1138): nstat input plugin. Thanks @Maksadbek!
- [#1139](https://github.com/influxdata/telegraf/pull/1139): instrumental output plugin. Thanks @jasonroelofs!
- [#1172](https://github.com/influxdata/telegraf/pull/1172): Ceph storage stats. Thanks @robinpercy!
- [#1233](https://github.com/influxdata/telegraf/pull/1233): Updated golint gopsutil dependency.
- [#1238](https://github.com/influxdata/telegraf/pull/1238): chrony input plugin. Thanks @zbindenren!
- [#479](https://github.com/influxdata/telegraf/issues/479): per-plugin execution time added to debug output.
- [#1249](https://github.com/influxdata/telegraf/issues/1249): influxdb output: added write_consistency argument.

### Bug Fixes

- [#1195](https://github.com/influxdata/telegraf/pull/1195): Docker panic on timeout. Thanks @zstyblik!
- [#1211](https://github.com/influxdata/telegraf/pull/1211): mongodb input. Fix possible panic. Thanks @kols!
- [#1215](https://github.com/influxdata/telegraf/pull/1215): Fix for possible gopsutil-dependent plugin hangs.
- [#1228](https://github.com/influxdata/telegraf/pull/1228): Fix service plugin host tag overwrite.
- [#1198](https://github.com/influxdata/telegraf/pull/1198): http_response: override request Host header properly
- [#1230](https://github.com/influxdata/telegraf/issues/1230): Fix Telegraf process hangup due to a single plugin hanging.
- [#1214](https://github.com/influxdata/telegraf/issues/1214): Use TCP timeout argument in net_response plugin.
- [#1243](https://github.com/influxdata/telegraf/pull/1243): Logfile not created on systemd.

## v0.13 [2016-05-11]

### Release Notes

- **Breaking change** in jolokia plugin. See the
[jolokia README](https://github.com/influxdata/telegraf/blob/master/plugins/inputs/jolokia/README.md)
for updated configuration. The plugin will now support proxy mode and will make
POST requests.

- New [agent] configuration option: `metric_batch_size`. This option tells
telegraf the maximum batch size to allow to accumulate before sending a flush
to the configured outputs. `metric_buffer_limit` now refers to the absolute
maximum number of metrics that will accumulate before metrics are dropped.

- There is no longer an option to
`flush_buffer_when_full`, this is now the default and only behavior of telegraf.

- **Breaking Change**: docker plugin tags. The cont_id tag no longer exists, it
will now be a field, and be called container_id. Additionally, cont_image and
cont_name are being renamed to container_image and container_name.

- **Breaking Change**: docker plugin measurements. The `docker_cpu`, `docker_mem`,
`docker_blkio` and `docker_net` measurements are being renamed to
`docker_container_cpu`, `docker_container_mem`, `docker_container_blkio` and
`docker_container_net`. Why? Because these metrics are
specifically tracking per-container stats. The problem with per-container stats,
in some use-cases, is that if containers are short-lived AND names are not
kept consistent, then the series cardinality will balloon very quickly.
So adding "container" to each metric will:
(1) make it more clear that these metrics are per-container, and
(2) allow users to easily drop per-container metrics if cardinality is an
issue (`namedrop = ["docker_container_*"]`)

- `tagexclude` and `taginclude` are now available, which can be used to remove
tags from measurements on inputs and outputs. See
[the configuration doc](https://github.com/influxdata/telegraf/blob/master/docs/CONFIGURATION.md)
for more details.

- **Measurement filtering:** All measurement filters now match based on glob
only. Previously there was an undocumented behavior where filters would match
based on _prefix_ in addition to globs. This means that a filter like
`fielddrop = ["time_"]` will need to be changed to `fielddrop = ["time_*"]`

- **datadog**: measurement and field names will no longer have `_` replaced by `.`

- The following plugins have changed their tags to _not_ overwrite the host tag:
  - cassandra: `host -> cassandra_host`
  - disque: `host -> disque_host`
  - rethinkdb: `host -> rethinkdb_host`

- **Breaking Change**: The `win_perf_counters` input has been changed to
sanitize field names, replacing `/Sec` and `/sec` with `_persec`, as well as
spaces with underscores. This is needed because Graphite doesn't like slashes
and spaces, and was failing to accept metrics that had them.
The `/[sS]ec` -> `_persec` is just to make things clearer and uniform.

- **Breaking Change**: snmp plugin. The `host` tag of the snmp plugin has been
changed to the `snmp_host` tag.

- The `disk` input plugin can now be configured with the `HOST_MOUNT_PREFIX` environment variable.
This value is prepended to any mountpaths discovered before retrieving stats.
It is not included on the report path. This is necessary for reporting host disk stats when running from within a container.

### Features

- [#1031](https://github.com/influxdata/telegraf/pull/1031): Jolokia plugin proxy mode. Thanks @saiello!
- [#1017](https://github.com/influxdata/telegraf/pull/1017): taginclude and tagexclude arguments.
- [#1015](https://github.com/influxdata/telegraf/pull/1015): Docker plugin schema refactor.
- [#889](https://github.com/influxdata/telegraf/pull/889): Improved MySQL plugin. Thanks @maksadbek!
- [#1060](https://github.com/influxdata/telegraf/pull/1060): TTL metrics added to MongoDB input plugin
- [#1056](https://github.com/influxdata/telegraf/pull/1056): Don't allow inputs to overwrite host tags.
- [#1035](https://github.com/influxdata/telegraf/issues/1035): Add `user`, `exe`, `pidfile` tags to procstat plugin.
- [#1041](https://github.com/influxdata/telegraf/issues/1041): Add `n_cpus` field to the system plugin.
- [#1072](https://github.com/influxdata/telegraf/pull/1072): New Input Plugin: filestat.
- [#1066](https://github.com/influxdata/telegraf/pull/1066): Replication lag metrics for MongoDB input plugin
- [#1086](https://github.com/influxdata/telegraf/pull/1086): Ability to specify AWS keys in config file. Thanks @johnrengelman!
- [#1096](https://github.com/influxdata/telegraf/pull/1096): Performance refactor of running output buffers.
- [#967](https://github.com/influxdata/telegraf/issues/967): Buffer logging improvements.
- [#1107](https://github.com/influxdata/telegraf/issues/1107): Support lustre2 job stats. Thanks @hanleyja!
- [#1122](https://github.com/influxdata/telegraf/pull/1122): Support setting config path through env variable and default paths.
- [#1128](https://github.com/influxdata/telegraf/pull/1128): MongoDB jumbo chunks metric for MongoDB input plugin
- [#1146](https://github.com/influxdata/telegraf/pull/1146): HAProxy socket support. Thanks weshmashian!

### Bug Fixes

- [#1050](https://github.com/influxdata/telegraf/issues/1050): jolokia plugin - do not overwrite host tag. Thanks @saiello!
- [#921](https://github.com/influxdata/telegraf/pull/921): mqtt_consumer stops gathering metrics. Thanks @chaton78!
- [#1013](https://github.com/influxdata/telegraf/pull/1013): Close dead riemann output connections. Thanks @echupriyanov!
- [#1012](https://github.com/influxdata/telegraf/pull/1012): Set default tags in test accumulator.
- [#1024](https://github.com/influxdata/telegraf/issues/1024): Don't replace `.` with `_` in datadog output.
- [#1058](https://github.com/influxdata/telegraf/issues/1058): Fix possible leaky TCP connections in influxdb output.
- [#1044](https://github.com/influxdata/telegraf/pull/1044): Fix SNMP OID possible collisions. Thanks @relip
- [#1022](https://github.com/influxdata/telegraf/issues/1022): Dont error deb/rpm install on systemd errors.
- [#1078](https://github.com/influxdata/telegraf/issues/1078): Use default AWS credential chain.
- [#1070](https://github.com/influxdata/telegraf/issues/1070): SQL Server input. Fix datatype conversion.
- [#1089](https://github.com/influxdata/telegraf/issues/1089): Fix leaky TCP connections in phpfpm plugin.
- [#914](https://github.com/influxdata/telegraf/issues/914): Telegraf can drop metrics on full buffers.
- [#1098](https://github.com/influxdata/telegraf/issues/1098): Sanitize invalid OpenTSDB characters.
- [#1110](https://github.com/influxdata/telegraf/pull/1110): Sanitize * to - in graphite serializer. Thanks @goodeggs!
- [#1118](https://github.com/influxdata/telegraf/pull/1118): Sanitize Counter names for `win_perf_counters` input.
- [#1125](https://github.com/influxdata/telegraf/pull/1125): Wrap all exec command runners with a timeout, so hung os processes don't halt Telegraf.
- [#1113](https://github.com/influxdata/telegraf/pull/1113): Set MaxRetry and RequiredAcks defaults in Kafka output.
- [#1090](https://github.com/influxdata/telegraf/issues/1090): [agent] and [global_tags] config sometimes not getting applied.
- [#1133](https://github.com/influxdata/telegraf/issues/1133): Use a timeout for docker list & stat cmds.
- [#1052](https://github.com/influxdata/telegraf/issues/1052): Docker panic fix when decode fails.
- [#1136](https://github.com/influxdata/telegraf/pull/1136): "DELAYED" Inserts were deprecated in MySQL 5.6.6. Thanks @PierreF

## v0.12.1 [2016-04-14]

### Release Notes

- Breaking change in the dovecot input plugin. See Features section below.
- Graphite output templates are now supported. See the
[Output Formats README](https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_OUTPUT.md#graphite)
- Possible breaking change for the librato and graphite outputs. Telegraf will
no longer insert field names when the field is simply named `value`. This is
because the `value` field is redundant in the graphite/librato context.

### Features

- [#1009](https://github.com/influxdata/telegraf/pull/1009): Cassandra input plugin. Thanks @subhachandrachandra!
- [#976](https://github.com/influxdata/telegraf/pull/976): Reduce allocations in the UDP and statsd inputs.
- [#979](https://github.com/influxdata/telegraf/pull/979): Reduce allocations in the TCP listener.
- [#992](https://github.com/influxdata/telegraf/pull/992): Refactor allocations in TCP/UDP listeners.
- [#935](https://github.com/influxdata/telegraf/pull/935): AWS Cloudwatch input plugin. Thanks @joshhardy & @ljosa!
- [#943](https://github.com/influxdata/telegraf/pull/943): http_response input plugin. Thanks @Lswith!
- [#939](https://github.com/influxdata/telegraf/pull/939): sysstat input plugin. Thanks @zbindenren!
- [#998](https://github.com/influxdata/telegraf/pull/998): **breaking change** enabled global, user and ip queries in dovecot plugin. Thanks @mikif70!
- [#1001](https://github.com/influxdata/telegraf/pull/1001): Graphite serializer templates.
- [#1008](https://github.com/influxdata/telegraf/pull/1008): Adding memstats metrics to the influxdb plugin.

### Bug Fixes

- [#968](https://github.com/influxdata/telegraf/issues/968): Processes plugin gets unknown state when spaces are in (command name)
- [#969](https://github.com/influxdata/telegraf/pull/969): ipmi_sensors: allow : in password. Thanks @awaw!
- [#972](https://github.com/influxdata/telegraf/pull/972): dovecot: remove extra newline in dovecot command. Thanks @mrannanj!
- [#645](https://github.com/influxdata/telegraf/issues/645): docker plugin i/o error on closed pipe. Thanks @tripledes!

## v0.12.0 [2016-04-05]

### Features

- [#951](https://github.com/influxdata/telegraf/pull/951): Parse environment variables in the config file.
- [#948](https://github.com/influxdata/telegraf/pull/948): Cleanup config file and make default package version include all plugins (but commented).
- [#927](https://github.com/influxdata/telegraf/pull/927): Adds parsing of tags to the statsd input when using DataDog's dogstatsd extension
- [#863](https://github.com/influxdata/telegraf/pull/863): AMQP output: allow external auth. Thanks @ekini!
- [#707](https://github.com/influxdata/telegraf/pull/707): Improved prometheus plugin. Thanks @titilambert!
- [#878](https://github.com/influxdata/telegraf/pull/878): Added json serializer. Thanks @ch3lo!
- [#880](https://github.com/influxdata/telegraf/pull/880): Add the ability to specify the bearer token to the prometheus plugin. Thanks @jchauncey!
- [#882](https://github.com/influxdata/telegraf/pull/882): Fixed SQL Server Plugin issues
- [#849](https://github.com/influxdata/telegraf/issues/849): Adding ability to parse single values as an input data type.
- [#844](https://github.com/influxdata/telegraf/pull/844): postgres_extensible plugin added. Thanks @menardorama!
- [#866](https://github.com/influxdata/telegraf/pull/866): couchbase input plugin. Thanks @ljosa!
- [#789](https://github.com/influxdata/telegraf/pull/789): Support multiple field specification and `field*` in graphite templates. Thanks @chrusty!
- [#762](https://github.com/influxdata/telegraf/pull/762): Nagios parser for the exec plugin. Thanks @titilambert!
- [#848](https://github.com/influxdata/telegraf/issues/848): Provide option to omit host tag from telegraf agent.
- [#928](https://github.com/influxdata/telegraf/pull/928): Deprecating the statsd "convert_names" options, expose separator config.
- [#919](https://github.com/influxdata/telegraf/pull/919): ipmi_sensor input plugin. Thanks @ebookbug!
- [#945](https://github.com/influxdata/telegraf/pull/945): KAFKA output: codec, acks, and retry configuration. Thanks @framiere!

### Bug Fixes

- [#890](https://github.com/influxdata/telegraf/issues/890): Create TLS config even if only ssl_ca is provided.
- [#884](https://github.com/influxdata/telegraf/issues/884): Do not call write method if there are 0 metrics to write.
- [#898](https://github.com/influxdata/telegraf/issues/898): Put database name in quotes, fixes special characters in the database name.
- [#656](https://github.com/influxdata/telegraf/issues/656): No longer run `lsof` on linux to get netstat data, fixes permissions issue.
- [#907](https://github.com/influxdata/telegraf/issues/907): Fix prometheus invalid label/measurement name key.
- [#841](https://github.com/influxdata/telegraf/issues/841): Fix memcached unix socket panic.
- [#873](https://github.com/influxdata/telegraf/issues/873): Fix SNMP plugin sometimes not returning metrics. Thanks @titilambert!
- [#934](https://github.com/influxdata/telegraf/pull/934): phpfpm: Fix fcgi uri path. Thanks @rudenkovk!
- [#805](https://github.com/influxdata/telegraf/issues/805): Kafka consumer stops gathering after i/o timeout.
- [#959](https://github.com/influxdata/telegraf/pull/959): reduce mongodb & prometheus collection timeouts. Thanks @PierreF!

## v0.11.1 [2016-03-17]

### Release Notes

- Primarily this release was cut to fix [#859](https://github.com/influxdata/telegraf/issues/859)

### Features

- [#747](https://github.com/influxdata/telegraf/pull/747): Start telegraf on install & remove on uninstall. Thanks @PierreF!
- [#794](https://github.com/influxdata/telegraf/pull/794): Add service reload ability. Thanks @entertainyou!

### Bug Fixes

- [#852](https://github.com/influxdata/telegraf/issues/852): Windows zip package fix
- [#859](https://github.com/influxdata/telegraf/issues/859): httpjson plugin panic

## v0.11.0 [2016-03-15]

### Features

- [#692](https://github.com/influxdata/telegraf/pull/770): Support InfluxDB retention policies
- [#771](https://github.com/influxdata/telegraf/pull/771): Default timeouts for input plugns. Thanks @PierreF!
- [#758](https://github.com/influxdata/telegraf/pull/758): UDP Listener input plugin, thanks @whatyouhide!
- [#769](https://github.com/influxdata/telegraf/issues/769): httpjson plugin: allow specifying SSL configuration.
- [#735](https://github.com/influxdata/telegraf/pull/735): SNMP Table feature. Thanks @titilambert!
- [#754](https://github.com/influxdata/telegraf/pull/754): docker plugin: adding `docker info` metrics to output. Thanks @titilambert!
- [#788](https://github.com/influxdata/telegraf/pull/788): -input-list and -output-list command-line options. Thanks @ebookbug!
- [#778](https://github.com/influxdata/telegraf/pull/778): Adding a TCP input listener.
- [#797](https://github.com/influxdata/telegraf/issues/797): Provide option for persistent MQTT consumer client sessions.
- [#799](https://github.com/influxdata/telegraf/pull/799): Add number of threads for procstat input plugin. Thanks @titilambert!
- [#776](https://github.com/influxdata/telegraf/pull/776): Add Zookeeper chroot option to kafka_consumer. Thanks @prune998!
- [#811](https://github.com/influxdata/telegraf/pull/811): Add processes plugin for classifying total procs on system. Thanks @titilambert!
- [#235](https://github.com/influxdata/telegraf/issues/235): Add number of users to the `system` input plugin.
- [#826](https://github.com/influxdata/telegraf/pull/826): "kernel" linux plugin for /proc/stat metrics (context switches, interrupts, etc.)
- [#847](https://github.com/influxdata/telegraf/pull/847): `ntpq`: Input plugin for running ntp query executable and gathering metrics.

### Bug Fixes

- [#748](https://github.com/influxdata/telegraf/issues/748): Fix sensor plugin split on ":"
- [#722](https://github.com/influxdata/telegraf/pull/722): Librato output plugin fixes. Thanks @chrusty!
- [#745](https://github.com/influxdata/telegraf/issues/745): Fix Telegraf toml parse panic on large config files. Thanks @titilambert!
- [#781](https://github.com/influxdata/telegraf/pull/781): Fix mqtt_consumer username not being set. Thanks @chaton78!
- [#786](https://github.com/influxdata/telegraf/pull/786): Fix mqtt output username not being set. Thanks @msangoi!
- [#773](https://github.com/influxdata/telegraf/issues/773): Fix duplicate measurements in snmp plugin. Thanks @titilambert!
- [#708](https://github.com/influxdata/telegraf/issues/708): packaging: build ARM package
- [#713](https://github.com/influxdata/telegraf/issues/713): packaging: insecure permissions error on log directory
- [#816](https://github.com/influxdata/telegraf/issues/816): Fix phpfpm panic if fcgi endpoint unreachable.
- [#828](https://github.com/influxdata/telegraf/issues/828): fix net_response plugin overwriting host tag.
- [#821](https://github.com/influxdata/telegraf/issues/821): Remove postgres password from server tag. Thanks @menardorama!

## v0.10.4.1

### Release Notes

- Bug in the build script broke deb and rpm packages.

### Bug Fixes

- [#750](https://github.com/influxdata/telegraf/issues/750): deb package broken
- [#752](https://github.com/influxdata/telegraf/issues/752): rpm package broken

## v0.10.4 [2016-02-24]

### Release Notes

- The pass/drop parameters have been renamed to fielddrop/fieldpass parameters,
to more accurately indicate their purpose.
- There are also now namedrop/namepass parameters for passing/dropping based
on the metric _name_.
- Experimental windows builds now available.

### Features

- [#727](https://github.com/influxdata/telegraf/pull/727): riak input, thanks @jcoene!
- [#694](https://github.com/influxdata/telegraf/pull/694): DNS Query input, thanks @mjasion!
- [#724](https://github.com/influxdata/telegraf/pull/724): username matching for procstat input, thanks @zorel!
- [#736](https://github.com/influxdata/telegraf/pull/736): Ignore dummy filesystems from disk plugin. Thanks @PierreF!
- [#737](https://github.com/influxdata/telegraf/pull/737): Support multiple fields for statsd input. Thanks @mattheath!

### Bug Fixes

- [#701](https://github.com/influxdata/telegraf/pull/701): output write count shouldnt print in quiet mode.
- [#746](https://github.com/influxdata/telegraf/pull/746): httpjson plugin: Fix HTTP GET parameters.

## v0.10.3 [2016-02-18]

### Release Notes

- Users of the `exec` and `kafka_consumer` (and the new `nats_consumer`
and `mqtt_consumer` plugins) can now specify the incoming data
format that they would like to parse. Currently supports: "json", "influx", and
"graphite"
- Users of message broker and file output plugins can now choose what data format
they would like to output. Currently supports: "influx" and "graphite"
- More info on parsing _incoming_ data formats can be found
[here](https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_INPUT.md)
- More info on serializing _outgoing_ data formats can be found
[here](https://github.com/influxdata/telegraf/blob/master/docs/DATA_FORMATS_OUTPUT.md)
- Telegraf now has an option `flush_buffer_when_full` that will flush the
metric buffer whenever it fills up for each output, rather than dropping
points and only flushing on a set time interval. This will default to `true`
and is in the `[agent]` config section.

### Features

- [#652](https://github.com/influxdata/telegraf/pull/652): CouchDB Input Plugin. Thanks @codehate!
- [#655](https://github.com/influxdata/telegraf/pull/655): Support parsing arbitrary data formats. Currently limited to kafka_consumer and exec inputs.
- [#671](https://github.com/influxdata/telegraf/pull/671): Dovecot input plugin. Thanks @mikif70!
- [#680](https://github.com/influxdata/telegraf/pull/680): NATS consumer input plugin. Thanks @netixen!
- [#676](https://github.com/influxdata/telegraf/pull/676): MQTT consumer input plugin.
- [#683](https://github.com/influxdata/telegraf/pull/683): PostGRES input plugin: add pg_stat_bgwriter. Thanks @menardorama!
- [#679](https://github.com/influxdata/telegraf/pull/679): File/stdout output plugin.
- [#679](https://github.com/influxdata/telegraf/pull/679): Support for arbitrary output data formats.
- [#695](https://github.com/influxdata/telegraf/pull/695): raindrops input plugin. Thanks @burdandrei!
- [#650](https://github.com/influxdata/telegraf/pull/650): net_response input plugin. Thanks @titilambert!
- [#699](https://github.com/influxdata/telegraf/pull/699): Flush based on buffer size rather than time.
- [#682](https://github.com/influxdata/telegraf/pull/682): Mesos input plugin. Thanks @tripledes!

### Bug Fixes

- [#443](https://github.com/influxdata/telegraf/issues/443): Fix Ping command timeout parameter on Linux.
- [#662](https://github.com/influxdata/telegraf/pull/667): Change `[tags]` to `[global_tags]` to fix multiple-plugin tags bug.
- [#642](https://github.com/influxdata/telegraf/issues/642): Riemann output plugin issues.
- [#394](https://github.com/influxdata/telegraf/issues/394): Support HTTP POST. Thanks @gabelev!
- [#715](https://github.com/influxdata/telegraf/pull/715): Fix influxdb precision config panic. Thanks @netixen!

## v0.10.2 [2016-02-04]

### Release Notes

- Statsd timing measurements are now aggregated into a single measurement with
fields.
- Graphite output now inserts tags into the bucket in alphabetical order.
- Normalized TLS/SSL support for output plugins: MQTT, AMQP, Kafka
- `verify_ssl` config option was removed from Kafka because it was actually
doing the opposite of what it claimed to do (yikes). It's been replaced by
`insecure_skip_verify`

### Features

- [#575](https://github.com/influxdata/telegraf/pull/575): Support for collecting Windows Performance Counters. Thanks @TheFlyingCorpse!
- [#564](https://github.com/influxdata/telegraf/issues/564): features for plugin writing simplification. Internal metric data type.
- [#603](https://github.com/influxdata/telegraf/pull/603): Aggregate statsd timing measurements into fields. Thanks @marcinbunsch!
- [#601](https://github.com/influxdata/telegraf/issues/601): Warn when overwriting cached metrics.
- [#614](https://github.com/influxdata/telegraf/pull/614): PowerDNS input plugin. Thanks @Kasen!
- [#617](https://github.com/influxdata/telegraf/pull/617): exec plugin: parse influx line protocol in addition to JSON.
- [#628](https://github.com/influxdata/telegraf/pull/628): Windows perf counters: pre-vista support

### Bug Fixes

- [#595](https://github.com/influxdata/telegraf/issues/595): graphite output should include tags to separate duplicate measurements.
- [#599](https://github.com/influxdata/telegraf/issues/599): datadog plugin tags not working.
- [#600](https://github.com/influxdata/telegraf/issues/600): datadog measurement/field name parsing is wrong.
- [#602](https://github.com/influxdata/telegraf/issues/602): Fix statsd field name templating.
- [#612](https://github.com/influxdata/telegraf/pull/612): Docker input panic fix if stats received are nil.
- [#634](https://github.com/influxdata/telegraf/pull/634): Properly set host headers in httpjson. Thanks @reginaldosousa!

## v0.10.1 [2016-01-27]

### Release Notes

- Telegraf now keeps a fixed-length buffer of metrics per-output. This buffer
defaults to 10,000 metrics, and is adjustable. The buffer is cleared when a
successful write to that output occurs.
- The docker plugin has been significantly overhauled to add more metrics
and allow for docker-machine (incl OSX) support.
[See the readme](https://github.com/influxdata/telegraf/blob/master/plugins/inputs/docker/README.md)
for the latest measurements, fields, and tags. There is also now support for
specifying a docker endpoint to get metrics from.

### Features

- [#509](https://github.com/influxdata/telegraf/pull/509): Flatten JSON arrays with indices. Thanks @psilva261!
- [#512](https://github.com/influxdata/telegraf/pull/512): Python 3 build script, add lsof dep to package. Thanks @Ormod!
- [#475](https://github.com/influxdata/telegraf/pull/475): Add response time to httpjson plugin. Thanks @titilambert!
- [#519](https://github.com/influxdata/telegraf/pull/519): Added a sensors input based on lm-sensors. Thanks @md14454!
- [#467](https://github.com/influxdata/telegraf/issues/467): Add option to disable statsd measurement name conversion.
- [#534](https://github.com/influxdata/telegraf/pull/534): NSQ input plugin. Thanks @allingeek!
- [#494](https://github.com/influxdata/telegraf/pull/494): Graphite output plugin. Thanks @titilambert!
- AMQP SSL support. Thanks @ekini!
- [#539](https://github.com/influxdata/telegraf/pull/539): Reload config on SIGHUP. Thanks @titilambert!
- [#522](https://github.com/influxdata/telegraf/pull/522): Phusion passenger input plugin. Thanks @kureikain!
- [#541](https://github.com/influxdata/telegraf/pull/541): Kafka output TLS cert support. Thanks @Ormod!
- [#551](https://github.com/influxdata/telegraf/pull/551): Statsd UDP read packet size now defaults to 1500 bytes, and is configurable.
- [#552](https://github.com/influxdata/telegraf/pull/552): Support for collection interval jittering.
- [#484](https://github.com/influxdata/telegraf/issues/484): Include usage percent with procstat metrics.
- [#553](https://github.com/influxdata/telegraf/pull/553): Amazon CloudWatch output. thanks @skwong2!
- [#503](https://github.com/influxdata/telegraf/pull/503): Support docker endpoint configuration.
- [#563](https://github.com/influxdata/telegraf/pull/563): Docker plugin overhaul.
- [#285](https://github.com/influxdata/telegraf/issues/285): Fixed-size buffer of points.
- [#546](https://github.com/influxdata/telegraf/pull/546): SNMP Input plugin. Thanks @titilambert!
- [#589](https://github.com/influxdata/telegraf/pull/589): Microsoft SQL Server input plugin. Thanks @zensqlmonitor!
- [#573](https://github.com/influxdata/telegraf/pull/573): Github webhooks consumer input. Thanks @jackzampolin!
- [#471](https://github.com/influxdata/telegraf/pull/471): httpjson request headers. Thanks @asosso!

### Bug Fixes

- [#506](https://github.com/influxdata/telegraf/pull/506): Ping input doesn't return response time metric when timeout. Thanks @titilambert!
- [#508](https://github.com/influxdata/telegraf/pull/508): Fix prometheus cardinality issue with the `net` plugin
- [#499](https://github.com/influxdata/telegraf/issues/499) & [#502](https://github.com/influxdata/telegraf/issues/502): php fpm unix socket and other fixes, thanks @kureikain!
- [#543](https://github.com/influxdata/telegraf/issues/543): Statsd Packet size sometimes truncated.
- [#440](https://github.com/influxdata/telegraf/issues/440): Don't query filtered devices for disk stats.
- [#463](https://github.com/influxdata/telegraf/issues/463): Docker plugin not working on AWS Linux
- [#568](https://github.com/influxdata/telegraf/issues/568): Multiple output race condition.
- [#585](https://github.com/influxdata/telegraf/pull/585): Log stack trace and continue on Telegraf panic. Thanks @wutaizeng!

## v0.10.0 [2016-01-12]

### Release Notes

- Linux packages have been taken out of `opt`, the binary is now in `/usr/bin`
and configuration files are in `/etc/telegraf`
- **breaking change** `plugins` have been renamed to `inputs`. This was done because
`plugins` is too generic, as there are now also "output plugins", and will likely
be "aggregator plugins" and "filter plugins" in the future. Additionally,
`inputs/` and `outputs/` directories have been placed in the root-level `plugins/`
directory.
- **breaking change** the `io` plugin has been renamed `diskio`
- **breaking change** plugin measurements aggregated into a single measurement.
- **breaking change** `jolokia` plugin: must use global tag/drop/pass parameters
for configuration.
- **breaking change** `twemproxy` plugin: `prefix` option removed.
- **breaking change** `procstat` cpu measurements are now prepended with `cpu_time_`
instead of only `cpu_`
- **breaking change** some command-line flags have been renamed to separate words.
`-configdirectory` -> `-config-directory`, `-filter` -> `-input-filter`,
`-outputfilter` -> `-output-filter`
- The prometheus plugin schema has not been changed (measurements have not been
aggregated).

### Packaging change note

RHEL/CentOS users upgrading from 0.2.x to 0.10.0 will probably have their
configurations overwritten by the upgrade. There is a backup stored at
/etc/telegraf/telegraf.conf.$(date +%s).backup.

### Features

- Plugin measurements aggregated into a single measurement.
- Added ability to specify per-plugin tags
- Added ability to specify per-plugin measurement suffix and prefix.
(`name_prefix` and `name_suffix`)
- Added ability to override base plugin measurement name. (`name_override`)

### Bug Fixes

## v0.2.5 [unreleased]

### Features

- [#427](https://github.com/influxdata/telegraf/pull/427): zfs plugin: pool stats added. Thanks @allenpetersen!
- [#428](https://github.com/influxdata/telegraf/pull/428): Amazon Kinesis output. Thanks @jimmystewpot!
- [#449](https://github.com/influxdata/telegraf/pull/449): influxdb plugin, thanks @mark-rushakoff

### Bug Fixes

- [#430](https://github.com/influxdata/telegraf/issues/430): Network statistics removed in elasticsearch 2.1. Thanks @jipperinbham!
- [#452](https://github.com/influxdata/telegraf/issues/452): Elasticsearch open file handles error. Thanks @jipperinbham!

## v0.2.4 [2015-12-08]

### Features

- [#412](https://github.com/influxdata/telegraf/pull/412): Additional memcached stats. Thanks @mgresser!
- [#410](https://github.com/influxdata/telegraf/pull/410): Additional redis metrics. Thanks @vlaadbrain!
- [#414](https://github.com/influxdata/telegraf/issues/414): Jolokia plugin auth parameters
- [#415](https://github.com/influxdata/telegraf/issues/415): memcached plugin: support unix sockets
- [#418](https://github.com/influxdata/telegraf/pull/418): memcached plugin additional unit tests.
- [#408](https://github.com/influxdata/telegraf/pull/408): MailChimp plugin.
- [#382](https://github.com/influxdata/telegraf/pull/382): Add system wide network protocol stats to `net` plugin.
- [#401](https://github.com/influxdata/telegraf/pull/401): Support pass/drop/tagpass/tagdrop for outputs. Thanks @oldmantaiter!

### Bug Fixes

- [#405](https://github.com/influxdata/telegraf/issues/405): Prometheus output cardinality issue
- [#388](https://github.com/influxdata/telegraf/issues/388): Fix collection hangup when cpu times decrement.

## v0.2.3 [2015-11-30]

### Release Notes

- **breaking change** The `kafka` plugin has been renamed to `kafka_consumer`.
and most of the config option names have changed.
This only affects the kafka consumer _plugin_ (not the
output). There were a number of problems with the kafka plugin that led to it
only collecting data once at startup, so the kafka plugin was basically non-
functional.
- Plugins can now be specified as a list, and multiple plugin instances of the
same type can be specified, like this:

```toml
[[inputs.cpu]]
  percpu = false
  totalcpu = true

[[inputs.cpu]]
  percpu = true
  totalcpu = false
  drop = ["cpu_time"]
```

- Riemann output added
- Aerospike plugin: tag changed from `host` -> `aerospike_host`

### Features

- [#379](https://github.com/influxdata/telegraf/pull/379): Riemann output, thanks @allenj!
- [#375](https://github.com/influxdata/telegraf/pull/375): kafka_consumer service plugin.
- [#392](https://github.com/influxdata/telegraf/pull/392): Procstat plugin can now accept pgrep -f pattern, thanks @ecarreras!
- [#383](https://github.com/influxdata/telegraf/pull/383): Specify plugins as a list.
- [#354](https://github.com/influxdata/telegraf/pull/354): Add ability to specify multiple metrics in one statsd line. Thanks @MerlinDMC!

### Bug Fixes

- [#371](https://github.com/influxdata/telegraf/issues/371): Kafka consumer plugin not functioning.
- [#389](https://github.com/influxdata/telegraf/issues/389): NaN value panic

## v0.2.2 [2015-11-18]

### Release Notes

- 0.2.1 has a bug where all lists within plugins get duplicated, this includes
lists of servers/URLs. 0.2.2 is being released solely to fix that bug

### Bug Fixes

- [#377](https://github.com/influxdata/telegraf/pull/377): Fix for duplicate slices in inputs.

## v0.2.1 [2015-11-16]

### Release Notes

- Telegraf will no longer use docker-compose for "long" unit test, it has been
changed to just run docker commands in the Makefile. See `make docker-run` and
`make docker-kill`. `make test` will still run all unit tests with docker.
- Long unit tests are now run in CircleCI, with docker & race detector
- Redis plugin tag has changed from `host` to `server`
- HAProxy plugin tag has changed from `host` to `server`
- UDP output now supported
- Telegraf will now compile on FreeBSD
- Users can now specify outputs as lists, specifying multiple outputs of the
same type.

### Features

- [#325](https://github.com/influxdata/telegraf/pull/325): NSQ output. Thanks @jrxFive!
- [#318](https://github.com/influxdata/telegraf/pull/318): Prometheus output. Thanks @oldmantaiter!
- [#338](https://github.com/influxdata/telegraf/pull/338): Restart Telegraf on package upgrade. Thanks @linsomniac!
- [#337](https://github.com/influxdata/telegraf/pull/337): Jolokia plugin, thanks @saiello!
- [#350](https://github.com/influxdata/telegraf/pull/350): Amon output.
- [#365](https://github.com/influxdata/telegraf/pull/365): Twemproxy plugin by @codeb2cc
- [#317](https://github.com/influxdata/telegraf/issues/317): ZFS plugin, thanks @cornerot!
- [#364](https://github.com/influxdata/telegraf/pull/364): Support InfluxDB UDP output.
- [#370](https://github.com/influxdata/telegraf/pull/370): Support specifying multiple outputs, as lists.
- [#372](https://github.com/influxdata/telegraf/pull/372): Remove gosigar and update go-dockerclient for FreeBSD support. Thanks @MerlinDMC!

### Bug Fixes

- [#331](https://github.com/influxdata/telegraf/pull/331): Dont overwrite host tag in redis plugin.
- [#336](https://github.com/influxdata/telegraf/pull/336): Mongodb plugin should take 2 measurements.
- [#351](https://github.com/influxdata/telegraf/issues/317): Fix continual "CREATE DATABASE" in writes
- [#360](https://github.com/influxdata/telegraf/pull/360): Apply prefix before ShouldPass check. Thanks @sotfo!

## v0.2.0 [2015-10-27]

### Release Notes

- The -test flag will now only output 2 collections for plugins that need it
- There is a new agent configuration option: `flush_interval`. This option tells
Telegraf how often to flush data to InfluxDB and other output sinks. For example,
users can set `interval = "2s"` and `flush_interval = "60s"` for Telegraf to
collect data every 2 seconds, and flush every 60 seconds.
- `precision` and `utc` are no longer valid agent config values. `precision` has
moved to the `influxdb` output config, where it will continue to default to "s"
- debug and test output will now print the raw line-protocol string
- Telegraf will now, by default, round the collection interval to the nearest
even interval. This means that `interval="10s"` will collect every :00, :10, etc.
To ease scale concerns, flushing will be "jittered" by a random amount so that
all Telegraf instances do not flush at the same time. Both of these options can
be controlled via the `round_interval` and `flush_jitter` config options.
- Telegraf will now retry metric flushes twice

### Features

- [#205](https://github.com/influxdata/telegraf/issues/205): Include per-db redis keyspace info
- [#226](https://github.com/influxdata/telegraf/pull/226): Add timestamps to points in Kafka/AMQP outputs. Thanks @ekini
- [#90](https://github.com/influxdata/telegraf/issues/90): Add Docker labels to tags in docker plugin
- [#223](https://github.com/influxdata/telegraf/pull/223): Add port tag to nginx plugin. Thanks @neezgee!
- [#227](https://github.com/influxdata/telegraf/pull/227): Add command intervals to exec plugin. Thanks @jpalay!
- [#241](https://github.com/influxdata/telegraf/pull/241): MQTT Output. Thanks @shirou!
- Memory plugin: cached and buffered measurements re-added
- Logging: additional logging for each collection interval, track the number
of metrics collected and from how many inputs.
- [#240](https://github.com/influxdata/telegraf/pull/240): procstat plugin, thanks @ranjib!
- [#244](https://github.com/influxdata/telegraf/pull/244): netstat plugin, thanks @shirou!
- [#262](https://github.com/influxdata/telegraf/pull/262): zookeeper plugin, thanks @jrxFive!
- [#237](https://github.com/influxdata/telegraf/pull/237): statsd service plugin, thanks @sparrc
- [#273](https://github.com/influxdata/telegraf/pull/273): puppet agent plugin, thats @jrxFive!
- [#280](https://github.com/influxdata/telegraf/issues/280): Use InfluxDB client v2.
- [#281](https://github.com/influxdata/telegraf/issues/281): Eliminate need to deep copy Batch Points.
- [#286](https://github.com/influxdata/telegraf/issues/286): bcache plugin, thanks @cornerot!
- [#287](https://github.com/influxdata/telegraf/issues/287): Batch AMQP output, thanks @ekini!
- [#301](https://github.com/influxdata/telegraf/issues/301): Collect on even intervals
- [#298](https://github.com/influxdata/telegraf/pull/298): Support retrying output writes
- [#300](https://github.com/influxdata/telegraf/issues/300): aerospike plugin. Thanks @oldmantaiter!
- [#322](https://github.com/influxdata/telegraf/issues/322): Librato output. Thanks @jipperinbham!

### Bug Fixes

- [#228](https://github.com/influxdata/telegraf/pull/228): New version of package will replace old one. Thanks @ekini!
- [#232](https://github.com/influxdata/telegraf/pull/232): Fix bashism run during deb package installation. Thanks @yankcrime!
- [#261](https://github.com/influxdata/telegraf/issues/260): RabbitMQ panics if wrong credentials given. Thanks @ekini!
- [#245](https://github.com/influxdata/telegraf/issues/245): Document Exec plugin example. Thanks @ekini!
- [#264](https://github.com/influxdata/telegraf/issues/264): logrotate config file fixes. Thanks @linsomniac!
- [#290](https://github.com/influxdata/telegraf/issues/290): Fix some plugins sending their values as strings.
- [#289](https://github.com/influxdata/telegraf/issues/289): Fix accumulator panic on nil tags.
- [#302](https://github.com/influxdata/telegraf/issues/302): Fix `[tags]` getting applied, thanks @gotyaoi!

## v0.1.9 [2015-09-22]

### Release Notes

- InfluxDB output config change: `url` is now `urls`, and is a list. Config files
will still be backwards compatible if only `url` is specified.
- The -test flag will now output two metric collections
- Support for filtering telegraf outputs on the CLI -- Telegraf will now
allow filtering of output sinks on the command-line using the `-outputfilter`
flag, much like how the `-filter` flag works for inputs.
- Support for filtering on config-file creation -- Telegraf now supports
filtering to -sample-config command. You can now run
`telegraf -sample-config -filter cpu -outputfilter influxdb` to get a config
file with only the cpu plugin defined, and the influxdb output defined.
- **Breaking Change**: The CPU collection plugin has been refactored to fix some
bugs and outdated dependency issues. At the same time, I also decided to fix
a naming consistency issue, so cpu_percentageIdle will become cpu_usage_idle.
Also, all CPU time measurements now have it indicated in their name, so cpu_idle
will become cpu_time_idle. Additionally, cpu_time measurements are going to be
dropped in the default config.
- **Breaking Change**: The memory plugin has been refactored and some measurements
have been renamed for consistency. Some measurements have also been removed from being outputted. They are still being collected by gopsutil, and could easily be
re-added in a "verbose" mode if there is demand for it.

### Features

- [#143](https://github.com/influxdata/telegraf/issues/143): InfluxDB clustering support
- [#181](https://github.com/influxdata/telegraf/issues/181): Makefile GOBIN support. Thanks @Vye!
- [#203](https://github.com/influxdata/telegraf/pull/200): AMQP output. Thanks @ekini!
- [#182](https://github.com/influxdata/telegraf/pull/182): OpenTSDB output. Thanks @rplessl!
- [#187](https://github.com/influxdata/telegraf/pull/187): Retry output sink connections on startup.
- [#220](https://github.com/influxdata/telegraf/pull/220): Add port tag to apache plugin. Thanks @neezgee!
- [#217](https://github.com/influxdata/telegraf/pull/217): Add filtering for output sinks
and filtering when specifying a config file.

### Bug Fixes

- [#170](https://github.com/influxdata/telegraf/issues/170): Systemd support
- [#175](https://github.com/influxdata/telegraf/issues/175): Set write precision before gathering metrics
- [#178](https://github.com/influxdata/telegraf/issues/178): redis plugin, multiple server thread hang bug
- Fix net plugin on darwin
- [#84](https://github.com/influxdata/telegraf/issues/84): Fix docker plugin on CentOS. Thanks @neezgee!
- [#189](https://github.com/influxdata/telegraf/pull/189): Fix mem_used_perc. Thanks @mced!
- [#192](https://github.com/influxdata/telegraf/issues/192): Increase compatibility of postgresql plugin. Now supports versions 8.1+
- [#203](https://github.com/influxdata/telegraf/issues/203): EL5 rpm support. Thanks @ekini!
- [#206](https://github.com/influxdata/telegraf/issues/206): CPU steal/guest values wrong on linux.
- [#212](https://github.com/influxdata/telegraf/issues/212): Add hashbang to postinstall script. Thanks @ekini!
- [#212](https://github.com/influxdata/telegraf/issues/212): Fix makefile warning. Thanks @ekini!

## v0.1.8 [2015-09-04]

### Release Notes

- Telegraf will now write data in UTC at second precision by default
- Now using Go 1.5 to build telegraf

### Features

- [#150](https://github.com/influxdata/telegraf/pull/150): Add Host Uptime metric to system plugin
- [#158](https://github.com/influxdata/telegraf/pull/158): Apache Plugin. Thanks @KPACHbIuLLIAnO4
- [#159](https://github.com/influxdata/telegraf/pull/159): Use second precision for InfluxDB writes
- [#165](https://github.com/influxdata/telegraf/pull/165): Add additional metrics to mysql plugin. Thanks @nickscript0
- [#162](https://github.com/influxdata/telegraf/pull/162): Write UTC by default, provide option
- [#166](https://github.com/influxdata/telegraf/pull/166): Upload binaries to S3
- [#169](https://github.com/influxdata/telegraf/pull/169): Ping plugin

### Bug Fixes

## v0.1.7 [2015-08-28]

### Features

- [#38](https://github.com/influxdata/telegraf/pull/38): Kafka output producer.
- [#133](https://github.com/influxdata/telegraf/pull/133): Add plugin.Gather error logging. Thanks @nickscript0!
- [#136](https://github.com/influxdata/telegraf/issues/136): Add a -usage flag for printing usage of a single plugin.
- [#137](https://github.com/influxdata/telegraf/issues/137): Memcached: fix when a value contains a space
- [#138](https://github.com/influxdata/telegraf/issues/138): MySQL server address tag.
- [#142](https://github.com/influxdata/telegraf/pull/142): Add Description and SampleConfig funcs to output interface
- Indent the toml config file for readability

### Bug Fixes

- [#128](https://github.com/influxdata/telegraf/issues/128): system_load measurement missing.
- [#129](https://github.com/influxdata/telegraf/issues/129): Latest pkg url fix.
- [#131](https://github.com/influxdata/telegraf/issues/131): Fix memory reporting on linux & darwin. Thanks @subhachandrachandra!
- [#140](https://github.com/influxdata/telegraf/issues/140): Memory plugin prec->perc typo fix. Thanks @brunoqc!

## v0.1.6 [2015-08-20]

### Features

- [#112](https://github.com/influxdata/telegraf/pull/112): Datadog output. Thanks @jipperinbham!
- [#116](https://github.com/influxdata/telegraf/pull/116): Use godep to vendor all dependencies
- [#120](https://github.com/influxdata/telegraf/pull/120): Httpjson plugin. Thanks @jpalay & @alvaromorales!

### Bug Fixes

- [#113](https://github.com/influxdata/telegraf/issues/113): Update README with Telegraf/InfluxDB compatibility
- [#118](https://github.com/influxdata/telegraf/pull/118): Fix for disk usage stats in Windows. Thanks @srfraser!
- [#122](https://github.com/influxdata/telegraf/issues/122): Fix for DiskUsage segv fault. Thanks @srfraser!
- [#126](https://github.com/influxdata/telegraf/issues/126): Nginx plugin not catching net.SplitHostPort error

## v0.1.5 [2015-08-13]

### Features

- [#54](https://github.com/influxdata/telegraf/pull/54): MongoDB plugin. Thanks @jipperinbham!
- [#55](https://github.com/influxdata/telegraf/pull/55): Elasticsearch plugin. Thanks @brocaar!
- [#71](https://github.com/influxdata/telegraf/pull/71): HAProxy plugin. Thanks @kureikain!
- [#72](https://github.com/influxdata/telegraf/pull/72): Adding TokuDB metrics to MySQL. Thanks vadimtk!
- [#73](https://github.com/influxdata/telegraf/pull/73): RabbitMQ plugin. Thanks @ianunruh!
- [#77](https://github.com/influxdata/telegraf/issues/77): Automatically create database.
- [#79](https://github.com/influxdata/telegraf/pull/56): Nginx plugin. Thanks @codeb2cc!
- [#86](https://github.com/influxdata/telegraf/pull/86): Lustre2 plugin. Thanks srfraser!
- [#91](https://github.com/influxdata/telegraf/pull/91): Unit testing
- [#92](https://github.com/influxdata/telegraf/pull/92): Exec plugin. Thanks @alvaromorales!
- [#98](https://github.com/influxdata/telegraf/pull/98): LeoFS plugin. Thanks @mocchira!
- [#103](https://github.com/influxdata/telegraf/pull/103): Filter by metric tags. Thanks @srfraser!
- [#106](https://github.com/influxdata/telegraf/pull/106): Options to filter plugins on startup. Thanks @zepouet!
- [#107](https://github.com/influxdata/telegraf/pull/107): Multiple outputs beyond influxdb. Thanks @jipperinbham!
- [#108](https://github.com/influxdata/telegraf/issues/108): Support setting per-CPU and total-CPU gathering.
- [#111](https://github.com/influxdata/telegraf/pull/111): Report CPU Usage in cpu plugin. Thanks @jpalay!

### Bug Fixes

- [#85](https://github.com/influxdata/telegraf/pull/85): Fix GetLocalHost testutil function for mac users
- [#89](https://github.com/influxdata/telegraf/pull/89): go fmt fixes
- [#94](https://github.com/influxdata/telegraf/pull/94): Fix for issue #93, explicitly call sarama.v1 -> sarama
- [#101](https://github.com/influxdata/telegraf/issues/101): switch back from master branch if building locally
- [#99](https://github.com/influxdata/telegraf/issues/99): update integer output to new InfluxDB line protocol format

## v0.1.4 [2015-07-09]

### Features

- [#56](https://github.com/influxdata/telegraf/pull/56): Update README for Kafka plugin. Thanks @EmilS!

### Bug Fixes

- [#50](https://github.com/influxdata/telegraf/pull/50): Fix init.sh script to use telegraf directory. Thanks @jseriff!
- [#52](https://github.com/influxdata/telegraf/pull/52): Update CHANGELOG to reference updated directory. Thanks @benfb!

## v0.1.3 [2015-07-05]

### Features

- [#35](https://github.com/influxdata/telegraf/pull/35): Add Kafka plugin. Thanks @EmilS!
- [#47](https://github.com/influxdata/telegraf/pull/47): Add RethinkDB plugin. Thanks @jipperinbham!

### Bug Fixes

- [#45](https://github.com/influxdata/telegraf/pull/45): Skip disk tags that don't have a value. Thanks @jhofeditz!
- [#43](https://github.com/influxdata/telegraf/pull/43): Fix bug in MySQL plugin. Thanks @marcosnils!

## v0.1.2 [2015-07-01]

### Features

- [#12](https://github.com/influxdata/telegraf/pull/12): Add Linux/ARM to the list of built binaries. Thanks @voxxit!
- [#14](https://github.com/influxdata/telegraf/pull/14): Clarify the S3 buckets that Telegraf is pushed to.
- [#16](https://github.com/influxdata/telegraf/pull/16): Convert Redis to use URI, support Redis AUTH. Thanks @jipperinbham!
- [#21](https://github.com/influxdata/telegraf/pull/21): Add memcached plugin. Thanks @Yukki!

### Bug Fixes

- [#13](https://github.com/influxdata/telegraf/pull/13): Fix the packaging script.
- [#19](https://github.com/influxdata/telegraf/pull/19): Add host name to metric tags. Thanks @sherifzain!
- [#20](https://github.com/influxdata/telegraf/pull/20): Fix race condition with accumulator mutex. Thanks @nkatsaros!
- [#23](https://github.com/influxdata/telegraf/pull/23): Change name of folder for packages. Thanks @colinrymer!
- [#32](https://github.com/influxdata/telegraf/pull/32): Fix spelling of memoory -> memory. Thanks @tylernisonoff!

## v0.1.1 [2015-06-19]

### Release Notes

This is the initial release of Telegraf.
