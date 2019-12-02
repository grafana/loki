{
  _images+:: {
    promtail: 'grafana/promtail:v1.0.0',
  },

  _config+:: {
    prometheus_insecure_skip_verify: false,
    promtail_config: {
      clients:[{
        username:: '',
        password:: '',
        scheme:: 'https',
        hostname:: 'logs-us-west1.grafana.net',
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
