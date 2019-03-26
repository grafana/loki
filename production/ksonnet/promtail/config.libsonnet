{
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
      container_root_path: '/var/lib/docker',
      external_labels: {},
      entry_parser: 'docker',
      client: {
          backoff_config: {
            minbackoff: 100ms,
            maxbackoff: 5s,
            maxretries: 5
        }
      }
    },

    service_url:
      if std.objectHas(self.promtail_config, 'username') then
        '%(scheme)s://%(username)s:%(password)s@%(hostname)s/api/prom/push' % self.promtail_config
      else
        '%(scheme)s://%(hostname)s/api/prom/push' % self.promtail_config,
  },
}
