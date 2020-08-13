{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1beta1', url='', help=''),
  mutatingWebhookConfiguration: (import 'mutatingWebhookConfiguration.libsonnet'),
  ruleWithOperations: (import 'ruleWithOperations.libsonnet'),
  serviceReference: (import 'serviceReference.libsonnet'),
  validatingWebhookConfiguration: (import 'validatingWebhookConfiguration.libsonnet'),
  webhook: (import 'webhook.libsonnet'),
  webhookClientConfig: (import 'webhookClientConfig.libsonnet')
}