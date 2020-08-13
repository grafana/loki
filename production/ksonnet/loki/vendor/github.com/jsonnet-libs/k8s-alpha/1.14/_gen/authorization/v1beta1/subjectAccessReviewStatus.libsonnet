{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='subjectAccessReviewStatus', url='', help='SubjectAccessReviewStatus'),
  '#withAllowed':: d.fn(help='Allowed is required. True if the action would be allowed, false otherwise.', args=[d.arg(name='allowed', type=d.T.boolean)]),
  withAllowed(allowed): { allowed: allowed },
  '#withDenied':: d.fn(help='Denied is optional. True if the action would be denied, otherwise false. If both allowed is false and denied is false, then the authorizer has no opinion on whether to authorize the action. Denied may not be true if Allowed is true.', args=[d.arg(name='denied', type=d.T.boolean)]),
  withDenied(denied): { denied: denied },
  '#withEvaluationError':: d.fn(help='EvaluationError is an indication that some error occurred during the authorization check. It is entirely possible to get an error and be able to continue determine authorization status in spite of it. For instance, RBAC can be missing a role, but enough roles are still present and bound to reason about the request.', args=[d.arg(name='evaluationError', type=d.T.string)]),
  withEvaluationError(evaluationError): { evaluationError: evaluationError },
  '#withReason':: d.fn(help='Reason is optional.  It indicates why a request was allowed or denied.', args=[d.arg(name='reason', type=d.T.string)]),
  withReason(reason): { reason: reason },
  '#mixin': 'ignore',
  mixin: self
}