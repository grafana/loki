{
  namespace:
    $.core.v1.namespace.new($._config.namespace),

  util+:: {
    local containerPort = $.core.v1.containerPort,

    defaultPorts::
      [
        containerPort.newNamed('http-metrics', 80),
        containerPort.newNamed('grpc', 9095),
      ],
  },
}
