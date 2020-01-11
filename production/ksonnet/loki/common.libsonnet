{
  namespace:
    $.core.v1.namespace.new($._config.namespace),

  util+:: {
    local containerPort = $.core.v1.containerPort,

    defaultPorts::
      [
        containerPort.newNamed(name='http-metrics', containerPort=80),
        containerPort.newNamed(name='grpc', containerPort=9095),
      ],
  },
}
