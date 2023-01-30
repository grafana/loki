local k = import 'ksonnet-util/kausal.libsonnet';

{
  util+:: {
    local containerPort = k.core.v1.containerPort,

    defaultPorts::
      [
        containerPort.newNamed(name='http-metrics', containerPort=$._config.http_listen_port),
        containerPort.newNamed(name='grpc', containerPort=9095),
      ],
  },
}
