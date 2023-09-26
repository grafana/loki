local k = import 'github.com/grafana/jsonnet-libs/ksonnet-util/kausal.libsonnet';
{
  local deployment = k.apps.v1.deployment,
  local container = k.core.v1.container,
  local port = k.core.v1.containerPort,
  local service = k.core.v1.service,
  local servicePort = k.core.v1.servicePort,
  local envVar = if std.objectHasAll(k.core.v1, 'envVar') then k.core.v1.envVar else k.core.v1.container.envType,
  local queryServiceName = self.jaeger.query_service.metadata.name,

  _config+:: {
    jagerAgentName: error 'must provide $._config.jaegerAgentName',
    jaeger: {
      agentPort: 6831,
      queryPort: 16686,
      queryGrpcPort: 16685,
      agentConfigsPort: 5778,
      collectorZipkinHttpPort: 9411,
    },
  },

  _addJaegerEnvVars:: function(c) c {
    env: [
      envVar.new('JAEGER_AGENT_HOST', $._config.jaegerAgentName),
      envVar.new('JAEGER_AGENT_PORT', '6831'),
      envVar.new('JAEGER_SAMPLER_TYPE', 'const'),
      envVar.new('JAEGER_SAMPLER_PARAM', '1'),
      envVar.new('JAEGER_TAGS', 'app=gel'),
    ],
  },

  jaeger: {
    deployment: deployment.new(name='jaeger', replicas=1, containers=[
      container.new('jaeger', 'jaegertracing/all-in-one')
      + container.withPorts([
        port.newNamed($._config.jaeger.queryGrpcPort, 'query-grpc'),
        port.newNamed($._config.jaeger.agentConfigsPort, 'agent-configs'),
        port.newNamed($._config.jaeger.collectorZipkinHttpPort, 'zipkin'),
        port.newNamed($._config.jaeger.queryPort, 'query'),
        port.newNamedUDP($._config.jaeger.agentPort, 'agent'),
        port.newNamedUDP(5775, 'zipkin-thrift'),
        port.newNamedUDP(6832, 'jaeger-thrift'),
      ]) +
      container.withEnv([
        envVar.new('COLLECTOR_ZIPKIN_HTTP_PORT', '%d' % [$._config.jaeger.collectorZipkinHttpPort]),
        envVar.new('JAEGER_AGENT_HOST', queryServiceName),
        envVar.new('JAEGER_AGENT_PORT', '%d' % [$._config.jaeger.agentPort]),
      ]) + container.mixin.readinessProbe.httpGet.withPath('/')
      + container.mixin.readinessProbe.httpGet.withPort(14269)
      + container.mixin.readinessProbe.withInitialDelaySeconds(5),
    ]),

    query_service: service.new('jaeger-query', {
      name: 'jaeger',
    }, [
      servicePort.newNamed('query', 80, $._config.jaeger.queryPort),
      servicePort.newNamed('query-gprc', $._config.jaeger.queryGrpcPort, $._config.jaeger.queryGrpcPort),
    ]),

    agent_service: service.new('jaeger-agent', {
      name: 'jaeger',
    }, [
      servicePort.newNamed('agent-compat', $._config.jaeger.agentPort, $._config.jaeger.agentPort) + servicePort.withProtocol('UDP'),
      servicePort.newNamed('agent-configs', $._config.jaeger.agentConfigsPort, $._config.jaeger.agentConfigsPort),
    ]),
  },
}
