{
  local setupGossipRing(storeOption, consulHostnameOption, multiStoreOptionsPrefix) = if $._config.multikv_migration_enabled then {
    [storeOption]: 'multi',
    [multiStoreOptionsPrefix + '.primary']: $._config.multikv_primary,
    [multiStoreOptionsPrefix + '.secondary']: $._config.multikv_secondary,
    // don't remove consul.hostname, it may still be needed.
  } else {
    [storeOption]: 'memberlist',
    [consulHostnameOption]: null,
  },

  _config+:: {
    gossip_member_label: 'loki_gossip_member',
    service_ignored_labels:: [$._config.gossip_member_label],

    // Enables use of memberlist for all rings, instead of consul. If multikv_migration_enabled is true, consul hostname is still configured,
    // but "primary" KV depends on value of multikv_primary.
    memberlist_ring_enabled: false,

    // Configures the memberlist cluster label. When verification is enabled, a memberlist member rejects any packet or stream
    // with a mismatching cluster label.
    // Cluster label and verification flag can be set here or directly to
    // `_config.loki.memberlist.cluster_label` and `_config.loki.memberlist.cluster_label_verification_disabled` respectively.
    // Retaining this config to keep it backwards compatible with 2.6.1 release.
    memberlist_cluster_label: '%(cluster)s.%(namespace)s' % self,
    memberlist_cluster_label_verification_disabled: false,

    // Migrating from consul to memberlist is a multi-step process:
    // 1) Enable multikv_migration_enabled, with primary=consul, secondary=memberlist, and multikv_mirror_enabled=false, restart components.
    // 2) Set multikv_mirror_enabled=true. This doesn't require restart.
    // 3) Swap multikv_primary and multikv_secondary, ie. multikv_primary=memberlist, multikv_secondary=consul. This doesn't require restart.
    // 4) Set multikv_migration_enabled=false and multikv_migration_teardown=true. This requires a restart, but components will now use only memberlist.
    // 5) Set multikv_migration_teardown=false. This doesn't require a restart.
    multikv_migration_enabled: false,
    multikv_migration_teardown: false,
    multikv_primary: 'consul',
    multikv_secondary: 'memberlist',
    multikv_switch_primary_secondary: false,
    multikv_mirror_enabled: false,

    multi_kv_obj: if $._config.multikv_migration_enabled then {
      store: 'multi',
      multi: {
        primary: $._config.multikv_primary,
        secondary: $._config.multikv_secondary,
      },
    } else {
      store: 'memberlist',
    },

    loki+: if $._config.memberlist_ring_enabled then {
      ingester+: {
        lifecycler+: {
          ring+: {
            kvstore+: $._config.multi_kv_obj,
          },
        },
      },

      distributor+: {
        ring+: {
          kvstore+: $._config.multi_kv_obj,
        },
      },

      ruler+: if !$._config.ruler_enabled then {} else {
        ring+: {
          kvstore+: $._config.multi_kv_obj,
        },
      },

      // other components pick this up from common
      common+: {
        ring+: {
          kvstore+: $._config.multi_kv_obj,
        },
      },

      memberlist: {
        abort_if_cluster_join_fails: false,

        // Expose this port on all distributor, ingester
        // and querier replicas.
        bind_port: gossipRingPort,

        // You can use a headless k8s service for all distributor,
        // ingester and querier components.
        join_members: ['dns+gossip-ring.%s.svc.cluster.local:%d' % [$._config.namespace, gossipRingPort]],

        max_join_backoff: '1m',
        max_join_retries: 10,
        min_join_backoff: '1s',

        cluster_label: $._config.memberlist_cluster_label,
        cluster_label_verification_disabled: $._config.memberlist_cluster_label_verification_disabled,
      },
    } else {},

    // When doing migration via multi KV store, this section can be used
    // to configure runtime parameters of multi KV store
    multi_kv_config: if !$._config.multikv_migration_enabled && !$._config.multikv_migration_teardown then {} else {
      primary: if $._config.multikv_switch_primary_secondary then $._config.multikv_secondary else $._config.multikv_primary,
      mirror_enabled: $._config.multikv_mirror_enabled,
    },
  },

  local gossipRingPort = 7946,

  local containerPort = $.core.v1.containerPort,
  local gossipPort = containerPort.newNamed(name='gossip-ring', containerPort=gossipRingPort),

  compactor_ports+:: if !$._config.memberlist_ring_enabled then [] else [gossipPort],
  distributor_ports+:: if !$._config.memberlist_ring_enabled then [] else [gossipPort],
  index_gateway_ports+:: if !$._config.memberlist_ring_enabled then [] else [gossipPort],
  ingester_ports+:: if !$._config.memberlist_ring_enabled then [] else [gossipPort],
  query_scheduler_ports+:: if !$._config.memberlist_ring_enabled then [] else [gossipPort],
  ruler_ports+:: if !$._config.memberlist_ring_enabled then [] else [gossipPort],

  // Don't add label to matcher, only to pod labels.
  local gossipLabel = $.apps.v1.statefulSet.spec.template.metadata.withLabelsMixin({ [$._config.gossip_member_label]: 'true' }),

  compactor_statefulset+: if !$._config.memberlist_ring_enabled then {} else gossipLabel,
  distributor_deployment+: if !$._config.memberlist_ring_enabled then {} else gossipLabel,
  index_gateway_statefulset+: if !$._config.memberlist_ring_enabled || !$._config.use_index_gateway then {} else gossipLabel,
  ingester_statefulset: if $._config.multi_zone_ingester_enabled && !$._config.multi_zone_ingester_migration_enabled then {} else
    (super.ingester_statefulset + if !$._config.memberlist_ring_enabled then {} else gossipLabel),
  ingester_zone_a_statefulset+:
    if $._config.multi_zone_ingester_enabled && $._config.memberlist_ring_enabled
    then gossipLabel
    else {},
  ingester_zone_b_statefulset+:
    if $._config.multi_zone_ingester_enabled && $._config.memberlist_ring_enabled
    then gossipLabel
    else {},
  ingester_zone_c_statefulset+:
    if $._config.multi_zone_ingester_enabled && $._config.memberlist_ring_enabled
    then gossipLabel
    else {},
  query_scheduler_deployment+: if !$._config.memberlist_ring_enabled || !$._config.query_scheduler_enabled then {} else gossipLabel,
  ruler_deployment+: if !$._config.memberlist_ring_enabled || !$._config.ruler_enabled || $._config.stateful_rulers then {} else gossipLabel,
  ruler_statefulset+: if !$._config.memberlist_ring_enabled || !$._config.ruler_enabled || !$._config.stateful_rulers then {} else gossipLabel,
  // Headless service (= no assigned IP, DNS returns all targets instead) pointing to gossip network members.
  gossip_ring_service:
    if !$._config.memberlist_ring_enabled then null
    else
      local service = $.core.v1.service;
      local servicePort = $.core.v1.servicePort;

      local ports = [
        servicePort.newNamed('gossip-ring', gossipRingPort, gossipRingPort) +
        servicePort.withProtocol('TCP'),
      ];
      service.new(
        'gossip-ring',  // name
        { [$._config.gossip_member_label]: 'true' },  // point to all gossip members
        ports,
      ) + service.mixin.spec.withClusterIp('None'),  // headless service

  // Disable the consul deployment if not migrating and using memberlist
  consul_deployment: if $._config.memberlist_ring_enabled && !$._config.multikv_migration_enabled && !$._config.multikv_migration_teardown then {} else super.consul_deployment,
  consul_service: if $._config.memberlist_ring_enabled && !$._config.multikv_migration_enabled && !$._config.multikv_migration_teardown then {} else super.consul_service,
  consul_config_map: if $._config.memberlist_ring_enabled && !$._config.multikv_migration_enabled && !$._config.multikv_migration_teardown then {} else super.consul_config_map,
  consul_sidekick_rbac: if $._config.memberlist_ring_enabled && !$._config.multikv_migration_enabled && !$._config.multikv_migration_teardown then {} else super.consul_sidekick_rbac,
}
