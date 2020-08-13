{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='ingressTLS', url='', help='IngressTLS describes the transport layer security associated with an Ingress.'),
  '#withHosts':: d.fn(help='Hosts are a list of hosts included in the TLS certificate. The values in this list must match the name/s used in the tlsSecret. Defaults to the wildcard host setting for the loadbalancer controller fulfilling this Ingress, if left unspecified.', args=[d.arg(name='hosts', type=d.T.array)]),
  withHosts(hosts): { hosts: if std.isArray(v=hosts) then hosts else [hosts] },
  '#withHostsMixin':: d.fn(help='Hosts are a list of hosts included in the TLS certificate. The values in this list must match the name/s used in the tlsSecret. Defaults to the wildcard host setting for the loadbalancer controller fulfilling this Ingress, if left unspecified.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='hosts', type=d.T.array)]),
  withHostsMixin(hosts): { hosts+: if std.isArray(v=hosts) then hosts else [hosts] },
  '#withSecretName':: d.fn(help='SecretName is the name of the secret used to terminate SSL traffic on 443. Field is left optional to allow SSL routing based on SNI hostname alone. If the SNI host in a listener conflicts with the "Host" header field used by an IngressRule, the SNI host is used for termination and value of the Host header is used for routing.', args=[d.arg(name='secretName', type=d.T.string)]),
  withSecretName(secretName): { secretName: secretName },
  '#mixin': 'ignore',
  mixin: self
}