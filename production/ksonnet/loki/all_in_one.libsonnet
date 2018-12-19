(import 'ksonnet-util/kausal.libsonnet') +
(import 'prometheus-ksonnet/lib/grafana.libsonnet') +
(import 'prometheus-ksonnet/lib/config.libsonnet') +
(import 'common.libsonnet') + {
  _images+:: {
    loki: 'grafana/loki:master',
    grafana: 'grafana/grafana:master',
  },

  _config+:: {
    boltdb_dir: '/tmp/loki/index',
    filesystem_dir: '/tmp/loki/chunks',

    grafana_root_url: '',
  },

  grafana_datasource_config_map:
    configMap.new('grafana-datasources') +
    configMap.withDataMixin({
      'loki.yml': $.util.manifestYaml({
        apiVersion: 1,
        datasources: [{
          name: 'Loki',
          type: 'loki',
          access: 'proxy',
          url: 'http://loki.default.svc',
          editable: false,
        }],
      }),
    }),

  yamlConfig:: {
    server: {
      http_listen_port: 80,
    },
    auth_enabled: false,
    ingester: {
      lifecycler: {
        address: '0.0.0.0',
        ring: {
          store: 'inmemory',
          replication_factor: 1,
        },
      },
    },
    schema_config: {
      configs: [{
        from: 0,
        store: 'boltdb',
        object_store: 'filesystem',
        schema: 'v9',
        index: {
          prefix: 'index_',
          period: '168h',
        },
      }],
    },
    storage_config: {
      boltdb: {
        directory: $._config.boltdb_dir,
      },
      filesystem: {
        directory: $._config.filesystem_dir,
      },
    },
  },

  local configMap = $.core.v1.configMap,

  config_file:
    configMap.new('loki') +
    configMap.withData({
      'config.yaml': $.util.manifestYaml($.yamlConfig),
    }),

  local container = $.core.v1.container,

  ingester_args:: {
    'config.file': '/etc/loki/config.yaml',
  },

  loki_container::
    container.new('loki', $._images.loki) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin($.util.mapToFlags($.ingester_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort(80) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1),

  local deployment = $.apps.v1beta1.deployment,

  loki_deployment:
    deployment.new('loki', 1, [$.loki_container]) +
    $.util.configVolumeMount('loki', '/etc/loki') +
    deployment.mixin.spec.template.spec.withTerminationGracePeriodSeconds(4800) +
    deployment.mixin.spec.template.metadata.withAnnotationsMixin({ schemaID: std.md5(std.toString($.yamlConfig)) }),

  ingester_service:
    $.util.serviceFor($.loki_deployment),
}
