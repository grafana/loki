{
  namespace:
    $.core.v1.namespace.new($._config.namespace),

  util+:: {
    local containerPort = $.core.v1.containerPort,

    defaultPorts::
      [
        containerPort.new(name='http-metrics', port=containerPort=80),
        containerPort.new(name='grpc', port=containerPort=9095),
      ],
  },
}
