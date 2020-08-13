{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1beta1', url='', help=''),
  httpIngressPath: (import 'httpIngressPath.libsonnet'),
  httpIngressRuleValue: (import 'httpIngressRuleValue.libsonnet'),
  ingress: (import 'ingress.libsonnet'),
  ingressBackend: (import 'ingressBackend.libsonnet'),
  ingressRule: (import 'ingressRule.libsonnet'),
  ingressSpec: (import 'ingressSpec.libsonnet'),
  ingressStatus: (import 'ingressStatus.libsonnet'),
  ingressTLS: (import 'ingressTLS.libsonnet')
}