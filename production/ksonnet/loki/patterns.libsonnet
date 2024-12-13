local k = import 'ksonnet-util/kausal.libsonnet';

{
  local container = k.core.v1.container,
  local podDisruptionBudget = k.policy.v1.podDisruptionBudget,

  _config+:: {
    pattern_ingester: {
      // globally enable or disable the use of the pattern ingester
      enabled: false,
      replicas: 3,
      allow_multiple_replicas_on_same_node: false,
    },
  },

  pattern_ingester_args:: $._config.commonArgs {
    target: 'pattern-ingester',
  },

  pattern_ingester_ports:: $.util.defaultPorts,


  pattern_ingester_container::
    container.new('pattern-ingester', $._images.pattern_ingester) +
    container.withPorts($.pattern_ingester_ports) +
    container.withArgsMixin(k.util.mapToFlags($.pattern_ingester_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    k.util.resourcesRequests('1', '7Gi') +
    k.util.resourcesLimits('2', '14Gi') +
    container.withEnvMixin($._config.commonEnvs) +
    $.jaeger_mixin,


  pattern_ingester_statefulset:
    if $._config.pattern_ingester.enabled then (
      $.newLokiStatefulSet('pattern-ingester', $._config.pattern_ingester.replicas, $.pattern_ingester_container, []) +
      $.util.podPriority('high') +
      (if !$._config.pattern_ingester.allow_multiple_replicas_on_same_node then $.util.antiAffinity else {})
    ) else {},

  pattern_ingester_service:
    if $._config.pattern_ingester.enabled then (
      k.util.serviceFor($.pattern_ingester_statefulset, $._config.service_ignored_labels)
    ) else {},

  pattern_ingester_pdb:
    if $._config.pattern_ingester.enabled then (
      podDisruptionBudget.new('loki-patter-ingester-pdb') +
      podDisruptionBudget.mixin.metadata.withLabels({ name: 'loki-pattern-ingester-pdb' }) +
      podDisruptionBudget.mixin.spec.selector.withMatchLabels({ name: 'pattern-ingester' }) +
      podDisruptionBudget.mixin.spec.withMaxUnavailable(1)
    ) else {},

}
