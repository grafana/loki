{
  _images+:: {
    promtail: 'grafana/promtail:latest',
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
      entry_parser: 'docker',
    },
  },
}
