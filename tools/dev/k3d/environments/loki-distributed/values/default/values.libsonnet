{
  lokiValues: function(configStr) {
    loki: {
      config: configStr,
    },
    ingester: {
      replicas: 3,
      persistence: {
        enabled: true,
      },
      podAnnotations: {
        'prometheus.io/scrape': 'true',
        'prometheus.io/path': '/metrics',
        'prometheus.io/port': '3100',
      },
    },
    distributor: {
      replicas: 3,
      podAnnotations: {
        'prometheus.io/scrape': 'true',
        'prometheus.io/path': '/metrics',
        'prometheus.io/port': '3100',
      },
    },
    querier: {
      replicas: 3,
      podAnnotations: {
        'prometheus.io/scrape': 'true',
        'prometheus.io/path': '/metrics',
        'prometheus.io/port': '3100',
      },
    },
    queryFrontend: {
      replicas: 3,
      podAnnotations: {
        'prometheus.io/scrape': 'true',
        'prometheus.io/path': '/metrics',
        'prometheus.io/port': '3100',
      },
    },
    gateway: {
      replicas: 1,
    },
    compactor: {
      enabled: true,
      persistence: {
        enabled: true,
      },
      podAnnotations: {
        'prometheus.io/scrape': 'true',
        'prometheus.io/path': '/metrics',
        'prometheus.io/port': '3100',
      },
    },
  },
}
