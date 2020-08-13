{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='objectFieldSelector', url='', help='ObjectFieldSelector selects an APIVersioned field of an object.'),
  '#withFieldPath':: d.fn(help='Path of the field to select in the specified API version.', args=[d.arg(name='fieldPath', type=d.T.string)]),
  withFieldPath(fieldPath): { fieldPath: fieldPath },
  '#mixin': 'ignore',
  mixin: self
}