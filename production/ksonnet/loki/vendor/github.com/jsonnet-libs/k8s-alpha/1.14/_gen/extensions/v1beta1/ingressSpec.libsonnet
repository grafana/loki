{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='ingressSpec', url='', help='IngressSpec describes the Ingress the user wishes to exist.'),
  '#backend':: d.obj(help='IngressBackend describes all endpoints for a given service and port.'),
  backend: {
    '#withServiceName':: d.fn(help='Specifies the name of the referenced service.', args=[d.arg(name='serviceName', type=d.T.string)]),
    withServiceName(serviceName): { backend+: { serviceName: serviceName } },
    '#withServicePort':: d.fn(help='IntOrString is a type that can hold an int32 or a string.  When used in JSON or YAML marshalling and unmarshalling, it produces or consumes the inner type.  This allows you to have, for example, a JSON field that can accept a name or number.', args=[d.arg(name='servicePort', type=d.T.string)]),
    withServicePort(servicePort): { backend+: { servicePort: servicePort } }
  },
  '#withRules':: d.fn(help='A list of host rules used to configure the Ingress. If unspecified, or no rule matches, all traffic is sent to the default backend.', args=[d.arg(name='rules', type=d.T.array)]),
  withRules(rules): { rules: if std.isArray(v=rules) then rules else [rules] },
  '#withRulesMixin':: d.fn(help='A list of host rules used to configure the Ingress. If unspecified, or no rule matches, all traffic is sent to the default backend.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='rules', type=d.T.array)]),
  withRulesMixin(rules): { rules+: if std.isArray(v=rules) then rules else [rules] },
  '#withTls':: d.fn(help='TLS configuration. Currently the Ingress only supports a single TLS port, 443. If multiple members of this list specify different hosts, they will be multiplexed on the same port according to the hostname specified through the SNI TLS extension, if the ingress controller fulfilling the ingress supports SNI.', args=[d.arg(name='tls', type=d.T.array)]),
  withTls(tls): { tls: if std.isArray(v=tls) then tls else [tls] },
  '#withTlsMixin':: d.fn(help='TLS configuration. Currently the Ingress only supports a single TLS port, 443. If multiple members of this list specify different hosts, they will be multiplexed on the same port according to the hostname specified through the SNI TLS extension, if the ingress controller fulfilling the ingress supports SNI.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='tls', type=d.T.array)]),
  withTlsMixin(tls): { tls+: if std.isArray(v=tls) then tls else [tls] },
  '#mixin': 'ignore',
  mixin: self
}