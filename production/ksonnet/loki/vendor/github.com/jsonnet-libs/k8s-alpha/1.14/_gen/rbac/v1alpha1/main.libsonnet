{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1alpha1', url='', help=''),
  aggregationRule: (import 'aggregationRule.libsonnet'),
  clusterRole: (import 'clusterRole.libsonnet'),
  clusterRoleBinding: (import 'clusterRoleBinding.libsonnet'),
  policyRule: (import 'policyRule.libsonnet'),
  role: (import 'role.libsonnet'),
  roleBinding: (import 'roleBinding.libsonnet'),
  roleRef: (import 'roleRef.libsonnet'),
  subject: (import 'subject.libsonnet')
}