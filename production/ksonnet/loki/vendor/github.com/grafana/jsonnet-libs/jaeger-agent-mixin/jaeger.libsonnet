{
  _config+:: {
    cluster: error 'Must define a cluster',
    namespace: error 'Must define a namespace',
    jaeger_agent_host: null,
  },

  local container = $.core.v1.container,

  jaeger_mixin::
    if $._config.jaeger_agent_host == null
    then {}
    else
      container.withEnvMixin([
        container.envType.new('JAEGER_AGENT_HOST', $._config.jaeger_agent_host),
        container.envType.new('JAEGER_TAGS', 'namespace=%s,cluster=%s' % [$._config.namespace, $._config.cluster]),
        container.envType.new('JAEGER_SAMPLER_MANAGER_HOST_PORT', 'http://%s:5778/sampling' % $._config.jaeger_agent_host),
      ]),
}
