{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1', url='', help=''),
  localSubjectAccessReview: (import 'localSubjectAccessReview.libsonnet'),
  nonResourceAttributes: (import 'nonResourceAttributes.libsonnet'),
  nonResourceRule: (import 'nonResourceRule.libsonnet'),
  resourceAttributes: (import 'resourceAttributes.libsonnet'),
  resourceRule: (import 'resourceRule.libsonnet'),
  selfSubjectAccessReview: (import 'selfSubjectAccessReview.libsonnet'),
  selfSubjectAccessReviewSpec: (import 'selfSubjectAccessReviewSpec.libsonnet'),
  selfSubjectRulesReview: (import 'selfSubjectRulesReview.libsonnet'),
  selfSubjectRulesReviewSpec: (import 'selfSubjectRulesReviewSpec.libsonnet'),
  subjectAccessReview: (import 'subjectAccessReview.libsonnet'),
  subjectAccessReviewSpec: (import 'subjectAccessReviewSpec.libsonnet'),
  subjectAccessReviewStatus: (import 'subjectAccessReviewStatus.libsonnet'),
  subjectRulesReviewStatus: (import 'subjectRulesReviewStatus.libsonnet')
}