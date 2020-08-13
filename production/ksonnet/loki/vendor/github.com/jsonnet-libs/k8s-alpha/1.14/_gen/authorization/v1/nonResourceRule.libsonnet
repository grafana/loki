{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='nonResourceRule', url='', help='NonResourceRule holds information that describes a rule for the non-resource'),
  '#withNonResourceURLs':: d.fn(help='NonResourceURLs is a set of partial urls that a user should have access to.  *s are allowed, but only as the full, final step in the path.  "*" means all.', args=[d.arg(name='nonResourceURLs', type=d.T.array)]),
  withNonResourceURLs(nonResourceURLs): { nonResourceURLs: if std.isArray(v=nonResourceURLs) then nonResourceURLs else [nonResourceURLs] },
  '#withNonResourceURLsMixin':: d.fn(help='NonResourceURLs is a set of partial urls that a user should have access to.  *s are allowed, but only as the full, final step in the path.  "*" means all.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='nonResourceURLs', type=d.T.array)]),
  withNonResourceURLsMixin(nonResourceURLs): { nonResourceURLs+: if std.isArray(v=nonResourceURLs) then nonResourceURLs else [nonResourceURLs] },
  '#withVerbs':: d.fn(help='Verb is a list of kubernetes non-resource API verbs, like: get, post, put, delete, patch, head, options.  "*" means all.', args=[d.arg(name='verbs', type=d.T.array)]),
  withVerbs(verbs): { verbs: if std.isArray(v=verbs) then verbs else [verbs] },
  '#withVerbsMixin':: d.fn(help='Verb is a list of kubernetes non-resource API verbs, like: get, post, put, delete, patch, head, options.  "*" means all.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='verbs', type=d.T.array)]),
  withVerbsMixin(verbs): { verbs+: if std.isArray(v=verbs) then verbs else [verbs] },
  '#mixin': 'ignore',
  mixin: self
}