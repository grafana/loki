# Telegraf [![Circle CI](https://circleci.com/gh/influxdata/telegraf.svg?style=svg)](https://circleci.com/gh/influxdata/telegraf) [![Docker pulls](https://img.shields.io/docker/pulls/library/telegraf.svg)](https://hub.docker.com/_/telegraf/)
[![Slack Status](https://img.shields.io/badge/slack-join_chat-white.svg?logo=slack&style=social)](https://www.influxdata.com/slack)

Telegraf is an agent for collecting, processing, aggregating, and writing metrics.

Design goals are to have a minimal memory footprint with a plugin system so
that developers in the community can easily add support for collecting
metrics.

Telegraf is plugin-driven and has the concept of 4 distinct plugin types:

1. [Input Plugins](#input-plugins) collect metrics from the system, services, or 3rd party APIs
2. [Processor Plugins](#processor-plugins) transform, decorate, and/or filter metrics
3. [Aggregator Plugins](#aggregator-plugins) create aggregate metrics (e.g. mean, min, max, quantiles, etc.)
4. [Output Plugins](#output-plugins) write metrics to various destinations

New plugins are designed to be easy to contribute, pull requests are welcomed
and we work to incorporate as many pull requests as possible.

## Try in Browser :rocket:

You can try Telegraf right in your browser in the [Telegraf playground](https://rootnroll.com/d/telegraf/).

## Contributing

There are many ways to contribute:
- Fix and [report bugs](https://github.com/influxdata/telegraf/issues/new)
- [Improve documentation](https://github.com/influxdata/telegraf/issues?q=is%3Aopen+label%3Adocumentation)
- [Review code and feature proposals](https://github.com/influxdata/telegraf/pulls)
- Answer questions and discuss here on github and on the [Community Site](https://community.influxdata.com/)
- [Contribute plugins](CONTRIBUTING.md)
- [Contribute external plugins](https://github.com/influxdata/telegraf/tree/master/plugins/inputs/execd/shim) *(experimental)*

## Minimum Requirements

Telegraf shares the same [minimum requirements][] as Go:
- Linux kernel version 2.6.23 or later
- Windows 7 or later
- FreeBSD 11.2 or later
- MacOS 10.11 El Capitan or later

[minimum requirements]: https://github.com/golang/go/wiki/MinimumRequirements#minimum-requirements

## Installation:

You can download the binaries directly from the [downloads](https://www.influxdata.com/downloads) page
or from the [releases](https://github.com/influxdata/telegraf/releases) section.

### Ansible Role:

Ansible role: https://github.com/rossmcdonald/telegraf

### From Source:

Telegraf requires Go version 1.13 or newer, the Makefile requires GNU make.

1. [Install Go](https://golang.org/doc/install) >=1.13 (1.15 recommended)
2. Clone the Telegraf repository:
   ```
   cd ~/src
   git clone https://github.com/influxdata/telegraf.git
   ```
3. Run `make` from the source directory
   ```
   cd ~/src/telegraf
   make
   ```

### Changelog

View the [changelog](/CHANGELOG.md) for the latest updates and changes by
version.

### Nightly Builds

These builds are generated from the master branch:
- [telegraf-nightly_darwin_amd64.tar.gz](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly_darwin_amd64.tar.gz)
- [telegraf_nightly_amd64.deb](https://dl.influxdata.com/telegraf/nightlies/telegraf_nightly_amd64.deb)
- [telegraf_nightly_arm64.deb](https://dl.influxdata.com/telegraf/nightlies/telegraf_nightly_arm64.deb)
- [telegraf-nightly.arm64.rpm](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly.arm64.rpm)
- [telegraf_nightly_armel.deb](https://dl.influxdata.com/telegraf/nightlies/telegraf_nightly_armel.deb)
- [telegraf-nightly.armel.rpm](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly.armel.rpm)
- [telegraf_nightly_armhf.deb](https://dl.influxdata.com/telegraf/nightlies/telegraf_nightly_armhf.deb)
- [telegraf-nightly.armv6hl.rpm](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly.armv6hl.rpm)
- [telegraf-nightly_freebsd_amd64.tar.gz](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly_freebsd_amd64.tar.gz)
- [telegraf-nightly_freebsd_i386.tar.gz](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly_freebsd_i386.tar.gz)
- [telegraf_nightly_i386.deb](https://dl.influxdata.com/telegraf/nightlies/telegraf_nightly_i386.deb)
- [telegraf-nightly.i386.rpm](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly.i386.rpm)
- [telegraf-nightly_linux_amd64.tar.gz](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly_linux_amd64.tar.gz)
- [telegraf-nightly_linux_arm64.tar.gz](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly_linux_arm64.tar.gz)
- [telegraf-nightly_linux_armel.tar.gz](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly_linux_armel.tar.gz)
- [telegraf-nightly_linux_armhf.tar.gz](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly_linux_armhf.tar.gz)
- [telegraf-nightly_linux_i386.tar.gz](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly_linux_i386.tar.gz)
- [telegraf-nightly_linux_s390x.tar.gz](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly_linux_s390x.tar.gz)
- [telegraf_nightly_s390x.deb](https://dl.influxdata.com/telegraf/nightlies/telegraf_nightly_s390x.deb)
- [telegraf-nightly.s390x.rpm](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly.s390x.rpm)
- [telegraf-nightly_windows_amd64.zip](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly_windows_amd64.zip)
- [telegraf-nightly_windows_i386.zip](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly_windows_i386.zip)
- [telegraf-nightly.x86_64.rpm](https://dl.influxdata.com/telegraf/nightlies/telegraf-nightly.x86_64.rpm)
- [telegraf-static-nightly_linux_amd64.tar.gz](https://dl.influxdata.com/telegraf/nightlies/telegraf-static-nightly_linux_amd64.tar.gz)

## How to use it:

See usage with:

```
telegraf --help
```

#### Generate a telegraf config file:

```
telegraf config > telegraf.conf
```

#### Generate config with only cpu input & influxdb output plugins defined:

```
telegraf --section-filter agent:inputs:outputs --input-filter cpu --output-filter influxdb config
```

#### Run a single telegraf collection, outputting metrics to stdout:

```
telegraf --config telegraf.conf --test
```

#### Run telegraf with all plugins defined in config file:

```
telegraf --config telegraf.conf
```

#### Run telegraf, enabling the cpu & memory input, and influxdb output plugins:

```
telegraf --config telegraf.conf --input-filter cpu:mem --output-filter influxdb
```

## Documentation

[Latest Release Documentation][release docs].

For documentation on the latest development code see the [documentation index][devel docs].

[release docs]: https://docs.influxdata.com/telegraf
[devel docs]: docs

## Input Plugins

* [activemq](./plugins/inputs/activemq)
* [aerospike](./plugins/inputs/aerospike)
* [amqp_consumer](./plugins/inputs/amqp_consumer) (rabbitmq)
* [apache](./plugins/inputs/apache)
* [apcupsd](./plugins/inputs/apcupsd)
* [aurora](./plugins/inputs/aurora)
* [aws cloudwatch](./plugins/inputs/cloudwatch) (Amazon Cloudwatch)
* [azure_storage_queue](./plugins/inputs/azure_storage_queue)
* [bcache](./plugins/inputs/bcache)
* [beanstalkd](./plugins/inputs/beanstalkd)
* [bind](./plugins/inputs/bind)
* [bond](./plugins/inputs/bond)
* [burrow](./plugins/inputs/burrow)
* [cassandra](./plugins/inputs/cassandra) (deprecated, use [jolokia2](./plugins/inputs/jolokia2))
* [ceph](./plugins/inputs/ceph)
* [cgroup](./plugins/inputs/cgroup)
* [chrony](./plugins/inputs/chrony)
* [cisco_telemetry_gnmi](./plugins/inputs/cisco_telemetry_gnmi) (deprecated, renamed to [gnmi](/plugins/inputs/gnmi))
* [cisco_telemetry_mdt](./plugins/inputs/cisco_telemetry_mdt)
* [clickhouse](./plugins/inputs/clickhouse)
* [cloud_pubsub](./plugins/inputs/cloud_pubsub) Google Cloud Pub/Sub
* [cloud_pubsub_push](./plugins/inputs/cloud_pubsub_push) Google Cloud Pub/Sub push endpoint
* [conntrack](./plugins/inputs/conntrack)
* [consul](./plugins/inputs/consul)
* [couchbase](./plugins/inputs/couchbase)
* [couchdb](./plugins/inputs/couchdb)
* [cpu](./plugins/inputs/cpu)
* [DC/OS](./plugins/inputs/dcos)
* [diskio](./plugins/inputs/diskio)
* [disk](./plugins/inputs/disk)
* [disque](./plugins/inputs/disque)
* [dmcache](./plugins/inputs/dmcache)
* [dns query time](./plugins/inputs/dns_query)
* [docker](./plugins/inputs/docker)
* [docker_log](./plugins/inputs/docker_log)
* [dovecot](./plugins/inputs/dovecot)
* [aws ecs](./plugins/inputs/ecs) (Amazon Elastic Container Service, Fargate)
* [elasticsearch](./plugins/inputs/elasticsearch)
* [ethtool](./plugins/inputs/ethtool)
* [eventhub_consumer](./plugins/inputs/eventhub_consumer) (Azure Event Hubs \& Azure IoT Hub)
* [exec](./plugins/inputs/exec) (generic executable plugin, support JSON, influx, graphite and nagios)
* [execd](./plugins/inputs/execd) (generic executable "daemon" processes)
* [fail2ban](./plugins/inputs/fail2ban)
* [fibaro](./plugins/inputs/fibaro)
* [file](./plugins/inputs/file)
* [filestat](./plugins/inputs/filestat)
* [filecount](./plugins/inputs/filecount)
* [fireboard](/plugins/inputs/fireboard)
* [fluentd](./plugins/inputs/fluentd)
* [github](./plugins/inputs/github)
* [gnmi](./plugins/inputs/gnmi)
* [graylog](./plugins/inputs/graylog)
* [haproxy](./plugins/inputs/haproxy)
* [hddtemp](./plugins/inputs/hddtemp)
* [httpjson](./plugins/inputs/httpjson) (generic JSON-emitting http service plugin)
* [http_listener](./plugins/inputs/influxdb_listener) (deprecated, renamed to [influxdb_listener](/plugins/inputs/influxdb_listener))
* [http_listener_v2](./plugins/inputs/http_listener_v2)
* [http](./plugins/inputs/http) (generic HTTP plugin, supports using input data formats)
* [http_response](./plugins/inputs/http_response)
* [icinga2](./plugins/inputs/icinga2)
* [infiniband](./plugins/inputs/infiniband)
* [influxdb](./plugins/inputs/influxdb)
* [influxdb_listener](./plugins/inputs/influxdb_listener)
* [influxdb_v2_listener](./plugins/inputs/influxdb_v2_listener)
* [intel_rdt](./plugins/inputs/intel_rdt)
* [internal](./plugins/inputs/internal)
* [interrupts](./plugins/inputs/interrupts)
* [ipmi_sensor](./plugins/inputs/ipmi_sensor)
* [ipset](./plugins/inputs/ipset)
* [iptables](./plugins/inputs/iptables)
* [ipvs](./plugins/inputs/ipvs)
* [jenkins](./plugins/inputs/jenkins)
* [jolokia2](./plugins/inputs/jolokia2) (java, cassandra, kafka)
* [jolokia](./plugins/inputs/jolokia) (deprecated, use [jolokia2](./plugins/inputs/jolokia2))
* [jti_openconfig_telemetry](./plugins/inputs/jti_openconfig_telemetry)
* [kafka_consumer](./plugins/inputs/kafka_consumer)
* [kapacitor](./plugins/inputs/kapacitor)
* [aws kinesis](./plugins/inputs/kinesis_consumer) (Amazon Kinesis)
* [kernel](./plugins/inputs/kernel)
* [kernel_vmstat](./plugins/inputs/kernel_vmstat)
* [kibana](./plugins/inputs/kibana)
* [kubernetes](./plugins/inputs/kubernetes)
* [kube_inventory](./plugins/inputs/kube_inventory)
* [lanz](./plugins/inputs/lanz)
* [leofs](./plugins/inputs/leofs)
* [linux_sysctl_fs](./plugins/inputs/linux_sysctl_fs)
* [logparser](./plugins/inputs/logparser) (deprecated, use [tail](/plugins/inputs/tail))
* [logstash](./plugins/inputs/logstash)
* [lustre2](./plugins/inputs/lustre2)
* [mailchimp](./plugins/inputs/mailchimp)
* [marklogic](./plugins/inputs/marklogic)
* [mcrouter](./plugins/inputs/mcrouter)
* [memcached](./plugins/inputs/memcached)
* [mem](./plugins/inputs/mem)
* [mesos](./plugins/inputs/mesos)
* [minecraft](./plugins/inputs/minecraft)
* [modbus](./plugins/inputs/modbus)
* [mongodb](./plugins/inputs/mongodb)
* [monit](./plugins/inputs/monit)
* [mqtt_consumer](./plugins/inputs/mqtt_consumer)
* [multifile](./plugins/inputs/multifile)
* [mysql](./plugins/inputs/mysql)
* [nats_consumer](./plugins/inputs/nats_consumer)
* [nats](./plugins/inputs/nats)
* [neptune_apex](./plugins/inputs/neptune_apex)
* [net](./plugins/inputs/net)
* [net_response](./plugins/inputs/net_response)
* [netstat](./plugins/inputs/net)
* [nginx](./plugins/inputs/nginx)
* [nginx_plus_api](./plugins/inputs/nginx_plus_api)
* [nginx_plus](./plugins/inputs/nginx_plus)
* [nginx_sts](./plugins/inputs/nginx_sts)
* [nginx_upstream_check](./plugins/inputs/nginx_upstream_check)
* [nginx_vts](./plugins/inputs/nginx_vts)
* [nsd](./plugins/inputs/nsd)
* [nsq_consumer](./plugins/inputs/nsq_consumer)
* [nsq](./plugins/inputs/nsq)
* [nstat](./plugins/inputs/nstat)
* [ntpq](./plugins/inputs/ntpq)
* [nvidia_smi](./plugins/inputs/nvidia_smi)
* [opcua](./plugins/inputs/opcua)
* [openldap](./plugins/inputs/openldap)
* [openntpd](./plugins/inputs/openntpd)
* [opensmtpd](./plugins/inputs/opensmtpd)
* [openweathermap](./plugins/inputs/openweathermap)
* [pf](./plugins/inputs/pf)
* [pgbouncer](./plugins/inputs/pgbouncer)
* [phpfpm](./plugins/inputs/phpfpm)
* [phusion passenger](./plugins/inputs/passenger)
* [ping](./plugins/inputs/ping)
* [postfix](./plugins/inputs/postfix)
* [postgresql_extensible](./plugins/inputs/postgresql_extensible)
* [postgresql](./plugins/inputs/postgresql)
* [powerdns](./plugins/inputs/powerdns)
* [powerdns_recursor](./plugins/inputs/powerdns_recursor)
* [processes](./plugins/inputs/processes)
* [procstat](./plugins/inputs/procstat)
* [prometheus](./plugins/inputs/prometheus) (can be used for [Caddy server](./plugins/inputs/prometheus/README.md#usage-for-caddy-http-server))
* [proxmox](./plugins/inputs/proxmox)
* [puppetagent](./plugins/inputs/puppetagent)
* [rabbitmq](./plugins/inputs/rabbitmq)
* [raindrops](./plugins/inputs/raindrops)
* [ras](./plugins/inputs/ras)
* [redfish](./plugins/inputs/redfish)
* [redis](./plugins/inputs/redis)
* [rethinkdb](./plugins/inputs/rethinkdb)
* [riak](./plugins/inputs/riak)
* [salesforce](./plugins/inputs/salesforce)
* [sensors](./plugins/inputs/sensors)
* [sflow](./plugins/inputs/sflow)
* [smart](./plugins/inputs/smart)
* [snmp_legacy](./plugins/inputs/snmp_legacy)
* [snmp](./plugins/inputs/snmp)
* [snmp_trap](./plugins/inputs/snmp_trap)
* [socket_listener](./plugins/inputs/socket_listener)
* [solr](./plugins/inputs/solr)
* [sql server](./plugins/inputs/sqlserver) (microsoft)
* [stackdriver](./plugins/inputs/stackdriver) (Google Cloud Monitoring)
* [statsd](./plugins/inputs/statsd)
* [suricata](./plugins/inputs/suricata)
* [swap](./plugins/inputs/swap)
* [synproxy](./plugins/inputs/synproxy)
* [syslog](./plugins/inputs/syslog)
* [sysstat](./plugins/inputs/sysstat)
* [systemd_units](./plugins/inputs/systemd_units)
* [system](./plugins/inputs/system)
* [tail](./plugins/inputs/tail)
* [temp](./plugins/inputs/temp)
* [tcp_listener](./plugins/inputs/socket_listener)
* [teamspeak](./plugins/inputs/teamspeak)
* [tengine](./plugins/inputs/tengine)
* [tomcat](./plugins/inputs/tomcat)
* [twemproxy](./plugins/inputs/twemproxy)
* [udp_listener](./plugins/inputs/socket_listener)
* [unbound](./plugins/inputs/unbound)
* [uwsgi](./plugins/inputs/uwsgi)
* [varnish](./plugins/inputs/varnish)
* [vsphere](./plugins/inputs/vsphere) VMware vSphere
* [webhooks](./plugins/inputs/webhooks)
  * [filestack](./plugins/inputs/webhooks/filestack)
  * [github](./plugins/inputs/webhooks/github)
  * [mandrill](./plugins/inputs/webhooks/mandrill)
  * [papertrail](./plugins/inputs/webhooks/papertrail)
  * [particle](./plugins/inputs/webhooks/particle)
  * [rollbar](./plugins/inputs/webhooks/rollbar)
* [win_eventlog](./plugins/inputs/win_eventlog)
* [win_perf_counters](./plugins/inputs/win_perf_counters) (windows performance counters)
* [win_services](./plugins/inputs/win_services)
* [wireguard](./plugins/inputs/wireguard)
* [wireless](./plugins/inputs/wireless)
* [x509_cert](./plugins/inputs/x509_cert)
* [zfs](./plugins/inputs/zfs)
* [zipkin](./plugins/inputs/zipkin)
* [zookeeper](./plugins/inputs/zookeeper)

## Parsers

- [InfluxDB Line Protocol](/plugins/parsers/influx)
- [Collectd](/plugins/parsers/collectd)
- [CSV](/plugins/parsers/csv)
- [Dropwizard](/plugins/parsers/dropwizard)
- [FormUrlencoded](/plugins/parser/form_urlencoded)
- [Graphite](/plugins/parsers/graphite)
- [Grok](/plugins/parsers/grok)
- [JSON](/plugins/parsers/json)
- [Logfmt](/plugins/parsers/logfmt)
- [Nagios](/plugins/parsers/nagios)
- [Value](/plugins/parsers/value), ie: 45 or "booyah"
- [Wavefront](/plugins/parsers/wavefront)

## Serializers

- [InfluxDB Line Protocol](/plugins/serializers/influx)
- [JSON](/plugins/serializers/json)
- [Graphite](/plugins/serializers/graphite)
- [ServiceNow](/plugins/serializers/nowmetric)
- [SplunkMetric](/plugins/serializers/splunkmetric)
- [Carbon2](/plugins/serializers/carbon2)
- [Wavefront](/plugins/serializers/wavefront)

## Processor Plugins

* [clone](/plugins/processors/clone)
* [converter](/plugins/processors/converter)
* [date](/plugins/processors/date)
* [dedup](/plugins/processors/dedup)
* [defaults](/plugins/processors/defaults)
* [enum](/plugins/processors/enum)
* [execd](/plugins/processors/execd)
* [ifname](/plugins/processors/ifname)
* [filepath](/plugins/processors/filepath)
* [override](/plugins/processors/override)
* [parser](/plugins/processors/parser)
* [pivot](/plugins/processors/pivot)
* [port_name](/plugins/processors/port_name)
* [printer](/plugins/processors/printer)
* [regex](/plugins/processors/regex)
* [rename](/plugins/processors/rename)
* [reverse_dns](/plugins/processors/reverse_dns)
* [s2geo](/plugins/processors/s2geo)
* [starlark](/plugins/processors/starlark)
* [strings](/plugins/processors/strings)
* [tag_limit](/plugins/processors/tag_limit)
* [template](/plugins/processors/template)
* [topk](/plugins/processors/topk)
* [unpivot](/plugins/processors/unpivot)

## Aggregator Plugins

* [basicstats](./plugins/aggregators/basicstats)
* [final](./plugins/aggregators/final)
* [histogram](./plugins/aggregators/histogram)
* [merge](./plugins/aggregators/merge)
* [minmax](./plugins/aggregators/minmax)
* [valuecounter](./plugins/aggregators/valuecounter)

## Output Plugins

* [influxdb](./plugins/outputs/influxdb) (InfluxDB 1.x)
* [influxdb_v2](./plugins/outputs/influxdb_v2) ([InfluxDB 2.x](https://github.com/influxdata/influxdb))
* [amon](./plugins/outputs/amon)
* [amqp](./plugins/outputs/amqp) (rabbitmq)
* [application_insights](./plugins/outputs/application_insights)
* [aws kinesis](./plugins/outputs/kinesis)
* [aws cloudwatch](./plugins/outputs/cloudwatch)
* [azure_monitor](./plugins/outputs/azure_monitor)
* [cloud_pubsub](./plugins/outputs/cloud_pubsub) Google Cloud Pub/Sub
* [cratedb](./plugins/outputs/cratedb)
* [datadog](./plugins/outputs/datadog)
* [discard](./plugins/outputs/discard)
* [dynatrace](./plugins/outputs/dynatrace)
* [elasticsearch](./plugins/outputs/elasticsearch)
* [exec](./plugins/outputs/exec)
* [execd](./plugins/outputs/execd)
* [file](./plugins/outputs/file)
* [graphite](./plugins/outputs/graphite)
* [graylog](./plugins/outputs/graylog)
* [health](./plugins/outputs/health)
* [http](./plugins/outputs/http)
* [instrumental](./plugins/outputs/instrumental)
* [kafka](./plugins/outputs/kafka)
* [librato](./plugins/outputs/librato)
* [mqtt](./plugins/outputs/mqtt)
* [nats](./plugins/outputs/nats)
* [newrelic](./plugins/outputs/newrelic)
* [nsq](./plugins/outputs/nsq)
* [opentsdb](./plugins/outputs/opentsdb)
* [prometheus](./plugins/outputs/prometheus_client)
* [riemann](./plugins/outputs/riemann)
* [riemann_legacy](./plugins/outputs/riemann_legacy)
* [socket_writer](./plugins/outputs/socket_writer)
* [stackdriver](./plugins/outputs/stackdriver) (Google Cloud Monitoring)
* [syslog](./plugins/outputs/syslog)
* [tcp](./plugins/outputs/socket_writer)
* [udp](./plugins/outputs/socket_writer)
* [warp10](./plugins/outputs/warp10)
* [wavefront](./plugins/outputs/wavefront)
* [sumologic](./plugins/outputs/sumologic)
