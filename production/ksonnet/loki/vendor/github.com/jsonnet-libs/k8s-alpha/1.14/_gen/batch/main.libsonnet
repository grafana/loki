{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='batch', url='', help=''),
  v1: (import 'v1/main.libsonnet'),
  v1beta1: (import 'v1beta1/main.libsonnet'),
  v2alpha1: (import 'v2alpha1/main.libsonnet')
}