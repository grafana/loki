local k = import 'ksonnet-util/kausal.libsonnet';

k {
  _images+:: {
    promtail: 'grafana/promtail:latest',
  },

  _config+:: {
    prometheus_insecure_skip_verify: false,
    promtail_config: {
      username: '',
      password: '',
      scheme: 'https',
      hostname: 'logs-us-west1.grafana.net',
      dataroot: '/var/lib/docker',
      external_labels: {},
    },


    service_url:
      if std.objectHas(self.promtail_config, 'username') then
        '%(scheme)s://%(username)s:%(password)s@%(hostname)s/api/prom/push' % self.promtail_config
      else
        '%(scheme)s://%(hostname)s/api/prom/push' % self.promtail_config,
  },

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

  promtail_config:: {
    client: {
      external_labels: $._config.promtail_config.external_labels,
    },
    scrape_configs: [
      {
        job_name: 'kubernetes-pods',
        kubernetes_sd_configs: [{
          role: 'pod',
        }],

        relabel_configs: [
          // Only scrape local pods; Promtail will drop targets with a __host__ label
          // that does not match the current host name.
          {
            source_labels: ['__meta_kubernetes_pod_node_name'],
            target_label: '__host__',
          },

          // Drop pods without a name label
          {
            source_labels: ['__meta_kubernetes_pod_label_name'],
            action: 'drop',
            regex: '^$',
          },

          // Rename jobs to be <namespace>/<name, from pod name label>
          {
            source_labels: ['__meta_kubernetes_namespace', '__meta_kubernetes_pod_label_name'],
            action: 'replace',
            separator: '/',
            target_label: 'job',
            replacement: '$1',
          },

          // But also include the namespace as a separate label, for routing alerts
          {
            source_labels: ['__meta_kubernetes_namespace'],
            action: 'replace',
            target_label: 'namespace',
          },

          // Rename instances to be the pod name
          {
            source_labels: ['__meta_kubernetes_pod_name'],
            action: 'replace',
            target_label: 'instance',
          },

          // Include container_name label
          {
            source_labels: ['__meta_kubernetes_pod_container_name'],
            action: 'replace',
            target_label: 'container_name',
          },

          // Also include all the other labels on the pod.
          {
            action: 'labelmap',
            regex: '__meta_kubernetes_pod_label_(.+)',
          },

          // Kubernetes puts logs under subdirectories keyed pod UID and container_name.
          {
            source_labels: ['__meta_kubernetes_pod_uid', '__meta_kubernetes_pod_container_name'],
            target_label: '__path__',
            separator: '/',
            replacement: '/var/log/pods/$1/*.log',
          },
        ],
      },
      {
        job_name: 'kubernetes-pods-app',
        kubernetes_sd_configs: [{
          role: 'pod',
        }],

        relabel_configs: [
          // Only scrape local pods; Promtail will drop targets with a __host__ label
          // that does not match the current host name.
          {
            source_labels: ['__meta_kubernetes_pod_node_name'],
            target_label: '__host__',
          },

          // Drop pods without a app label
          {
            source_labels: ['__meta_kubernetes_pod_label_app'],
            action: 'drop',
            regex: '^$',
          },

          // Drop pods with a 'name' and an 'app' label.  They will have already been added by 
          // the scrape_config that matches on the 'name' label
          {
            source_labels: ['__meta_kubernetes_pod_label_name'],
            action: 'drop',
            regex: '.+',
          },

          // Rename jobs to be <namespace>/<app, from pod app label>
          {
            source_labels: ['__meta_kubernetes_namespace', '__meta_kubernetes_pod_label_app'],
            action: 'replace',
            separator: '/',
            target_label: 'job',
            replacement: '$1',
          },

          // But also include the namespace as a separate label, for routing alerts
          {
            source_labels: ['__meta_kubernetes_namespace'],
            action: 'replace',
            target_label: 'namespace',
          },

          // Rename instances to be the pod name
          {
            source_labels: ['__meta_kubernetes_pod_name'],
            action: 'replace',
            target_label: 'instance',
          },

          // Include container_name label
          {
            source_labels: ['__meta_kubernetes_pod_container_name'],
            action: 'replace',
            target_label: 'container_name',
          },

          // Also include all the other labels on the pod.
          {
            action: 'labelmap',
            regex: '__meta_kubernetes_pod_label_(.+)',
          },

          // Kubernetes puts logs under subdirectories keyed pod UID and container_name.
          {
            source_labels: ['__meta_kubernetes_pod_uid', '__meta_kubernetes_pod_container_name'],
            target_label: '__path__',
            separator: '/',
            replacement: '/var/log/pods/$1/*.log',
          },
        ],
      },
    ],
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
    container.mixin.securityContext.withPrivileged(true) +
    container.mixin.securityContext.withRunAsUser(0),

  local daemonSet = $.extensions.v1beta1.daemonSet,

  promtail_daemonset:
    daemonSet.new('promtail', [$.promtail_container]) +
    daemonSet.mixin.spec.template.spec.withServiceAccount('promtail') +
    $.util.configVolumeMount('promtail', '/etc/promtail') +
    $.util.hostVolumeMount('varlog', '/var/log', '/var/log') +
    $.util.hostVolumeMount('varlibdockercontainers', $._config.promtail_config.dataroot + '/containers', $._config.promtail_config.dataroot + '/containers', readOnly=true),
}
