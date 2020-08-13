{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='nodeConfigStatus', url='', help='NodeConfigStatus describes the status of the config assigned by Node.Spec.ConfigSource.'),
  '#active':: d.obj(help='NodeConfigSource specifies a source of node configuration. Exactly one subfield (excluding metadata) must be non-nil.'),
  active: {
    '#configMap':: d.obj(help='ConfigMapNodeConfigSource contains the information to reference a ConfigMap as a config source for the Node.'),
    configMap: {
      '#withKubeletConfigKey':: d.fn(help='KubeletConfigKey declares which key of the referenced ConfigMap corresponds to the KubeletConfiguration structure This field is required in all cases.', args=[d.arg(name='kubeletConfigKey', type=d.T.string)]),
      withKubeletConfigKey(kubeletConfigKey): { active+: { configMap+: { kubeletConfigKey: kubeletConfigKey } } },
      '#withName':: d.fn(help='Name is the metadata.name of the referenced ConfigMap. This field is required in all cases.', args=[d.arg(name='name', type=d.T.string)]),
      withName(name): { active+: { configMap+: { name: name } } },
      '#withNamespace':: d.fn(help='Namespace is the metadata.namespace of the referenced ConfigMap. This field is required in all cases.', args=[d.arg(name='namespace', type=d.T.string)]),
      withNamespace(namespace): { active+: { configMap+: { namespace: namespace } } },
      '#withResourceVersion':: d.fn(help='ResourceVersion is the metadata.ResourceVersion of the referenced ConfigMap. This field is forbidden in Node.Spec, and required in Node.Status.', args=[d.arg(name='resourceVersion', type=d.T.string)]),
      withResourceVersion(resourceVersion): { active+: { configMap+: { resourceVersion: resourceVersion } } },
      '#withUid':: d.fn(help='UID is the metadata.UID of the referenced ConfigMap. This field is forbidden in Node.Spec, and required in Node.Status.', args=[d.arg(name='uid', type=d.T.string)]),
      withUid(uid): { active+: { configMap+: { uid: uid } } }
    }
  },
  '#assigned':: d.obj(help='NodeConfigSource specifies a source of node configuration. Exactly one subfield (excluding metadata) must be non-nil.'),
  assigned: {
    '#configMap':: d.obj(help='ConfigMapNodeConfigSource contains the information to reference a ConfigMap as a config source for the Node.'),
    configMap: {
      '#withKubeletConfigKey':: d.fn(help='KubeletConfigKey declares which key of the referenced ConfigMap corresponds to the KubeletConfiguration structure This field is required in all cases.', args=[d.arg(name='kubeletConfigKey', type=d.T.string)]),
      withKubeletConfigKey(kubeletConfigKey): { assigned+: { configMap+: { kubeletConfigKey: kubeletConfigKey } } },
      '#withName':: d.fn(help='Name is the metadata.name of the referenced ConfigMap. This field is required in all cases.', args=[d.arg(name='name', type=d.T.string)]),
      withName(name): { assigned+: { configMap+: { name: name } } },
      '#withNamespace':: d.fn(help='Namespace is the metadata.namespace of the referenced ConfigMap. This field is required in all cases.', args=[d.arg(name='namespace', type=d.T.string)]),
      withNamespace(namespace): { assigned+: { configMap+: { namespace: namespace } } },
      '#withResourceVersion':: d.fn(help='ResourceVersion is the metadata.ResourceVersion of the referenced ConfigMap. This field is forbidden in Node.Spec, and required in Node.Status.', args=[d.arg(name='resourceVersion', type=d.T.string)]),
      withResourceVersion(resourceVersion): { assigned+: { configMap+: { resourceVersion: resourceVersion } } },
      '#withUid':: d.fn(help='UID is the metadata.UID of the referenced ConfigMap. This field is forbidden in Node.Spec, and required in Node.Status.', args=[d.arg(name='uid', type=d.T.string)]),
      withUid(uid): { assigned+: { configMap+: { uid: uid } } }
    }
  },
  '#lastKnownGood':: d.obj(help='NodeConfigSource specifies a source of node configuration. Exactly one subfield (excluding metadata) must be non-nil.'),
  lastKnownGood: {
    '#configMap':: d.obj(help='ConfigMapNodeConfigSource contains the information to reference a ConfigMap as a config source for the Node.'),
    configMap: {
      '#withKubeletConfigKey':: d.fn(help='KubeletConfigKey declares which key of the referenced ConfigMap corresponds to the KubeletConfiguration structure This field is required in all cases.', args=[d.arg(name='kubeletConfigKey', type=d.T.string)]),
      withKubeletConfigKey(kubeletConfigKey): { lastKnownGood+: { configMap+: { kubeletConfigKey: kubeletConfigKey } } },
      '#withName':: d.fn(help='Name is the metadata.name of the referenced ConfigMap. This field is required in all cases.', args=[d.arg(name='name', type=d.T.string)]),
      withName(name): { lastKnownGood+: { configMap+: { name: name } } },
      '#withNamespace':: d.fn(help='Namespace is the metadata.namespace of the referenced ConfigMap. This field is required in all cases.', args=[d.arg(name='namespace', type=d.T.string)]),
      withNamespace(namespace): { lastKnownGood+: { configMap+: { namespace: namespace } } },
      '#withResourceVersion':: d.fn(help='ResourceVersion is the metadata.ResourceVersion of the referenced ConfigMap. This field is forbidden in Node.Spec, and required in Node.Status.', args=[d.arg(name='resourceVersion', type=d.T.string)]),
      withResourceVersion(resourceVersion): { lastKnownGood+: { configMap+: { resourceVersion: resourceVersion } } },
      '#withUid':: d.fn(help='UID is the metadata.UID of the referenced ConfigMap. This field is forbidden in Node.Spec, and required in Node.Status.', args=[d.arg(name='uid', type=d.T.string)]),
      withUid(uid): { lastKnownGood+: { configMap+: { uid: uid } } }
    }
  },
  '#withError':: d.fn(help='Error describes any problems reconciling the Spec.ConfigSource to the Active config. Errors may occur, for example, attempting to checkpoint Spec.ConfigSource to the local Assigned record, attempting to checkpoint the payload associated with Spec.ConfigSource, attempting to load or validate the Assigned config, etc. Errors may occur at different points while syncing config. Earlier errors (e.g. download or checkpointing errors) will not result in a rollback to LastKnownGood, and may resolve across Kubelet retries. Later errors (e.g. loading or validating a checkpointed config) will result in a rollback to LastKnownGood. In the latter case, it is usually possible to resolve the error by fixing the config assigned in Spec.ConfigSource. You can find additional information for debugging by searching the error message in the Kubelet log. Error is a human-readable description of the error state; machines can check whether or not Error is empty, but should not rely on the stability of the Error text across Kubelet versions.', args=[d.arg(name='err', type=d.T.string)]),
  withError(err): { 'error': err },
  '#mixin': 'ignore',
  mixin: self
}