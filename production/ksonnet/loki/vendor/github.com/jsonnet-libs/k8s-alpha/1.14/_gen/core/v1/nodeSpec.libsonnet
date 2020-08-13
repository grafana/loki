{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='nodeSpec', url='', help='NodeSpec describes the attributes that a node is created with.'),
  '#configSource':: d.obj(help='NodeConfigSource specifies a source of node configuration. Exactly one subfield (excluding metadata) must be non-nil.'),
  configSource: {
    '#configMap':: d.obj(help='ConfigMapNodeConfigSource contains the information to reference a ConfigMap as a config source for the Node.'),
    configMap: {
      '#withKubeletConfigKey':: d.fn(help='KubeletConfigKey declares which key of the referenced ConfigMap corresponds to the KubeletConfiguration structure This field is required in all cases.', args=[d.arg(name='kubeletConfigKey', type=d.T.string)]),
      withKubeletConfigKey(kubeletConfigKey): { configSource+: { configMap+: { kubeletConfigKey: kubeletConfigKey } } },
      '#withName':: d.fn(help='Name is the metadata.name of the referenced ConfigMap. This field is required in all cases.', args=[d.arg(name='name', type=d.T.string)]),
      withName(name): { configSource+: { configMap+: { name: name } } },
      '#withNamespace':: d.fn(help='Namespace is the metadata.namespace of the referenced ConfigMap. This field is required in all cases.', args=[d.arg(name='namespace', type=d.T.string)]),
      withNamespace(namespace): { configSource+: { configMap+: { namespace: namespace } } },
      '#withResourceVersion':: d.fn(help='ResourceVersion is the metadata.ResourceVersion of the referenced ConfigMap. This field is forbidden in Node.Spec, and required in Node.Status.', args=[d.arg(name='resourceVersion', type=d.T.string)]),
      withResourceVersion(resourceVersion): { configSource+: { configMap+: { resourceVersion: resourceVersion } } },
      '#withUid':: d.fn(help='UID is the metadata.UID of the referenced ConfigMap. This field is forbidden in Node.Spec, and required in Node.Status.', args=[d.arg(name='uid', type=d.T.string)]),
      withUid(uid): { configSource+: { configMap+: { uid: uid } } }
    }
  },
  '#withExternalID':: d.fn(help='Deprecated. Not all kubelets will set this field. Remove field after 1.13. see: https://issues.k8s.io/61966', args=[d.arg(name='externalID', type=d.T.string)]),
  withExternalID(externalID): { externalID: externalID },
  '#withPodCIDR':: d.fn(help='PodCIDR represents the pod IP range assigned to the node.', args=[d.arg(name='podCIDR', type=d.T.string)]),
  withPodCIDR(podCIDR): { podCIDR: podCIDR },
  '#withProviderID':: d.fn(help='ID of the node assigned by the cloud provider in the format: <ProviderName>://<ProviderSpecificNodeID>', args=[d.arg(name='providerID', type=d.T.string)]),
  withProviderID(providerID): { providerID: providerID },
  '#withTaints':: d.fn(help="If specified, the node's taints.", args=[d.arg(name='taints', type=d.T.array)]),
  withTaints(taints): { taints: if std.isArray(v=taints) then taints else [taints] },
  '#withTaintsMixin':: d.fn(help="If specified, the node's taints.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='taints', type=d.T.array)]),
  withTaintsMixin(taints): { taints+: if std.isArray(v=taints) then taints else [taints] },
  '#withUnschedulable':: d.fn(help='Unschedulable controls node schedulability of new pods. By default, node is schedulable. More info: https://kubernetes.io/docs/concepts/nodes/node/#manual-node-administration', args=[d.arg(name='unschedulable', type=d.T.boolean)]),
  withUnschedulable(unschedulable): { unschedulable: unschedulable },
  '#mixin': 'ignore',
  mixin: self
}