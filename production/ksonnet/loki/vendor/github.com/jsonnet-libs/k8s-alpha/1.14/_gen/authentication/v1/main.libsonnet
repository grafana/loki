{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1', url='', help=''),
  tokenReview: (import 'tokenReview.libsonnet'),
  tokenReviewSpec: (import 'tokenReviewSpec.libsonnet'),
  tokenReviewStatus: (import 'tokenReviewStatus.libsonnet'),
  userInfo: (import 'userInfo.libsonnet')
}