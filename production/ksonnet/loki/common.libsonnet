local k = import 'ksonnet-util/kausal.libsonnet';

{
  namespace:
    k.core.v1.namespace.new($._config.namespace),

  jaeger_mixin+::
    if $._config.jaeger_agent_host == null
    then {}
    else
      k.core.v1.container.withEnvMixin([
        k.core.v1.container.envType.new('JAEGER_REPORTER_MAX_QUEUE_SIZE', std.toString($._config.jaeger_reporter_max_queue)),
      ]),

  util+:: {
    local containerPort = k.core.v1.containerPort,
    local container = k.core.v1.container,

    defaultPorts::
      [
        containerPort.new(name='http-metrics', port=$._config.http_listen_port),
        containerPort.new(name='grpc', port=9095),
      ],

    // We use DNS SRV record for discovering the schedulers or the frontends if schedulers are disabled.
    // These expect port names like _grpclb on the services.
    grpclbDefaultPorts:: [
      containerPort.new(name='http-metrics', port=$._config.http_listen_port),
      containerPort.new(name='grpclb', port=9095),
    ],

    // This helps ensure we create SRV records starting with _grpclb
    grpclbServiceFor(deployment):: k.util.serviceFor(deployment, nameFormat='%(port)s'),


    readinessProbe::
      container.mixin.readinessProbe.httpGet.withPath('/ready') +
      container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
      container.mixin.readinessProbe.withInitialDelaySeconds(15) +
      container.mixin.readinessProbe.withTimeoutSeconds(1),
  },
}
