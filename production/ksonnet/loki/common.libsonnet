{
  namespace:
    $.core.v1.namespace.new($._config.namespace),

  util+:: {
    local containerPort = $.core.v1.containerPort,

    defaultPorts::
      [
        containerPort.new(name='http-metrics', port=$._config.http_listen_port),
        containerPort.new(name='grpc', port=9095),
      ],
  },
}
