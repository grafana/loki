{
  namespace:
    $.core.v1.namespace.new($._config.namespace),

  util+:: {
    local containerPort = $.core.v1.containerPort,
    local container = $.core.v1.container,

    defaultPorts::
      [
        containerPort.new(name='http-metrics', port=$._config.http_listen_port),
        containerPort.new(name='grpc', port=9095),
      ],

    readinessProbe::
      container.mixin.readinessProbe.httpGet.withPath('/ready') +
      container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
      container.mixin.readinessProbe.withInitialDelaySeconds(15) +
      container.mixin.readinessProbe.withTimeoutSeconds(1),

  },
}
