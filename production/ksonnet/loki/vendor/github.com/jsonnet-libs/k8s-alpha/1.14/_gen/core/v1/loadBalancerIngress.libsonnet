{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='loadBalancerIngress', url='', help='LoadBalancerIngress represents the status of a load-balancer ingress point: traffic intended for the service should be sent to an ingress point.'),
  '#withHostname':: d.fn(help='Hostname is set for load-balancer ingress points that are DNS based (typically AWS load-balancers)', args=[d.arg(name='hostname', type=d.T.string)]),
  withHostname(hostname): { hostname: hostname },
  '#withIp':: d.fn(help='IP is set for load-balancer ingress points that are IP based (typically GCE or OpenStack load-balancers)', args=[d.arg(name='ip', type=d.T.string)]),
  withIp(ip): { ip: ip },
  '#mixin': 'ignore',
  mixin: self
}