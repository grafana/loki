{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='subjectRulesReviewStatus', url='', help="SubjectRulesReviewStatus contains the result of a rules check. This check can be incomplete depending on the set of authorizers the server is configured with and any errors experienced during evaluation. Because authorization rules are additive, if a rule appears in a list it's safe to assume the subject has that permission, even if that list is incomplete."),
  '#withEvaluationError':: d.fn(help="EvaluationError can appear in combination with Rules. It indicates an error occurred during rule evaluation, such as an authorizer that doesn't support rule evaluation, and that ResourceRules and/or NonResourceRules may be incomplete.", args=[d.arg(name='evaluationError', type=d.T.string)]),
  withEvaluationError(evaluationError): { evaluationError: evaluationError },
  '#withIncomplete':: d.fn(help="Incomplete is true when the rules returned by this call are incomplete. This is most commonly encountered when an authorizer, such as an external authorizer, doesn't support rules evaluation.", args=[d.arg(name='incomplete', type=d.T.boolean)]),
  withIncomplete(incomplete): { incomplete: incomplete },
  '#withNonResourceRules':: d.fn(help="NonResourceRules is the list of actions the subject is allowed to perform on non-resources. The list ordering isn't significant, may contain duplicates, and possibly be incomplete.", args=[d.arg(name='nonResourceRules', type=d.T.array)]),
  withNonResourceRules(nonResourceRules): { nonResourceRules: if std.isArray(v=nonResourceRules) then nonResourceRules else [nonResourceRules] },
  '#withNonResourceRulesMixin':: d.fn(help="NonResourceRules is the list of actions the subject is allowed to perform on non-resources. The list ordering isn't significant, may contain duplicates, and possibly be incomplete.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='nonResourceRules', type=d.T.array)]),
  withNonResourceRulesMixin(nonResourceRules): { nonResourceRules+: if std.isArray(v=nonResourceRules) then nonResourceRules else [nonResourceRules] },
  '#withResourceRules':: d.fn(help="ResourceRules is the list of actions the subject is allowed to perform on resources. The list ordering isn't significant, may contain duplicates, and possibly be incomplete.", args=[d.arg(name='resourceRules', type=d.T.array)]),
  withResourceRules(resourceRules): { resourceRules: if std.isArray(v=resourceRules) then resourceRules else [resourceRules] },
  '#withResourceRulesMixin':: d.fn(help="ResourceRules is the list of actions the subject is allowed to perform on resources. The list ordering isn't significant, may contain duplicates, and possibly be incomplete.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='resourceRules', type=d.T.array)]),
  withResourceRulesMixin(resourceRules): { resourceRules+: if std.isArray(v=resourceRules) then resourceRules else [resourceRules] },
  '#mixin': 'ignore',
  mixin: self
}