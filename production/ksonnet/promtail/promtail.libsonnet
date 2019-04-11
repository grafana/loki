local k = import 'ksonnet-util/kausal.libsonnet';
local config = import 'config.libsonnet';
local scrape_config = import './scrape_config.libsonnet';

k + config + scrape_config {
  namespace:
    $.core.v1.namespace.new($._config.namespace),

  local policyRule = $.rbac.v1beta1.policyRule,

  promtail_rbac:
    $.util.rbac('promtail', [
      policyRule.new() +
      policyRule.withApiGroups(['']) +
      policyRule.withResources(['nodes', 'nodes/proxy', 'services', 'endpoints', 'pods']) +
      policyRule.withVerbs(['get', 'list', 'watch']),
    ]),

  promtail_config+:: {
    client: {
      external_labels: $._config.promtail_config.external_labels,
    },
  },

  local configMap = $.core.v1.configMap,

  promtail_config_map:
    configMap.new('promtail') +
    configMap.withData({
      'promtail.yml': $.util.manifestYaml($.promtail_config),
    }),

  promtail_args:: {
    'client.url': $._config.service_url,
    'config.file': '/etc/promtail/promtail.yml',
  },

  local container = $.core.v1.container,

  promtail_container::
    container.new('promtail', $._images.promtail) +
    container.withPorts($.core.v1.containerPort.new('http-metrics', 80)) +
    container.withArgsMixin($.util.mapToFlags($.promtail_args)) +
    container.withEnv([
      container.envType.fromFieldPath('HOSTNAME', 'spec.nodeName'),
    ]) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort(80) +
    container.mixin.readinessProbe.withInitialDelaySeconds(10) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    container.mixin.securityContext.withPrivileged(true) +
    container.mixin.securityContext.withRunAsUser(0),

  local daemonSet = $.extensions.v1beta1.daemonSet,

  promtail_daemonset:
    daemonSet.new('promtail', [$.promtail_container]) +
    daemonSet.mixin.spec.template.spec.withServiceAccount('promtail') +
    $.util.configVolumeMount('promtail', '/etc/promtail') +
    $.util.hostVolumeMount('varlog', '/var/log', '/var/log') +
    $.util.hostVolumeMount('varlibdockercontainers', $._config.promtail_config.container_root_path + '/containers', $._config.promtail_config.container_root_path + '/containers', readOnly=true),
}
