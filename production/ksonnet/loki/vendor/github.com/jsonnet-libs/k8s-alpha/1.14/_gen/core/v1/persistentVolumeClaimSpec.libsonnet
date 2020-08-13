{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='persistentVolumeClaimSpec', url='', help='PersistentVolumeClaimSpec describes the common attributes of storage devices and allows a Source for provider-specific attributes'),
  '#dataSource':: d.obj(help='TypedLocalObjectReference contains enough information to let you locate the typed referenced object inside the same namespace.'),
  dataSource: {
    '#withApiGroup':: d.fn(help='APIGroup is the group for the resource being referenced. If APIGroup is not specified, the specified Kind must be in the core API group. For any other third-party types, APIGroup is required.', args=[d.arg(name='apiGroup', type=d.T.string)]),
    withApiGroup(apiGroup): { dataSource+: { apiGroup: apiGroup } },
    '#withKind':: d.fn(help='Kind is the type of resource being referenced', args=[d.arg(name='kind', type=d.T.string)]),
    withKind(kind): { dataSource+: { kind: kind } },
    '#withName':: d.fn(help='Name is the name of resource being referenced', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { dataSource+: { name: name } }
  },
  '#resources':: d.obj(help='ResourceRequirements describes the compute resource requirements.'),
  resources: {
    '#withLimits':: d.fn(help='Limits describes the maximum amount of compute resources allowed. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/', args=[d.arg(name='limits', type=d.T.object)]),
    withLimits(limits): { resources+: { limits: limits } },
    '#withLimitsMixin':: d.fn(help='Limits describes the maximum amount of compute resources allowed. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='limits', type=d.T.object)]),
    withLimitsMixin(limits): { resources+: { limits+: limits } },
    '#withRequests':: d.fn(help='Requests describes the minimum amount of compute resources required. If Requests is omitted for a container, it defaults to Limits if that is explicitly specified, otherwise to an implementation-defined value. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/', args=[d.arg(name='requests', type=d.T.object)]),
    withRequests(requests): { resources+: { requests: requests } },
    '#withRequestsMixin':: d.fn(help='Requests describes the minimum amount of compute resources required. If Requests is omitted for a container, it defaults to Limits if that is explicitly specified, otherwise to an implementation-defined value. More info: https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='requests', type=d.T.object)]),
    withRequestsMixin(requests): { resources+: { requests+: requests } }
  },
  '#selector':: d.obj(help='A label selector is a label query over a set of resources. The result of matchLabels and matchExpressions are ANDed. An empty label selector matches all objects. A null label selector matches no objects.'),
  selector: {
    '#withMatchExpressions':: d.fn(help='matchExpressions is a list of label selector requirements. The requirements are ANDed.', args=[d.arg(name='matchExpressions', type=d.T.array)]),
    withMatchExpressions(matchExpressions): { selector+: { matchExpressions: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] } },
    '#withMatchExpressionsMixin':: d.fn(help='matchExpressions is a list of label selector requirements. The requirements are ANDed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='matchExpressions', type=d.T.array)]),
    withMatchExpressionsMixin(matchExpressions): { selector+: { matchExpressions+: if std.isArray(v=matchExpressions) then matchExpressions else [matchExpressions] } },
    '#withMatchLabels':: d.fn(help='matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.', args=[d.arg(name='matchLabels', type=d.T.object)]),
    withMatchLabels(matchLabels): { selector+: { matchLabels: matchLabels } },
    '#withMatchLabelsMixin':: d.fn(help='matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels map is equivalent to an element of matchExpressions, whose key field is "key", the operator is "In", and the values array contains only "value". The requirements are ANDed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='matchLabels', type=d.T.object)]),
    withMatchLabelsMixin(matchLabels): { selector+: { matchLabels+: matchLabels } }
  },
  '#withAccessModes':: d.fn(help='AccessModes contains the desired access modes the volume should have. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#access-modes-1', args=[d.arg(name='accessModes', type=d.T.array)]),
  withAccessModes(accessModes): { accessModes: if std.isArray(v=accessModes) then accessModes else [accessModes] },
  '#withAccessModesMixin':: d.fn(help='AccessModes contains the desired access modes the volume should have. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#access-modes-1\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='accessModes', type=d.T.array)]),
  withAccessModesMixin(accessModes): { accessModes+: if std.isArray(v=accessModes) then accessModes else [accessModes] },
  '#withStorageClassName':: d.fn(help='Name of the StorageClass required by the claim. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#class-1', args=[d.arg(name='storageClassName', type=d.T.string)]),
  withStorageClassName(storageClassName): { storageClassName: storageClassName },
  '#withVolumeMode':: d.fn(help='volumeMode defines what type of volume is required by the claim. Value of Filesystem is implied when not included in claim spec. This is a beta feature.', args=[d.arg(name='volumeMode', type=d.T.string)]),
  withVolumeMode(volumeMode): { volumeMode: volumeMode },
  '#withVolumeName':: d.fn(help='VolumeName is the binding reference to the PersistentVolume backing this claim.', args=[d.arg(name='volumeName', type=d.T.string)]),
  withVolumeName(volumeName): { volumeName: volumeName },
  '#mixin': 'ignore',
  mixin: self
}