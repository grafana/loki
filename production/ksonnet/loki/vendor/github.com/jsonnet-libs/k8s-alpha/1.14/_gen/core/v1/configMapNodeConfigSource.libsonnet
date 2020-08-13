{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='configMapNodeConfigSource', url='', help='ConfigMapNodeConfigSource contains the information to reference a ConfigMap as a config source for the Node.'),
  '#withKubeletConfigKey':: d.fn(help='KubeletConfigKey declares which key of the referenced ConfigMap corresponds to the KubeletConfiguration structure This field is required in all cases.', args=[d.arg(name='kubeletConfigKey', type=d.T.string)]),
  withKubeletConfigKey(kubeletConfigKey): { kubeletConfigKey: kubeletConfigKey },
  '#withName':: d.fn(help='Name is the metadata.name of the referenced ConfigMap. This field is required in all cases.', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#withNamespace':: d.fn(help='Namespace is the metadata.namespace of the referenced ConfigMap. This field is required in all cases.', args=[d.arg(name='namespace', type=d.T.string)]),
  withNamespace(namespace): { namespace: namespace },
  '#withResourceVersion':: d.fn(help='ResourceVersion is the metadata.ResourceVersion of the referenced ConfigMap. This field is forbidden in Node.Spec, and required in Node.Status.', args=[d.arg(name='resourceVersion', type=d.T.string)]),
  withResourceVersion(resourceVersion): { resourceVersion: resourceVersion },
  '#withUid':: d.fn(help='UID is the metadata.UID of the referenced ConfigMap. This field is forbidden in Node.Spec, and required in Node.Status.', args=[d.arg(name='uid', type=d.T.string)]),
  withUid(uid): { uid: uid },
  '#mixin': 'ignore',
  mixin: self
}