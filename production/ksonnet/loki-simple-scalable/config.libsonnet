local k = import 'ksonnet-util/kausal.libsonnet',
      statefulSet = k.apps.v1.statefulSet;

{
  _config+:: {
    headless_service_name: error 'must provide a name for the headless memberlist service under $._config.headless_service_name',
    http_listen_port: error 'must provide http listen port under $._config.http_listen_port',
    loki: error 'must provide loki config under $._config.loki',

    commonArgs: {
      'config.file': '/etc/loki/config.yaml',
    },

    config_hash_mixin:
      statefulSet.mixin.spec.template.metadata.withAnnotationsMixin({
        config_hash: std.md5(std.toString($._config.loki)),
      }),
  },

  local configMap = k.core.v1.configMap,

  config_file:
    configMap.new('loki') +
    configMap.withData({
      'config.yaml': k.util.manifestYaml($._config.loki),
    }),

  local deployment = k.apps.v1.deployment,

}
