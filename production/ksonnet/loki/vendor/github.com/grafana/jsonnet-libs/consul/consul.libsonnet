local k = import 'ksonnet-util/kausal.libsonnet';

k {
  _config+:: {
    consul_replicas: 3,
  },

  _images+:: {
    consul: 'consul:1.5.3',
    consulSidekick: 'weaveworks/consul-sidekick:master-f18ad13',
    statsdExporter: 'prom/statsd-exporter:v0.12.2',
    consulExporter: 'prom/consul-exporter:v0.5.0',
  },

  local configMap = $.core.v1.configMap,

  consul_config:: {
    telemetry: {
      dogstatsd_addr: '127.0.0.1:9125',
    },
    leave_on_terminate: true,
  },

  consul_config_map:
    configMap.new('consul') +
    configMap.withData({
      'consul-config.json': std.toString($.consul_config),
      mapping: |||
        mappings:
        - match: consul.*.runtime.*
          name: consul_runtime
          labels:
            type: $2
        - match: consul.runtime.total_gc_pause_ns
          name: consul_runtime_total_gc_pause_ns
          labels:
            type: $2
        - match: consul.consul.health.service.query-tag.*.*.*
          name: consul_health_service_query_tag
          labels:
            query: $1.$2.$3
        - match: consul.consul.health.service.query-tag.*.*.*.*
          name: consul_health_service_query_tag
          labels:
            query: $1.$2.$3.$4
        - match: consul.consul.health.service.query-tag.*.*.*.*.*
          name: consul_health_service_query_tag
          labels:
            query: $1.$2.$3.$4.$5
        - match: consul.consul.health.service.query-tag.*.*.*.*.*.*
          name: consul_health_service_query_tag
          labels:
            query: $1.$2.$3.$4.$5.$6
        - match: consul.consul.health.service.query-tag.*.*.*.*.*.*.*
          name: consul_health_service_query_tag
          labels:
            query: $1.$2.$3.$4.$5.$6.$7
        - match: consul.consul.health.service.query-tag.*.*.*.*.*.*.*.*
          name: consul_health_service_query_tag
          labels:
            query: $1.$2.$3.$4.$5.$6.$7.$8
        - match: consul.consul.health.service.query-tag.*.*.*.*.*.*.*.*.*
          name: consul_health_service_query_tag
          labels:
            query: $1.$2.$3.$4.$5.$6.$7.$8.$9
        - match: consul.consul.health.service.query-tag.*.*.*.*.*.*.*.*.*.*
          name: consul_health_service_query_tag
          labels:
            query: $1.$2.$3.$4.$5.$6.$7.$8.$9.$10
        - match: consul.consul.health.service.query-tag.*.*.*.*.*.*.*.*.*.*.*
          name: consul_health_service_query_tag
          labels:
            query: $1.$2.$3.$4.$5.$6.$7.$8.$9.$10.$11
        - match: consul.consul.health.service.query-tag.*.*.*.*.*.*.*.*.*.*.*.*
          name: consul_health_service_query_tag
          labels:
            query: $1.$2.$3.$4.$5.$6.$7.$8.$9.$10.$11.$12
        - match: consul.consul.catalog.deregister
          name: consul_catalog_deregister
          labels: {}
        - match: consul.consul.dns.domain_query.*.*.*.*.*
          name: consul_dns_domain_query
          labels:
            query: $1.$2.$3.$4.$5
        - match: consul.consul.health.service.not-found.*
          name: consul_health_service_not_found
          labels:
            query: $1
        - match: consul.consul.health.service.query.*
          name: consul_health_service_query
          labels:
            query: $1
        - match: consul.*.memberlist.health.score
          name: consul_memberlist_health_score
          labels: {}
        - match: consul.serf.queue.*
          name: consul_serf_events
          labels:
            type: $1
        - match: consul.serf.snapshot.appendLine
          name: consul_serf_snapshot_appendLine
          labels:
            type: $1
        - match: consul.serf.coordinate.adjustment-ms
          name: consul_serf_coordinate_adjustment_ms
          labels: {}
        - match: consul.consul.rpc.query
          name: consul_rpc_query
          labels: {}
        - match: consul.*.consul.session_ttl.active
          name: consul_session_ttl_active
          labels: {}
        - match: consul.raft.rpc.*
          name: consul_raft_rpc
          labels:
            type: $1
        - match: consul.raft.rpc.appendEntries.storeLogs
          name: consul_raft_rpc_appendEntries_storeLogs
          labels:
            type: $1
        - match: consul.consul.fsm.persist
          name: consul_fsm_persist
          labels: {}
        - match: consul.raft.fsm.apply
          name: consul_raft_fsm_apply
          labels: {}
        - match: consul.raft.leader.lastContact
          name: consul_raft_leader_lastcontact
          labels: {}
        - match: consul.raft.leader.dispatchLog
          name: consul_raft_leader_dispatchLog
          labels: {}
        - match: consul.raft.commitTime
          name: consul_raft_commitTime
          labels: {}
        - match: consul.raft.replication.appendEntries.logs.*.*.*.*
          name: consul_raft_replication_appendEntries_logs
          labels:
            query: ${1}.${2}.${3}.${4}
        - match: consul.raft.replication.appendEntries.rpc.*.*.*.*
          name: consul_raft_replication_appendEntries_rpc
          labels:
            query: ${1}.${2}.${3}.${4}
        - match: consul.raft.replication.heartbeat.*.*.*.*
          name: consul_raft_replication_heartbeat
          labels:
            query: ${1}.${2}.${3}.${4}
        - match: consul.consul.rpc.request
          name: consul_rpc_requests
          labels: {}
        - match: consul.consul.rpc.accept_conn
          name: consul_rpc_accept_conn
          labels: {}
        - match: consul.memberlist.udp.*
          name: consul_memberlist_udp
          labels:
            type: $1
        - match: consul.memberlist.tcp.*
          name: consul_memberlist_tcp
          labels:
            type: $1
        - match: consul.memberlist.gossip
          name: consul_memberlist_gossip
          labels: {}
        - match: consul.memberlist.probeNode
          name: consul_memberlist_probenode
          labels: {}
        - match: consul.memberlist.pushPullNode
          name: consul_memberlist_pushpullnode
          labels: {}
        - match: consul.http.*
          name: consul_http_request
          labels:
            method: $1
            path: /
        - match: consul.http.*.*
          name: consul_http_request
          labels:
            method: $1
            path: /$2
        - match: consul.http.*.*.*
          name: consul_http_request
          labels:
            method: $1
            path: /$2/$3
        - match: consul.http.*.*.*.*
          name: consul_http_request
          labels:
            method: $1
            path: /$2/$3/$4
        - match: consul.http.*.*.*.*.*
          name: consul_http_request
          labels:
            method: $1
            path: /$2/$3/$4/$5
        - match: consul.consul.leader.barrier
          name: consul_leader_barrier
          labels: {}
        - match: consul.consul.leader.reconcileMember
          name: consul_leader_reconcileMember
          labels: {}
        - match: consul.consul.leader.reconcile
          name: consul_leader_reconcile
          labels: {}
        - match: consul.consul.fsm.coordinate.batch-update
          name: consul_fsm_coordinate_batch_update
          labels: {}
        - match: consul.consul.fsm.autopilot
          name: consul_fsm_autopilot
          labels: {}
        - match: consul.consul.fsm.kvs.cas
          name: consul_fsm_kvs_cas
          labels: {}
        - match: consul.consul.fsm.register
          name: consul_fsm_register
          labels: {}
        - match: consul.consul.fsm.deregister
          name: consul_fsm_deregister
          labels: {}
        - match: consul.consul.fsm.tombstone.reap
          name: consul_fsm_tombstone_reap
          labels: {}
        - match: consul.consul.catalog.register
          name: consul_catalog_register
          labels: {}
        - match: consul.consul.catalog.deregister
          name: consul_catalog_deregister
          labels: {}
        - match: consul.consul.leader.reapTombstones
          name: consul_leader_reapTombstones
          labels: {}
      |||,
    }),

  local container = $.core.v1.container,
  local containerPort = $.core.v1.containerPort,

  consul_container::
    container.new('consul', $._images.consul) +
    container.withArgs([
      'agent',
      '-ui',
      '-server',
      '-client=0.0.0.0',
      '-config-file=/etc/config/consul-config.json',
      '-bootstrap-expect=%d' % $._config.consul_replicas,
    ]) +
    container.withEnvMap({
      CHECKPOINT_DISABLE: '1',
    }) +
    container.withPorts([
      containerPort.new('server', 8300),
      containerPort.new('serf', 8301),
      containerPort.new('client', 8400),
      containerPort.new('api', 8500),
    ]) +
    $.util.resourcesRequests('100m', '500Mi'),

  local policyRule = $.rbac.v1.policyRule,

  consul_sidekick_rbac:
    $.util.namespacedRBAC('consul-sidekick', [
      policyRule.withApiGroups(['', 'extensions', 'apps']) +
      policyRule.withResources(['pods', 'replicasets']) +
      policyRule.withVerbs(['get', 'list', 'watch']),
    ]),

  consul_sidekick_container::
    container.new('sidekick', $._images.consulSidekick) +
    container.withArgs([
      '--namespace=$(POD_NAMESPACE)',
      '--pod-name=$(POD_NAME)',
    ]) +
    container.withEnv([
      $.core.v1.envVar.fromFieldPath('POD_NAMESPACE', 'metadata.namespace'),
      $.core.v1.envVar.fromFieldPath('POD_NAME', 'metadata.name'),
    ]),

  consul_statsd_exporter::
    container.new('statsd-exporter', $._images.statsdExporter) +
    container.withArgs([
      '--web.listen-address=:8000',
      '--statsd.mapping-config=/etc/config/mapping',
    ]) +
    container.withPorts(containerPort.new('http-metrics', 8000)),

  consul_exporter::
    container.new('consul-exporter', $._images.consulExporter) +
    container.withArgs([
      '--consul.server=localhost:8500',
      '--web.listen-address=:9107',
      '--consul.timeout=1s',
    ]) +
    container.withPorts(containerPort.new('http-metrics', 9107)),

  local deployment = $.apps.v1.deployment,

  consul_deployment:
    deployment.new('consul', $._config.consul_replicas, [
      $.consul_container,
      $.consul_sidekick_container,
      $.consul_statsd_exporter,
      $.consul_exporter,
    ]) +
    deployment.spec.template.spec.withServiceAccount('consul-sidekick') +
    $.util.configMapVolumeMount($.consul_config_map, '/etc/config') +
    $.util.antiAffinity,

  consul_service:
    $.util.serviceFor($.consul_deployment),
}
