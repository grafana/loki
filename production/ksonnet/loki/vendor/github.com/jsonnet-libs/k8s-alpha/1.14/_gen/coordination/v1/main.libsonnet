{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1', url='', help=''),
  lease: (import 'lease.libsonnet'),
  leaseSpec: (import 'leaseSpec.libsonnet')
}