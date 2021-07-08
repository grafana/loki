local scrape_config = import './scrape_config.libsonnet';
local config = import 'config.libsonnet';
local k = import 'ksonnet-util/kausal.libsonnet';

// backwards compatibility with ksonnet
local envVar = if std.objectHasAll(k.core.v1, 'envVar') then k.core.v1.envVar else k.core.v1.container.envType;

config + scrape_config {
  namespace:
    k.core.v1.namespace.new($._config.namespace),

  // The RBAC functions in kausal.libsonnet require namespace to be set
  local namespaced_k = k {
    _config+:: { namespace: $._config.namespace },
  },

  local policyRule = k.rbac.v1beta1.policyRule,


  promtail_rbac:
    namespaced_k.util.rbac($._config.promtail_cluster_role_name, [
      policyRule.new() +
      policyRule.withApiGroups(['']) +
      policyRule.withResources(['nodes', 'nodes/proxy', 'services', 'endpoints', 'pods']) +
      policyRule.withVerbs(['get', 'list', 'watch']),
    ]),

  promtail_config+:: {
    local service_url(client) =
      if std.objectHasAll(client, 'username') then
        '%(scheme)s://%(username)s:%(password)s@%(hostname)s/loki/api/v1/push' % client
      else
        '%(scheme)s://%(hostname)s/loki/api/v1/push' % client,

    local client_config(client) = client {
      url: service_url(client),
    },

    clients: std.map(client_config, $._config.promtail_config.clients),
  },

  local configMap = k.core.v1.configMap,

  promtail_config_map:
    configMap.new($._config.promtail_configmap_name) +
    configMap.withData({
      'promtail.yml': k.util.manifestYaml($.promtail_config),
    }),

  promtail_args:: {
    'config.file': '/etc/promtail/promtail.yml',
  },

  local container = k.core.v1.container,

  promtail_container::
    container.new('promtail', $._images.promtail) +
    container.withPorts(k.core.v1.containerPort.new(name='http-metrics', port=80)) +
    container.withArgsMixin(k.util.mapToFlags($.promtail_args)) +
    container.withEnv([
      envVar.fromFieldPath('HOSTNAME', 'spec.nodeName'),
    ]) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort(80) +
    container.mixin.readinessProbe.withInitialDelaySeconds(10) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    container.mixin.securityContext.withPrivileged(true) +
    container.mixin.securityContext.withRunAsUser(0),

  local daemonSet = k.apps.v1.daemonSet,

  promtail_daemonset:
    daemonSet.new($._config.promtail_pod_name, [$.promtail_container]) +
    daemonSet.mixin.spec.template.spec.withServiceAccount($._config.promtail_cluster_role_name) +
    k.util.configMapVolumeMount($.promtail_config_map, '/etc/promtail') +
    k.util.hostVolumeMount('varlog', '/var/log', '/var/log') +
    k.util.hostVolumeMount('varlibdockercontainers', $._config.promtail_config.container_root_path + '/containers', $._config.promtail_config.container_root_path + '/containers', readOnly=true),
}
