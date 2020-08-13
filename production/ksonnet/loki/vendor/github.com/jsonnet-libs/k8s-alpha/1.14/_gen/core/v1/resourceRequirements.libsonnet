{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='resourceRequirements', url='', help='ResourceRequirements describes the compute resource requirements.'),
  '#withLimits':: d.fn(help='Limits describes the maximum amount of compute resources allowed. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/', args=[d.arg(name='limits', type=d.T.object)]),
  withLimits(limits): { limits: limits },
  '#withLimitsMixin':: d.fn(help='Limits describes the maximum amount of compute resources allowed. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='limits', type=d.T.object)]),
  withLimitsMixin(limits): { limits+: limits },
  '#withRequests':: d.fn(help='Requests describes the minimum amount of compute resources required. If Requests is omitted for a container, it defaults to Limits if that is explicitly specified, otherwise to an implementation-defined value. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/', args=[d.arg(name='requests', type=d.T.object)]),
  withRequests(requests): { requests: requests },
  '#withRequestsMixin':: d.fn(help='Requests describes the minimum amount of compute resources required. If Requests is omitted for a container, it defaults to Limits if that is explicitly specified, otherwise to an implementation-defined value. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='requests', type=d.T.object)]),
  withRequestsMixin(requests): { requests+: requests },
  '#mixin': 'ignore',
  mixin: self
}