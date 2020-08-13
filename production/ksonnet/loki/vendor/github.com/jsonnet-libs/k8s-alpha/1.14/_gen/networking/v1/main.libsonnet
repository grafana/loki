{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='v1', url='', help=''),
  ipBlock: (import 'ipBlock.libsonnet'),
  networkPolicy: (import 'networkPolicy.libsonnet'),
  networkPolicyEgressRule: (import 'networkPolicyEgressRule.libsonnet'),
  networkPolicyIngressRule: (import 'networkPolicyIngressRule.libsonnet'),
  networkPolicyPeer: (import 'networkPolicyPeer.libsonnet'),
  networkPolicyPort: (import 'networkPolicyPort.libsonnet'),
  networkPolicySpec: (import 'networkPolicySpec.libsonnet')
}