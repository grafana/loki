{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='sessionAffinityConfig', url='', help='SessionAffinityConfig represents the configurations of session affinity.'),
  '#clientIP':: d.obj(help='ClientIPConfig represents the configurations of Client IP based session affinity.'),
  clientIP: {
    '#withTimeoutSeconds':: d.fn(help='timeoutSeconds specifies the seconds of ClientIP type session sticky time. The value must be >0 && <=86400(for 1 day) if ServiceAffinity == "ClientIP". Default value is 10800(for 3 hours).', args=[d.arg(name='timeoutSeconds', type=d.T.integer)]),
    withTimeoutSeconds(timeoutSeconds): { clientIP+: { timeoutSeconds: timeoutSeconds } }
  },
  '#mixin': 'ignore',
  mixin: self
}