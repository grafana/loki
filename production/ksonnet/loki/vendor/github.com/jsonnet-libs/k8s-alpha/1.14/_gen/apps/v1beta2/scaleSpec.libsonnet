{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='scaleSpec', url='', help='ScaleSpec describes the attributes of a scale subresource'),
  '#withReplicas':: d.fn(help='desired number of instances for the scaled object.', args=[d.arg(name='replicas', type=d.T.integer)]),
  withReplicas(replicas): { replicas: replicas },
  '#mixin': 'ignore',
  mixin: self
}