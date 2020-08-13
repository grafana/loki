{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1beta1', url='', help=''),
  certificateSigningRequest: (import 'certificateSigningRequest.libsonnet'),
  certificateSigningRequestCondition: (import 'certificateSigningRequestCondition.libsonnet'),
  certificateSigningRequestSpec: (import 'certificateSigningRequestSpec.libsonnet'),
  certificateSigningRequestStatus: (import 'certificateSigningRequestStatus.libsonnet')
}