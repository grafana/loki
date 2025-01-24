local rolloutOperator = import 'rollout-operator.libsonnet';

{
  local container = $.core.v1.container,
  local deployment = $.apps.v1.deployment,
  local statefulSet = $.apps.v1.statefulSet,
  local topologySpreadConstraints = $.core.v1.topologySpreadConstraint,
  local podDisruptionBudget = $.policy.v1.podDisruptionBudget,
  local volume = $.core.v1.volume,
  local service = $.core.v1.service,
  local pvc = $.core.v1.persistentVolumeClaim,

  local podAntiAffinity = deployment.mixin.spec.template.spec.affinity.podAntiAffinity,

  _config+:: {
    multi_zone_ingester_enabled: true,
    multi_zone_ingester_migration_enabled: false,
    multi_zone_ingester_replication_write_path_enabled: true,
    multi_zone_ingester_replication_read_path_enabled: true,
    multi_zone_ingester_replicas: 3,
    multi_zone_ingester_max_unavailable: std.max(1, std.floor($._config.multi_zone_ingester_replicas / 9)),
    multi_zone_default_ingester_zone: false,
    multi_zone_ingester_exclude_default: false,
    multi_zone_ingester_name_prefix: 'ingester-zone',

    // If use_topology_spread is true, ingesters can run on nodes already running ingesters but will be
    // spread through the available nodes using a TopologySpreadConstraints with a max skew
    // of topology_spread_max_skew.
    // See: https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
    // If use_topology_spread is false, ingesters will not be scheduled on nodes already running ingesters.
    multi_zone_ingester_use_topology_spread: false,
    multi_zone_ingester_topology_spread_max_skew: 1,

    node_selector: null,
  },

  // Zone-aware replication.
  distributor_args+:: if !($._config.multi_zone_ingester_enabled && $._config.multi_zone_ingester_replication_write_path_enabled) then {} else {
    'distributor.zone-awareness-enabled': 'true',
  },

  ruler_args+:: (
    if !($._config.multi_zone_ingester_enabled && $._config.multi_zone_ingester_replication_write_path_enabled) then {} else {
      'distributor.zone-awareness-enabled': 'true',
    }
  ),

  querier_args+:: (
    if !($._config.multi_zone_ingester_enabled && $._config.multi_zone_ingester_replication_read_path_enabled) then {} else {
      'distributor.zone-awareness-enabled': 'true',
    }
  ),

  // Multi-zone ingesters.
  ingester_zone_a_args:: {},
  ingester_zone_b_args:: {},
  ingester_zone_c_args:: {},

  ingester_container+:: if !$._config.multi_zone_default_ingester_zone then {} else
    container.withArgs($.util.mapToFlags($.ingester_args {
      'ingester.availability-zone': 'zone-default',
    })),


  // remove after upstream PR is merged and is in a K release
  // functions for k8s objects
  newLokiPdb(deploymentName, maxUnavailable=1)::
    local pdbName = '%s-pdb' % deploymentName;

    podDisruptionBudget.new() +
    podDisruptionBudget.mixin.metadata.withName(pdbName) +
    podDisruptionBudget.mixin.metadata.withLabels({ name: pdbName }) +
    podDisruptionBudget.mixin.spec.selector.withMatchLabels({ name: deploymentName }) +
    podDisruptionBudget.mixin.spec.withMaxUnavailable(maxUnavailable),

  newIngesterPdb(ingesterName)::
    $.newLokiPdb(ingesterName),

  newLokiStatefulSet(name, replicas, container, pvc, podManagementPolicy='Parallel')::
    statefulSet.new(name, replicas, container, pvc) +
    statefulSet.mixin.spec.withServiceName(name) +
    statefulSet.mixin.spec.template.metadata.withLabels({ name: name }) +
    statefulSet.mixin.spec.selector.withMatchLabels({ name: name }) +
    statefulSet.mixin.spec.template.spec.securityContext.withFsGroup(10001) +  // 10001 is the group ID assigned to Loki in the Dockerfile
    statefulSet.mixin.spec.updateStrategy.withType('RollingUpdate') +
    $.config_hash_mixin +
    (if podManagementPolicy != null then statefulSet.mixin.spec.withPodManagementPolicy(podManagementPolicy) else {}) +
    (if !std.isObject($._config.node_selector) then {} else statefulSet.mixin.spec.template.spec.withNodeSelectorMixin($._config.node_selector)) +
    $.util.configVolumeMount('loki', '/etc/loki/config') +
    $.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ),

  newIngesterZoneContainer(zone, zone_args)::
    $.ingester_container +
    container.withArgs($.util.mapToFlags(
      $.ingester_args + zone_args + {
        'ingester.availability-zone': 'zone-%s' % zone,
      },
    )),

  newIngesterZoneStatefulSet(zone, container, name_prefix='')::
    local name = '%(prefix)s-%(zone)s' % { prefix: if name_prefix == '' then $._config.multi_zone_ingester_name_prefix else name_prefix, zone: zone };

    self.newIngesterStatefulSet(name, container, with_anti_affinity=false) +
    statefulSet.mixin.metadata.withLabels({ 'rollout-group': 'ingester' }) +
    statefulSet.mixin.metadata.withAnnotations({ 'rollout-max-unavailable': std.toString($._config.multi_zone_ingester_max_unavailable) }) +
    statefulSet.mixin.spec.template.metadata.withLabels({ name: name, 'rollout-group': 'ingester' }) +
    statefulSet.mixin.spec.selector.withMatchLabels({ name: name, 'rollout-group': 'ingester' }) +
    statefulSet.mixin.spec.updateStrategy.withType('OnDelete') +
    statefulSet.mixin.spec.withReplicas(std.ceil($._config.multi_zone_ingester_replicas / 3)) +
    (
      if $._config.multi_zone_ingester_use_topology_spread then
        statefulSet.spec.template.spec.withTopologySpreadConstraints(
          // Evenly spread queriers among available nodes.
          topologySpreadConstraints.labelSelector.withMatchLabels({ name: name }) +
          topologySpreadConstraints.withTopologyKey('kubernetes.io/hostname') +
          topologySpreadConstraints.withWhenUnsatisfiable('ScheduleAnyway') +
          topologySpreadConstraints.withMaxSkew($._config.multi_zone_ingester_topology_spread_max_skew),
        )
      else {}
    ) +
    (if !std.isObject($._config.node_selector) then {} else statefulSet.mixin.spec.template.spec.withNodeSelectorMixin($._config.node_selector)) +
    if $._config.ingester_allow_multiple_replicas_on_same_node then {} else {
      spec+:
        // Allow to schedule 2+ ingesters in the same zone on the same node, but do not schedule 2+ ingesters in
        // different zones on the same node. In case of 1 node failure in the Kubernetes cluster, only ingesters
        // in 1 zone will be affected.
        podAntiAffinity.withRequiredDuringSchedulingIgnoredDuringExecution([
          podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecutionType.new() +
          podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecutionType.mixin.labelSelector.withMatchExpressions([
            { key: 'rollout-group', operator: 'In', values: ['ingester'] },
            { key: 'name', operator: 'NotIn', values: [name] },
          ]) +
          podAntiAffinity.requiredDuringSchedulingIgnoredDuringExecutionType.withTopologyKey('kubernetes.io/hostname'),
        ]).spec,
    },

  // Creates a headless service for the per-zone ingesters StatefulSet. We don't use it
  // but we need to create it anyway because it's responsible for the network identity of
  // the StatefulSet pods. For more information, see:
  // https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#statefulset-v1-apps
  newIngesterZoneService(sts)::
    $.util.serviceFor(sts, $._config.service_ignored_labels) +
    service.mixin.spec.withClusterIp('None'),  // Headless.

  ingester_zone_a_container:: if !$._config.multi_zone_ingester_enabled then null else
    self.newIngesterZoneContainer('a', $.ingester_zone_a_args),

  ingester_zone_a_statefulset: if !$._config.multi_zone_ingester_enabled then {} else
    self.newIngesterZoneStatefulSet('a', $.ingester_zone_a_container),

  ingester_zone_a_service: if !$._config.multi_zone_ingester_enabled then null else
    $.newIngesterZoneService($.ingester_zone_a_statefulset),

  ingester_zone_b_container:: if !$._config.multi_zone_ingester_enabled then null else
    self.newIngesterZoneContainer('b', $.ingester_zone_b_args),

  ingester_zone_b_statefulset: if !$._config.multi_zone_ingester_enabled then {} else
    self.newIngesterZoneStatefulSet('b', $.ingester_zone_b_container),

  ingester_zone_b_service: if !$._config.multi_zone_ingester_enabled then null else
    $.newIngesterZoneService($.ingester_zone_b_statefulset),

  ingester_zone_c_container:: if !$._config.multi_zone_ingester_enabled then null else
    self.newIngesterZoneContainer('c', $.ingester_zone_c_args),

  ingester_zone_c_statefulset: if !$._config.multi_zone_ingester_enabled then {} else
    self.newIngesterZoneStatefulSet('c', $.ingester_zone_c_container),

  ingester_zone_c_service: if !$._config.multi_zone_ingester_enabled then null else
    $.newIngesterZoneService($.ingester_zone_c_statefulset),

  ingester_rollout_pdb: if !$._config.multi_zone_ingester_enabled then null else
    podDisruptionBudget.new('ingester-rollout-pdb') +
    podDisruptionBudget.mixin.metadata.withLabels({ name: 'ingester-rollout-pdb' }) +
    podDisruptionBudget.mixin.spec.selector.withMatchLabels({ 'rollout-group': 'ingester' }) +
    podDisruptionBudget.mixin.spec.withMaxUnavailable(1),

  ingester_statefulset:
    // Remove the default "ingester" StatefulSet if multi-zone is enabled and no migration is in progress.
    if $._config.multi_zone_ingester_enabled && !$._config.multi_zone_ingester_migration_enabled
    then {}
    else super.ingester_statefulset,

  ingester_service:
    // Remove the default "ingester" service if multi-zone is enabled and no migration is in progress.
    if $._config.multi_zone_ingester_enabled && !$._config.multi_zone_ingester_migration_enabled
    then null
    else super.ingester_service,

  ingester_pdb:
    // Keep it if multi-zone is disabled.
    if !$._config.multi_zone_ingester_enabled
    then super.ingester_pdb
    // We don’t want Kubernetes to terminate any "ingester" StatefulSet's pod while migration is in progress.
    else if $._config.multi_zone_ingester_migration_enabled
    then super.ingester_pdb + podDisruptionBudget.mixin.spec.withMaxUnavailable(0)
    // Remove it if multi-zone is enabled and no migration is in progress.
    else null,
} + {
  distributor_args+:: if $._config.multi_zone_ingester_exclude_default then {
    'distributor.excluded-zones': 'zone-default',
  } else {},

  ruler_args+:: if $._config.multi_zone_ingester_exclude_default then {
    'distributor.excluded-zones': 'zone-default',
  } else {},
} + rolloutOperator
