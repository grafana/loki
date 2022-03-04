{
  _images+:: {
    promtail: 'grafana/promtail:2.4.2',
  },

  _config+:: {
    prometheus_insecure_skip_verify: false,
    promtail_config: {
      clients: [{
        username:: '',
        password:: '',
        scheme:: 'https',
        hostname:: error 'must define a valid hostname',
        external_labels: {},
      }],
      container_root_path: '/var/lib/docker',
      pipeline_stages: [{
        docker: {},
      }],
    },
    promtail_cluster_role_name: 'promtail',
    promtail_configmap_name: 'promtail',
    promtail_pod_name: 'promtail',
  },
}
