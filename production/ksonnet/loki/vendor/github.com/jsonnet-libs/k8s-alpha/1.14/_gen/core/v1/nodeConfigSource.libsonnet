{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='nodeConfigSource', url='', help='NodeConfigSource specifies a source of node configuration. Exactly one subfield (excluding metadata) must be non-nil.'),
  '#configMap':: d.obj(help='ConfigMapNodeConfigSource contains the information to reference a ConfigMap as a config source for the Node.'),
  configMap: {
    '#withKubeletConfigKey':: d.fn(help='KubeletConfigKey declares which key of the referenced ConfigMap corresponds to the KubeletConfiguration structure This field is required in all cases.', args=[d.arg(name='kubeletConfigKey', type=d.T.string)]),
    withKubeletConfigKey(kubeletConfigKey): { configMap+: { kubeletConfigKey: kubeletConfigKey } },
    '#withName':: d.fn(help='Name is the metadata.name of the referenced ConfigMap. This field is required in all cases.', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { configMap+: { name: name } },
    '#withNamespace':: d.fn(help='Namespace is the metadata.namespace of the referenced ConfigMap. This field is required in all cases.', args=[d.arg(name='namespace', type=d.T.string)]),
    withNamespace(namespace): { configMap+: { namespace: namespace } },
    '#withResourceVersion':: d.fn(help='ResourceVersion is the metadata.ResourceVersion of the referenced ConfigMap. This field is forbidden in Node.Spec, and required in Node.Status.', args=[d.arg(name='resourceVersion', type=d.T.string)]),
    withResourceVersion(resourceVersion): { configMap+: { resourceVersion: resourceVersion } },
    '#withUid':: d.fn(help='UID is the metadata.UID of the referenced ConfigMap. This field is forbidden in Node.Spec, and required in Node.Status.', args=[d.arg(name='uid', type=d.T.string)]),
    withUid(uid): { configMap+: { uid: uid } }
  },
  '#mixin': 'ignore',
  mixin: self
}