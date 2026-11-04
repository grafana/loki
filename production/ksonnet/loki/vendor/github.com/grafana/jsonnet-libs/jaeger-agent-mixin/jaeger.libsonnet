local k = import 'ksonnet-util/kausal.libsonnet';

{
  _config+:: {
    cluster: error 'Must define a cluster',
    namespace: error 'Must define a namespace',
    jaeger_agent_host: null,
    jaeger_sampler_type: null,
    jaeger_sampler_param: null,
    with_otel_resource_attrs: true,
    with_otel_container_resource_attrs: true,
  },

  local container = k.core.v1.container,

  jaeger_mixin::
    if $._config.jaeger_agent_host == null
    then {}
    else
      {
        local containerAttributes = if $._config.with_otel_container_resource_attrs && 'name' in self then ',k8s.container.name=%s' % self.name else '',

        local jaegerTags = if $._config.with_otel_resource_attrs then
          ('k8s.namespace.name=%s,service.namespace=%s,k8s.cluster.name=%s' % [$._config.namespace, $._config.namespace, $._config.cluster])
          + containerAttributes
        else
          'namespace=%s,cluster=%s' % [$._config.namespace, $._config.cluster] + containerAttributes,
        env+: [
                { name: 'JAEGER_AGENT_HOST', value: $._config.jaeger_agent_host },
                { name: 'JAEGER_TAGS', value: jaegerTags },
                { name: 'JAEGER_SAMPLER_MANAGER_HOST_PORT', value: 'http://%s:5778/sampling' % $._config.jaeger_agent_host },
              ] +
              (
                if $._config.jaeger_sampler_type == null
                then []
                else
                  [{
                    name: 'JAEGER_SAMPLER_TYPE',
                    value: $._config.jaeger_sampler_type,
                  }]
              ) +
              (
                if $._config.jaeger_sampler_param == null
                then []
                else
                  [{
                    name: 'JAEGER_SAMPLER_PARAM',
                    value: $._config.jaeger_sampler_param,
                  }]
              ),
      },
}
