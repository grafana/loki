
local config = import 'config.libsonnet';
config + {
  scrape_configs: [
    {
      job_name: 'kubernetes-pods-name',
      entry_parser: $._config.promtail_config.entry_parser,
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
      job_name: 'kubernetes-pods-controller',
      entry_parser: $._config.promtail_config.entry_parser,
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

        // Drop pods with a 'name' label.  They will have already been added by
        // the scrape_config that matches on the 'name' label
        {
          source_labels: ['__meta_kubernetes_pod_label_name'],
          action: 'drop',
          regex: '.+',
        },

        // Drop pods with an indirect controller. eg Deployments create replicaSets
        // which then create pods.
        {
          source_labels: ['__meta_kubernetes_pod_controller_name'],
          action: 'drop',
          regex: '^([0-9a-z-.]+)(-[0-9a-f]{8,10})$',
        },

        // Rename jobs to be <namespace>/<controller_name>
        {
          source_labels: ['__meta_kubernetes_namespace', '__meta_kubernetes_pod_controller_name'],
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
      ]
    },
    {
      job_name: 'kubernetes-pods-indirect-controller',
      entry_parser: $._config.promtail_config.entry_parser,
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

        // Drop pods with a 'name' label.  They will have already been added by
        // the scrape_config that matches on the 'name' label
        {
          source_labels: ['__meta_kubernetes_pod_label_name'],
          action: 'drop',
          regex: '.+',
        },

        // Drop pods not from an indirect controller. eg StatefulSets, DaemonSets
        {
          source_labels: ['__meta_kubernetes_pod_controller_name'],
          action: 'keep',
          regex: '^([0-9a-z-.]+)(-[0-9a-f]{8,10})$',
        },

        // put the indirect controller name into a temp label.
        {
          source_labels: ['__meta_kubernetes_pod_controller_name'],
          action: 'replace',
          regex: '^([0-9a-z-.]+)(-[0-9a-f]{8,10})$',
          target_label: '__tmp_controller',
        },

        // Rename jobs to be <namespace>/<controller_name>
        {
          source_labels: ['__meta_kubernetes_namespace', '__tmp_controller'],
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
      ]
    },
  ]
}