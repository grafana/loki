{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='certificateSigningRequestStatus', url='', help=''),
  '#withCertificate':: d.fn(help='If request was approved, the controller will place the issued certificate here.', args=[d.arg(name='certificate', type=d.T.string)]),
  withCertificate(certificate): { certificate: certificate },
  '#withConditions':: d.fn(help='Conditions applied to the request, such as approval or denial.', args=[d.arg(name='conditions', type=d.T.array)]),
  withConditions(conditions): { conditions: if std.isArray(v=conditions) then conditions else [conditions] },
  '#withConditionsMixin':: d.fn(help='Conditions applied to the request, such as approval or denial.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='conditions', type=d.T.array)]),
  withConditionsMixin(conditions): { conditions+: if std.isArray(v=conditions) then conditions else [conditions] },
  '#mixin': 'ignore',
  mixin: self
}