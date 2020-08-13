{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1alpha1', url='', help=''),
  auditSink: (import 'auditSink.libsonnet'),
  auditSinkSpec: (import 'auditSinkSpec.libsonnet'),
  policy: (import 'policy.libsonnet'),
  serviceReference: (import 'serviceReference.libsonnet'),
  webhook: (import 'webhook.libsonnet'),
  webhookClientConfig: (import 'webhookClientConfig.libsonnet'),
  webhookThrottleConfig: (import 'webhookThrottleConfig.libsonnet')
}