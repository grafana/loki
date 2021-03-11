local config = import 'config.libsonnet';

config {
  local gen_scrape_config(job_name, pod_uid) = {
    job_name: job_name,
    pipeline_stages: $._config.promtail_config.pipeline_stages,
    kubernetes_sd_configs: [{
      role: 'pod',
    }],

    relabel_configs: self.prelabel_config + [
      // Only scrape local pods; Promtail will drop targets with a __host__ label
      // that does not match the current host name.
      {
        source_labels: ['__meta_kubernetes_pod_node_name'],
        target_label: '__host__',
      },

      // Drop pods without a __service__ label.
      {
        source_labels: ['__service__'],
        action: 'drop',
        regex: '',
      },

      // Include all the other labels on the pod.
      // Perform this mapping before applying additional label replacement rules
      // to prevent a supplied label from overwriting any of the following labels.
      {
        action: 'labelmap',
        regex: '__meta_kubernetes_pod_label_(.+)',
      },

      // Rename jobs to be <namespace>/<service>.
      {
        source_labels: ['__meta_kubernetes_namespace', '__service__'],
        action: 'replace',
        separator: '/',
        target_label: 'job',
        replacement: '$1',
      },

      // But also include the namespace, pod, container as separate
      // labels. They uniquely identify a container. They are also
      // identical to the target labels configured in Prometheus
      // (but note that Loki does not use an instance label).
      {
        source_labels: ['__meta_kubernetes_namespace'],
        action: 'replace',
        target_label: 'namespace',
      },
      {
        source_labels: ['__meta_kubernetes_pod_name'],
        action: 'replace',
        target_label: 'pod',  // Not 'pod_name', which disappeared in K8s 1.16.
      },
      {
        source_labels: ['__meta_kubernetes_pod_container_name'],
        action: 'replace',
        target_label: 'container',  // Not 'container_name', which disappeared in K8s 1.16.
      },

      // Kubernetes puts logs under subdirectories keyed pod UID and container_name.
      {
        source_labels: [pod_uid, '__meta_kubernetes_pod_container_name'],
        target_label: '__path__',
        separator: '/',
        replacement: '/var/log/pods/*$1/*.log',
      },
    ],
  },

  promtail_config:: {
    scrape_configs: [
      // Scrape config to scrape any pods with a 'name' label.
      gen_scrape_config('kubernetes-pods-name', '__meta_kubernetes_pod_uid') {
        prelabel_config:: [
          // Use name label as __service__.
          {
            source_labels: ['__meta_kubernetes_pod_label_name'],
            target_label: '__service__',
          },
        ],
      },

      // Scrape config to scrape any pods with an 'app' label.
      gen_scrape_config('kubernetes-pods-app', '__meta_kubernetes_pod_uid') {
        prelabel_config:: [
          // Drop pods with a 'name' label.  They will have already been added by
          // the scrape_config that matches on the 'name' label
          {
            source_labels: ['__meta_kubernetes_pod_label_name'],
            action: 'drop',
            regex: '.+',
          },

          // Use app label as the __service__.
          {
            source_labels: ['__meta_kubernetes_pod_label_app'],
            target_label: '__service__',
          },
        ],
      },

      // Scrape config to scrape any pods with a direct controller (eg
      // StatefulSets).
      gen_scrape_config('kubernetes-pods-direct-controllers', '__meta_kubernetes_pod_uid') {
        prelabel_config:: [
          // Drop pods with a 'name' or 'app' label.  They will have already been added by
          // the scrape_config that matches above.
          {
            source_labels: ['__meta_kubernetes_pod_label_name', '__meta_kubernetes_pod_label_app'],
            separator: '',
            action: 'drop',
            regex: '.+',
          },

          // Drop pods with an indirect controller. eg Deployments create replicaSets
          // which then create pods.
          {
            source_labels: ['__meta_kubernetes_pod_controller_name'],
            action: 'drop',
            regex: '[0-9a-z-.]+-[0-9a-f]{8,10}',
          },

          // Use controller name as __service__.
          {
            source_labels: ['__meta_kubernetes_pod_controller_name'],
            target_label: '__service__',
          },
        ],
      },

      // Scrape config to scrape any pods with an indirect controller (eg
      // Deployments).
      gen_scrape_config('kubernetes-pods-indirect-controller', '__meta_kubernetes_pod_uid') {
        prelabel_config:: [
          // Drop pods with a 'name' or 'app' label.  They will have already been added by
          // the scrape_config that matches above.
          {
            source_labels: ['__meta_kubernetes_pod_label_name', '__meta_kubernetes_pod_label_app'],
            separator: '',
            action: 'drop',
            regex: '.+',
          },

          // Drop pods not from an indirect controller. eg StatefulSets, DaemonSets
          {
            source_labels: ['__meta_kubernetes_pod_controller_name'],
            regex: '[0-9a-z-.]+-[0-9a-f]{8,10}',
            action: 'keep',
          },

          // Put the indirect controller name into a temp label.
          {
            source_labels: ['__meta_kubernetes_pod_controller_name'],
            action: 'replace',
            regex: '([0-9a-z-.]+)-[0-9a-f]{8,10}',
            target_label: '__service__',
          },
        ],
      },

      // Scrape config to scrape any control plane static pods (e.g. kube-apiserver
      // etcd, kube-controller-manager & kube-scheduler)
      gen_scrape_config('kubernetes-pods-static', '__meta_kubernetes_pod_annotation_kubernetes_io_config_mirror') {
        prelabel_config:: [
          // Ignore pods that aren't mirror pods
          {
            action: 'drop',
            source_labels: ['__meta_kubernetes_pod_annotation_kubernetes_io_config_mirror'],
            regex: '',
          },

          // Static control plane pods usually have a component label that identifies them
          {
            action: 'replace',
            source_labels: ['__meta_kubernetes_pod_label_component'],
            target_label: '__service__',
          },
        ],
      },
    ],
  },
}
