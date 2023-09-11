{
  local container = $.core.v1.container,
  local deployment = $.apps.v1.deployment,
  local statefulSet = $.apps.v1.statefulSet,
  local podDisruptionBudget = $.policy.v1.podDisruptionBudget,
  local volume = $.core.v1.volume,
  local roleBinding = $.rbac.v1.roleBinding,
  local role = $.rbac.v1.role,
  local service = $.core.v1.service,
  local serviceAccount = $.core.v1.serviceAccount,
  local servicePort = $.core.v1.servicePort,
  local policyRule = $.rbac.v1.policyRule,
  local podAntiAffinity = deployment.mixin.spec.template.spec.affinity.podAntiAffinity,

  _images+:: {
    rollout_operator: 'grafana/rollout-operator:v0.1.1',
  },

  _config+: {
    multi_zone_ingester_enabled: true,
    multi_zone_ingester_migration_enabled: false,
    multi_zone_ingester_replication_write_path_enabled: true,
    multi_zone_ingester_replication_read_path_enabled: true,
    multi_zone_ingester_replicas: 3,
    multi_zone_ingester_max_unavailable: std.max(1, std.floor($._config.multi_zone_ingester_replicas / 9)),
    multi_zone_default_ingester_zone: false,
    multi_zone_ingester_exclude_default: false,
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


  // For migration purposes we need to be able to configure a zone for single ingester statefulset deployments.
  ingester_container+:: if !$._config.multi_zone_default_ingester_zone then {} else
    container.withArgs($.util.mapToFlags($.ingester_args {
      'ingester.availability-zone': 'zone-default',
    })),

  newIngesterZoneContainer(zone, zone_args)::
    local zone_name = 'zone-%s' % zone;

    $.ingester_container +
    container.withArgs($.util.mapToFlags(
      $.ingester_args + zone_args + {
        'ingester.availability-zone': 'zone-%s' % zone,
      },
    )),

  newIngesterZoneStatefulSet(zone, container)::
    local name = 'ingester-zone-%s' % zone;

    // We can turn off anti-affinity for zone aware statefulsets since it's safe to
    // deploy multiple ingesters from the same zone on the same node.
    $.newIngesterStatefulSet(name, container, with_anti_affinity=false) +
    statefulSet.mixin.metadata.withLabels({ 'rollout-group': 'ingester' }) +
    statefulSet.mixin.metadata.withAnnotations({ 'rollout-max-unavailable': std.toString($._config.multi_zone_ingester_max_unavailable) }) +
    statefulSet.mixin.spec.template.metadata.withLabels({ name: name, 'rollout-group': 'ingester' }) +
    statefulSet.mixin.spec.selector.withMatchLabels({ name: name, 'rollout-group': 'ingester' }) +
    statefulSet.mixin.spec.updateStrategy.withType('OnDelete') +
    statefulSet.mixin.spec.template.spec.withTerminationGracePeriodSeconds(4800) +
    statefulSet.mixin.spec.withReplicas(std.ceil($._config.multi_zone_ingester_replicas / 3)) +
    (if !std.isObject($._config.node_selector) then {} else statefulSet.mixin.spec.template.spec.withNodeSelectorMixin($._config.node_selector)) +
    if $._config.ingester_allow_multiple_replicas_on_same_node then {} else {
      spec+:
        // Allow to schedule 2+ ingesters in the same zone on the same node, but do not schedule 2+ ingesters in
        // different zones on the samee node. In case of 1 node failure in the Kubernetes cluster, only ingesters
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

  ingester_zone_a_container:: if !$._config.multi_zone_ingester_enabled then {} else
    self.newIngesterZoneContainer('a', $.ingester_zone_a_args),

  ingester_zone_a_statefulset: if !$._config.multi_zone_ingester_enabled then {} else
    self.newIngesterZoneStatefulSet('a', $.ingester_zone_a_container),

  ingester_zone_a_service: if !$._config.multi_zone_ingester_enabled then {} else
    $.newIngesterZoneService($.ingester_zone_a_statefulset),

  ingester_zone_b_container:: if !$._config.multi_zone_ingester_enabled then {} else
    self.newIngesterZoneContainer('b', $.ingester_zone_b_args),

  ingester_zone_b_statefulset: if !$._config.multi_zone_ingester_enabled then {} else
    self.newIngesterZoneStatefulSet('b', $.ingester_zone_b_container),

  ingester_zone_b_service: if !$._config.multi_zone_ingester_enabled then {} else
    $.newIngesterZoneService($.ingester_zone_b_statefulset),

  ingester_zone_c_container:: if !$._config.multi_zone_ingester_enabled then {} else
    self.newIngesterZoneContainer('c', $.ingester_zone_c_args),

  ingester_zone_c_statefulset: if !$._config.multi_zone_ingester_enabled then {} else
    self.newIngesterZoneStatefulSet('c', $.ingester_zone_c_container),

  ingester_zone_c_service: if !$._config.multi_zone_ingester_enabled then {} else
    $.newIngesterZoneService($.ingester_zone_c_statefulset),

  ingester_rollout_pdb: if !$._config.multi_zone_ingester_enabled then {} else
    podDisruptionBudget.new('ingester-rollout-pdb') +
    podDisruptionBudget.mixin.metadata.withLabels({ name: 'ingester-rollout-pdb' }) +
    podDisruptionBudget.mixin.spec.selector.withMatchLabels({ 'rollout-group': 'ingester' }) +
    podDisruptionBudget.mixin.spec.withMaxUnavailable(1),

  // Single-zone ingesters shouldn't be configured when multi-zone is enabled.
  ingester_statefulset:
    // Remove the default "ingester" StatefulSet if multi-zone is enabled and no migration is in progress.
    if $._config.multi_zone_ingester_enabled && !$._config.multi_zone_ingester_migration_enabled
    then {}
    else super.ingester_statefulset,

  ingester_service:
    // Remove the default "ingester" service if multi-zone is enabled and no migration is in progress.
    if $._config.multi_zone_ingester_enabled && !$._config.multi_zone_ingester_migration_enabled
    then {}
    else super.ingester_service,

  ingester_pdb:
    // Keep it if multi-zone is disabled.
    if !$._config.multi_zone_ingester_enabled
    then super.ingester_pdb
    // We don’t want Kubernetes to terminate any "ingester" StatefulSet's pod while migration is in progress.
    else if $._config.multi_zone_ingester_migration_enabled
    then super.ingester_pdb + podDisruptionBudget.mixin.spec.withMaxUnavailable(0)
    // Remove it if multi-zone is enabled and no migration is in progress.
    else {},

  // Rollout operator.
  local rollout_operator_enabled = $._config.multi_zone_ingester_enabled,

  rollout_operator_args:: {
    'kubernetes.namespace': $._config.namespace,
  },

  rollout_operator_container::
    container.new('rollout-operator', $._images.rollout_operator) +
    container.withArgsMixin($.util.mapToFlags($.rollout_operator_args)) +
    container.withPorts([
      $.core.v1.containerPort.new('http-metrics', 8001),
    ]) +
    $.util.resourcesRequests('100m', '100Mi') +
    $.util.resourcesLimits('1', '200Mi') +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort(8001) +
    container.mixin.readinessProbe.withInitialDelaySeconds(5) +
    container.mixin.readinessProbe.withTimeoutSeconds(1),

  rollout_operator_deployment: if !rollout_operator_enabled then {} else
    deployment.new('rollout-operator', 1, [$.rollout_operator_container]) +
    deployment.mixin.metadata.withName('rollout-operator') +
    deployment.mixin.spec.template.spec.withServiceAccountName('rollout-operator') +
    // Ensure Kubernetes doesn't run 2 operators at the same time.
    deployment.mixin.spec.strategy.rollingUpdate.withMaxSurge(0) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxUnavailable(1),

  rollout_operator_role: if !rollout_operator_enabled then {} else
    role.new('rollout-operator-role') +
    role.mixin.metadata.withNamespace($._config.namespace) +
    role.withRulesMixin([
      policyRule.withApiGroups('') +
      policyRule.withResources(['pods']) +
      policyRule.withVerbs(['list', 'get', 'watch', 'delete']),
      policyRule.withApiGroups('apps') +
      policyRule.withResources(['statefulsets']) +
      policyRule.withVerbs(['list', 'get', 'watch']),
      policyRule.withApiGroups('apps') +
      policyRule.withResources(['statefulsets/status']) +
      policyRule.withVerbs(['update']),
    ]),

  rollout_operator_rolebinding: if !rollout_operator_enabled then {} else
    roleBinding.new('rollout-operator-rolebinding') +
    roleBinding.mixin.metadata.withNamespace($._config.namespace) +
    roleBinding.mixin.roleRef.withApiGroup('rbac.authorization.k8s.io') +
    roleBinding.mixin.roleRef.withKind('Role') +
    roleBinding.mixin.roleRef.withName('rollout-operator-role') +
    roleBinding.withSubjectsMixin({
      kind: 'ServiceAccount',
      name: 'rollout-operator',
      namespace: $._config.namespace,
    }),

  rollout_operator_service_account: if !rollout_operator_enabled then {} else
    serviceAccount.new('rollout-operator'),
} + {
  distributor_args+:: if $._config.multi_zone_ingester_exclude_default then {
    'distributor.excluded-zones': 'zone-default',
  } else {},

  ruler_args+:: if $._config.multi_zone_ingester_exclude_default then {
    'distributor.excluded-zones': 'zone-default',
  } else {},
}
