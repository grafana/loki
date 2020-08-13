{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='ingressStatus', url='', help='IngressStatus describe the current state of the Ingress.'),
  '#loadBalancer':: d.obj(help='LoadBalancerStatus represents the status of a load-balancer.'),
  loadBalancer: {
    '#withIngress':: d.fn(help='Ingress is a list containing ingress points for the load-balancer. Traffic intended for the service should be sent to these ingress points.', args=[d.arg(name='ingress', type=d.T.array)]),
    withIngress(ingress): { loadBalancer+: { ingress: if std.isArray(v=ingress) then ingress else [ingress] } },
    '#withIngressMixin':: d.fn(help='Ingress is a list containing ingress points for the load-balancer. Traffic intended for the service should be sent to these ingress points.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='ingress', type=d.T.array)]),
    withIngressMixin(ingress): { loadBalancer+: { ingress+: if std.isArray(v=ingress) then ingress else [ingress] } }
  },
  '#mixin': 'ignore',
  mixin: self
}