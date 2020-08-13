{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='rollbackConfig', url='', help='DEPRECATED.'),
  '#withRevision':: d.fn(help='The revision to rollback to. If set to 0, rollback to the last revision.', args=[d.arg(name='revision', type=d.T.integer)]),
  withRevision(revision): { revision: revision },
  '#mixin': 'ignore',
  mixin: self
}