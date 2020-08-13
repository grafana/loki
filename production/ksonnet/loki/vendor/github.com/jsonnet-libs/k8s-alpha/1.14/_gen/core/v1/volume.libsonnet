{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='volume', url='', help='Volume represents a named volume in a pod that may be accessed by any container in the pod.'),
  '#awsElasticBlockStore':: d.obj(help='Represents a Persistent Disk resource in AWS.\n\nAn AWS EBS disk must exist before mounting to a container. The disk must also be in the same AWS zone as the kubelet. An AWS EBS disk can only be mounted as read/write once. AWS EBS volumes support ownership management and SELinux relabeling.'),
  awsElasticBlockStore: {
    '#withFsType':: d.fn(help='Filesystem type of the volume that you want to mount. Tip: Ensure that the filesystem type is supported by the host operating system. Examples: "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified. More info: https://kubernetes.io/docs/concepts/storage/volumes#awselasticblockstore', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { awsElasticBlockStore+: { fsType: fsType } },
    '#withPartition':: d.fn(help='The partition in the volume that you want to mount. If omitted, the default is to mount by volume name. Examples: For volume /dev/sda1, you specify the partition as "1". Similarly, the volume partition for /dev/sda is "0" (or you can leave the property empty).', args=[d.arg(name='partition', type=d.T.integer)]),
    withPartition(partition): { awsElasticBlockStore+: { partition: partition } },
    '#withReadOnly':: d.fn(help='Specify "true" to force and set the ReadOnly property in VolumeMounts to "true". If omitted, the default is "false". More info: https://kubernetes.io/docs/concepts/storage/volumes#awselasticblockstore', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { awsElasticBlockStore+: { readOnly: readOnly } },
    '#withVolumeID':: d.fn(help='Unique ID of the persistent disk resource in AWS (Amazon EBS volume). More info: https://kubernetes.io/docs/concepts/storage/volumes#awselasticblockstore', args=[d.arg(name='volumeID', type=d.T.string)]),
    withVolumeID(volumeID): { awsElasticBlockStore+: { volumeID: volumeID } }
  },
  '#azureDisk':: d.obj(help='AzureDisk represents an Azure Data Disk mount on the host and bind mount to the pod.'),
  azureDisk: {
    '#withCachingMode':: d.fn(help='Host Caching mode: None, Read Only, Read Write.', args=[d.arg(name='cachingMode', type=d.T.string)]),
    withCachingMode(cachingMode): { azureDisk+: { cachingMode: cachingMode } },
    '#withDiskName':: d.fn(help='The Name of the data disk in the blob storage', args=[d.arg(name='diskName', type=d.T.string)]),
    withDiskName(diskName): { azureDisk+: { diskName: diskName } },
    '#withDiskURI':: d.fn(help='The URI the data disk in the blob storage', args=[d.arg(name='diskURI', type=d.T.string)]),
    withDiskURI(diskURI): { azureDisk+: { diskURI: diskURI } },
    '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified.', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { azureDisk+: { fsType: fsType } },
    '#withKind':: d.fn(help='Expected values Shared: multiple blob disks per storage account  Dedicated: single blob disk per storage account  Managed: azure managed data disk (only in managed availability set). defaults to shared', args=[d.arg(name='kind', type=d.T.string)]),
    withKind(kind): { azureDisk+: { kind: kind } },
    '#withReadOnly':: d.fn(help='Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { azureDisk+: { readOnly: readOnly } }
  },
  '#azureFile':: d.obj(help='AzureFile represents an Azure File Service mount on the host and bind mount to the pod.'),
  azureFile: {
    '#withReadOnly':: d.fn(help='Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { azureFile+: { readOnly: readOnly } },
    '#withSecretName':: d.fn(help='the name of secret that contains Azure Storage Account Name and Key', args=[d.arg(name='secretName', type=d.T.string)]),
    withSecretName(secretName): { azureFile+: { secretName: secretName } },
    '#withShareName':: d.fn(help='Share Name', args=[d.arg(name='shareName', type=d.T.string)]),
    withShareName(shareName): { azureFile+: { shareName: shareName } }
  },
  '#cephfs':: d.obj(help='Represents a Ceph Filesystem mount that lasts the lifetime of a pod Cephfs volumes do not support ownership management or SELinux relabeling.'),
  cephfs: {
    '#secretRef':: d.obj(help='LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.'),
    secretRef: {
      '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
      withName(name): { cephfs+: { secretRef+: { name: name } } }
    },
    '#withMonitors':: d.fn(help='Required: Monitors is a collection of Ceph monitors More info: https://releases.k8s.io/HEAD/examples/volumes/cephfs/README.md#how-to-use-it', args=[d.arg(name='monitors', type=d.T.array)]),
    withMonitors(monitors): { cephfs+: { monitors: if std.isArray(v=monitors) then monitors else [monitors] } },
    '#withMonitorsMixin':: d.fn(help='Required: Monitors is a collection of Ceph monitors More info: https://releases.k8s.io/HEAD/examples/volumes/cephfs/README.md#how-to-use-it\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='monitors', type=d.T.array)]),
    withMonitorsMixin(monitors): { cephfs+: { monitors+: if std.isArray(v=monitors) then monitors else [monitors] } },
    '#withPath':: d.fn(help='Optional: Used as the mounted root, rather than the full Ceph tree, default is /', args=[d.arg(name='path', type=d.T.string)]),
    withPath(path): { cephfs+: { path: path } },
    '#withReadOnly':: d.fn(help='Optional: Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts. More info: https://releases.k8s.io/HEAD/examples/volumes/cephfs/README.md#how-to-use-it', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { cephfs+: { readOnly: readOnly } },
    '#withSecretFile':: d.fn(help='Optional: SecretFile is the path to key ring for User, default is /etc/ceph/user.secret More info: https://releases.k8s.io/HEAD/examples/volumes/cephfs/README.md#how-to-use-it', args=[d.arg(name='secretFile', type=d.T.string)]),
    withSecretFile(secretFile): { cephfs+: { secretFile: secretFile } },
    '#withUser':: d.fn(help='Optional: User is the rados user name, default is admin More info: https://releases.k8s.io/HEAD/examples/volumes/cephfs/README.md#how-to-use-it', args=[d.arg(name='user', type=d.T.string)]),
    withUser(user): { cephfs+: { user: user } }
  },
  '#cinder':: d.obj(help='Represents a cinder volume resource in Openstack. A Cinder volume must exist before mounting to a container. The volume must also be in the same region as the kubelet. Cinder volumes support ownership management and SELinux relabeling.'),
  cinder: {
    '#secretRef':: d.obj(help='LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.'),
    secretRef: {
      '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
      withName(name): { cinder+: { secretRef+: { name: name } } }
    },
    '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Examples: "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified. More info: https://releases.k8s.io/HEAD/examples/mysql-cinder-pd/README.md', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { cinder+: { fsType: fsType } },
    '#withReadOnly':: d.fn(help='Optional: Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts. More info: https://releases.k8s.io/HEAD/examples/mysql-cinder-pd/README.md', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { cinder+: { readOnly: readOnly } },
    '#withVolumeID':: d.fn(help='volume id used to identify the volume in cinder More info: https://releases.k8s.io/HEAD/examples/mysql-cinder-pd/README.md', args=[d.arg(name='volumeID', type=d.T.string)]),
    withVolumeID(volumeID): { cinder+: { volumeID: volumeID } }
  },
  '#configMap':: d.obj(help="Adapts a ConfigMap into a volume.\n\nThe contents of the target ConfigMap's Data field will be presented in a volume as files using the keys in the Data field as the file names, unless the items element is populated with specific mappings of keys to paths. ConfigMap volumes support ownership management and SELinux relabeling."),
  configMap: {
    '#withDefaultMode':: d.fn(help='Optional: mode bits to use on created files by default. Must be a value between 0 and 0777. Defaults to 0644. Directories within the path are not affected by this setting. This might be in conflict with other options that affect the file mode, like fsGroup, and the result can be other mode bits set.', args=[d.arg(name='defaultMode', type=d.T.integer)]),
    withDefaultMode(defaultMode): { configMap+: { defaultMode: defaultMode } },
    '#withItems':: d.fn(help="If unspecified, each key-value pair in the Data field of the referenced ConfigMap will be projected into the volume as a file whose name is the key and content is the value. If specified, the listed keys will be projected into the specified paths, and unlisted keys will not be present. If a key is specified which is not present in the ConfigMap, the volume setup will error unless it is marked optional. Paths must be relative and may not contain the '..' path or start with '..'.", args=[d.arg(name='items', type=d.T.array)]),
    withItems(items): { configMap+: { items: if std.isArray(v=items) then items else [items] } },
    '#withItemsMixin':: d.fn(help="If unspecified, each key-value pair in the Data field of the referenced ConfigMap will be projected into the volume as a file whose name is the key and content is the value. If specified, the listed keys will be projected into the specified paths, and unlisted keys will not be present. If a key is specified which is not present in the ConfigMap, the volume setup will error unless it is marked optional. Paths must be relative and may not contain the '..' path or start with '..'.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='items', type=d.T.array)]),
    withItemsMixin(items): { configMap+: { items+: if std.isArray(v=items) then items else [items] } },
    '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { configMap+: { name: name } },
    '#withOptional':: d.fn(help="Specify whether the ConfigMap or it's keys must be defined", args=[d.arg(name='optional', type=d.T.boolean)]),
    withOptional(optional): { configMap+: { optional: optional } }
  },
  '#csi':: d.obj(help='Represents a source location of a volume to mount, managed by an external CSI driver'),
  csi: {
    '#nodePublishSecretRef':: d.obj(help='LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.'),
    nodePublishSecretRef: {
      '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
      withName(name): { csi+: { nodePublishSecretRef+: { name: name } } }
    },
    '#withDriver':: d.fn(help='Driver is the name of the CSI driver that handles this volume. Consult with your admin for the correct name as registered in the cluster.', args=[d.arg(name='driver', type=d.T.string)]),
    withDriver(driver): { csi+: { driver: driver } },
    '#withFsType':: d.fn(help='Filesystem type to mount. Ex. "ext4", "xfs", "ntfs". If not provided, the empty value is passed to the associated CSI driver which will determine the default filesystem to apply.', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { csi+: { fsType: fsType } },
    '#withReadOnly':: d.fn(help='Specifies a read-only configuration for the volume. Defaults to false (read/write).', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { csi+: { readOnly: readOnly } },
    '#withVolumeAttributes':: d.fn(help="VolumeAttributes stores driver-specific properties that are passed to the CSI driver. Consult your driver's documentation for supported values.", args=[d.arg(name='volumeAttributes', type=d.T.object)]),
    withVolumeAttributes(volumeAttributes): { csi+: { volumeAttributes: volumeAttributes } },
    '#withVolumeAttributesMixin':: d.fn(help="VolumeAttributes stores driver-specific properties that are passed to the CSI driver. Consult your driver's documentation for supported values.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='volumeAttributes', type=d.T.object)]),
    withVolumeAttributesMixin(volumeAttributes): { csi+: { volumeAttributes+: volumeAttributes } }
  },
  '#downwardAPI':: d.obj(help='DownwardAPIVolumeSource represents a volume containing downward API info. Downward API volumes support ownership management and SELinux relabeling.'),
  downwardAPI: {
    '#withDefaultMode':: d.fn(help='Optional: mode bits to use on created files by default. Must be a value between 0 and 0777. Defaults to 0644. Directories within the path are not affected by this setting. This might be in conflict with other options that affect the file mode, like fsGroup, and the result can be other mode bits set.', args=[d.arg(name='defaultMode', type=d.T.integer)]),
    withDefaultMode(defaultMode): { downwardAPI+: { defaultMode: defaultMode } },
    '#withItems':: d.fn(help='Items is a list of downward API volume file', args=[d.arg(name='items', type=d.T.array)]),
    withItems(items): { downwardAPI+: { items: if std.isArray(v=items) then items else [items] } },
    '#withItemsMixin':: d.fn(help='Items is a list of downward API volume file\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='items', type=d.T.array)]),
    withItemsMixin(items): { downwardAPI+: { items+: if std.isArray(v=items) then items else [items] } }
  },
  '#emptyDir':: d.obj(help='Represents an empty directory for a pod. Empty directory volumes support ownership management and SELinux relabeling.'),
  emptyDir: {
    '#withMedium':: d.fn(help="What type of storage medium should back this directory. The default is '' which means to use the node's default medium. Must be an empty string (default) or Memory. More info: https://kubernetes.io/docs/concepts/storage/volumes#emptydir", args=[d.arg(name='medium', type=d.T.string)]),
    withMedium(medium): { emptyDir+: { medium: medium } },
    '#withSizeLimit':: d.fn(help="Quantity is a fixed-point representation of a number. It provides convenient marshaling/unmarshaling in JSON and YAML, in addition to String() and Int64() accessors.\n\nThe serialization format is:\n\n<quantity>        ::= <signedNumber><suffix>\n  (Note that <suffix> may be empty, from the '' case in <decimalSI>.)\n<digit>           ::= 0 | 1 | ... | 9 <digits>          ::= <digit> | <digit><digits> <number>          ::= <digits> | <digits>.<digits> | <digits>. | .<digits> <sign>            ::= '+' | '-' <signedNumber>    ::= <number> | <sign><number> <suffix>          ::= <binarySI> | <decimalExponent> | <decimalSI> <binarySI>        ::= Ki | Mi | Gi | Ti | Pi | Ei\n  (International System of units; See: http://physics.nist.gov/cuu/Units/binary.html)\n<decimalSI>       ::= m | '' | k | M | G | T | P | E\n  (Note that 1024 = 1Ki but 1000 = 1k; I didn't choose the capitalization.)\n<decimalExponent> ::= 'e' <signedNumber> | 'E' <signedNumber>\n\nNo matter which of the three exponent forms is used, no quantity may represent a number greater than 2^63-1 in magnitude, nor may it have more than 3 decimal places. Numbers larger or more precise will be capped or rounded up. (E.g.: 0.1m will rounded up to 1m.) This may be extended in the future if we require larger or smaller quantities.\n\nWhen a Quantity is parsed from a string, it will remember the type of suffix it had, and will use the same type again when it is serialized.\n\nBefore serializing, Quantity will be put in 'canonical form'. This means that Exponent/suffix will be adjusted up or down (with a corresponding increase or decrease in Mantissa) such that:\n  a. No precision is lost\n  b. No fractional digits will be emitted\n  c. The exponent (or suffix) is as large as possible.\nThe sign will be omitted unless the number is negative.\n\nExamples:\n  1.5 will be serialized as '1500m'\n  1.5Gi will be serialized as '1536Mi'\n\nNote that the quantity will NEVER be internally represented by a floating point number. That is the whole point of this exercise.\n\nNon-canonical values will still parse as long as they are well formed, but will be re-emitted in their canonical form. (So always use canonical form, or don't diff.)\n\nThis format is intended to make it difficult to use these numbers without writing some sort of special handling code in the hopes that that will cause implementors to also use a fixed point implementation.", args=[d.arg(name='sizeLimit', type=d.T.string)]),
    withSizeLimit(sizeLimit): { emptyDir+: { sizeLimit: sizeLimit } }
  },
  '#fc':: d.obj(help='Represents a Fibre Channel volume. Fibre Channel volumes can only be mounted as read/write once. Fibre Channel volumes support ownership management and SELinux relabeling.'),
  fc: {
    '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified.', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { fc+: { fsType: fsType } },
    '#withLun':: d.fn(help='Optional: FC target lun number', args=[d.arg(name='lun', type=d.T.integer)]),
    withLun(lun): { fc+: { lun: lun } },
    '#withReadOnly':: d.fn(help='Optional: Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { fc+: { readOnly: readOnly } },
    '#withTargetWWNs':: d.fn(help='Optional: FC target worldwide names (WWNs)', args=[d.arg(name='targetWWNs', type=d.T.array)]),
    withTargetWWNs(targetWWNs): { fc+: { targetWWNs: if std.isArray(v=targetWWNs) then targetWWNs else [targetWWNs] } },
    '#withTargetWWNsMixin':: d.fn(help='Optional: FC target worldwide names (WWNs)\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='targetWWNs', type=d.T.array)]),
    withTargetWWNsMixin(targetWWNs): { fc+: { targetWWNs+: if std.isArray(v=targetWWNs) then targetWWNs else [targetWWNs] } },
    '#withWwids':: d.fn(help='Optional: FC volume world wide identifiers (wwids) Either wwids or combination of targetWWNs and lun must be set, but not both simultaneously.', args=[d.arg(name='wwids', type=d.T.array)]),
    withWwids(wwids): { fc+: { wwids: if std.isArray(v=wwids) then wwids else [wwids] } },
    '#withWwidsMixin':: d.fn(help='Optional: FC volume world wide identifiers (wwids) Either wwids or combination of targetWWNs and lun must be set, but not both simultaneously.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='wwids', type=d.T.array)]),
    withWwidsMixin(wwids): { fc+: { wwids+: if std.isArray(v=wwids) then wwids else [wwids] } }
  },
  '#flexVolume':: d.obj(help='FlexVolume represents a generic volume resource that is provisioned/attached using an exec based plugin.'),
  flexVolume: {
    '#secretRef':: d.obj(help='LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.'),
    secretRef: {
      '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
      withName(name): { flexVolume+: { secretRef+: { name: name } } }
    },
    '#withDriver':: d.fn(help='Driver is the name of the driver to use for this volume.', args=[d.arg(name='driver', type=d.T.string)]),
    withDriver(driver): { flexVolume+: { driver: driver } },
    '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". The default filesystem depends on FlexVolume script.', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { flexVolume+: { fsType: fsType } },
    '#withOptions':: d.fn(help='Optional: Extra command options if any.', args=[d.arg(name='options', type=d.T.object)]),
    withOptions(options): { flexVolume+: { options: options } },
    '#withOptionsMixin':: d.fn(help='Optional: Extra command options if any.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='options', type=d.T.object)]),
    withOptionsMixin(options): { flexVolume+: { options+: options } },
    '#withReadOnly':: d.fn(help='Optional: Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { flexVolume+: { readOnly: readOnly } }
  },
  '#flocker':: d.obj(help='Represents a Flocker volume mounted by the Flocker agent. One and only one of datasetName and datasetUUID should be set. Flocker volumes do not support ownership management or SELinux relabeling.'),
  flocker: {
    '#withDatasetName':: d.fn(help='Name of the dataset stored as metadata -> name on the dataset for Flocker should be considered as deprecated', args=[d.arg(name='datasetName', type=d.T.string)]),
    withDatasetName(datasetName): { flocker+: { datasetName: datasetName } },
    '#withDatasetUUID':: d.fn(help='UUID of the dataset. This is unique identifier of a Flocker dataset', args=[d.arg(name='datasetUUID', type=d.T.string)]),
    withDatasetUUID(datasetUUID): { flocker+: { datasetUUID: datasetUUID } }
  },
  '#gcePersistentDisk':: d.obj(help='Represents a Persistent Disk resource in Google Compute Engine.\n\nA GCE PD must exist before mounting to a container. The disk must also be in the same GCE project and zone as the kubelet. A GCE PD can only be mounted as read/write once or read-only many times. GCE PDs support ownership management and SELinux relabeling.'),
  gcePersistentDisk: {
    '#withFsType':: d.fn(help='Filesystem type of the volume that you want to mount. Tip: Ensure that the filesystem type is supported by the host operating system. Examples: "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified. More info: https://kubernetes.io/docs/concepts/storage/volumes#gcepersistentdisk', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { gcePersistentDisk+: { fsType: fsType } },
    '#withPartition':: d.fn(help='The partition in the volume that you want to mount. If omitted, the default is to mount by volume name. Examples: For volume /dev/sda1, you specify the partition as "1". Similarly, the volume partition for /dev/sda is "0" (or you can leave the property empty). More info: https://kubernetes.io/docs/concepts/storage/volumes#gcepersistentdisk', args=[d.arg(name='partition', type=d.T.integer)]),
    withPartition(partition): { gcePersistentDisk+: { partition: partition } },
    '#withPdName':: d.fn(help='Unique name of the PD resource in GCE. Used to identify the disk in GCE. More info: https://kubernetes.io/docs/concepts/storage/volumes#gcepersistentdisk', args=[d.arg(name='pdName', type=d.T.string)]),
    withPdName(pdName): { gcePersistentDisk+: { pdName: pdName } },
    '#withReadOnly':: d.fn(help='ReadOnly here will force the ReadOnly setting in VolumeMounts. Defaults to false. More info: https://kubernetes.io/docs/concepts/storage/volumes#gcepersistentdisk', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { gcePersistentDisk+: { readOnly: readOnly } }
  },
  '#gitRepo':: d.obj(help="Represents a volume that is populated with the contents of a git repository. Git repo volumes do not support ownership management. Git repo volumes support SELinux relabeling.\n\nDEPRECATED: GitRepo is deprecated. To provision a container with a git repo, mount an EmptyDir into an InitContainer that clones the repo using git, then mount the EmptyDir into the Pod's container."),
  gitRepo: {
    '#withDirectory':: d.fn(help="Target directory name. Must not contain or start with '..'.  If '.' is supplied, the volume directory will be the git repository.  Otherwise, if specified, the volume will contain the git repository in the subdirectory with the given name.", args=[d.arg(name='directory', type=d.T.string)]),
    withDirectory(directory): { gitRepo+: { directory: directory } },
    '#withRepository':: d.fn(help='Repository URL', args=[d.arg(name='repository', type=d.T.string)]),
    withRepository(repository): { gitRepo+: { repository: repository } },
    '#withRevision':: d.fn(help='Commit hash for the specified revision.', args=[d.arg(name='revision', type=d.T.string)]),
    withRevision(revision): { gitRepo+: { revision: revision } }
  },
  '#glusterfs':: d.obj(help='Represents a Glusterfs mount that lasts the lifetime of a pod. Glusterfs volumes do not support ownership management or SELinux relabeling.'),
  glusterfs: {
    '#withEndpoints':: d.fn(help='EndpointsName is the endpoint name that details Glusterfs topology. More info: https://releases.k8s.io/HEAD/examples/volumes/glusterfs/README.md#create-a-pod', args=[d.arg(name='endpoints', type=d.T.string)]),
    withEndpoints(endpoints): { glusterfs+: { endpoints: endpoints } },
    '#withPath':: d.fn(help='Path is the Glusterfs volume path. More info: https://releases.k8s.io/HEAD/examples/volumes/glusterfs/README.md#create-a-pod', args=[d.arg(name='path', type=d.T.string)]),
    withPath(path): { glusterfs+: { path: path } },
    '#withReadOnly':: d.fn(help='ReadOnly here will force the Glusterfs volume to be mounted with read-only permissions. Defaults to false. More info: https://releases.k8s.io/HEAD/examples/volumes/glusterfs/README.md#create-a-pod', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { glusterfs+: { readOnly: readOnly } }
  },
  '#hostPath':: d.obj(help='Represents a host path mapped into a pod. Host path volumes do not support ownership management or SELinux relabeling.'),
  hostPath: {
    '#withPath':: d.fn(help='Path of the directory on the host. If the path is a symlink, it will follow the link to the real path. More info: https://kubernetes.io/docs/concepts/storage/volumes#hostpath', args=[d.arg(name='path', type=d.T.string)]),
    withPath(path): { hostPath+: { path: path } },
    '#withType':: d.fn(help='Type for HostPath Volume Defaults to "" More info: https://kubernetes.io/docs/concepts/storage/volumes#hostpath', args=[d.arg(name='type', type=d.T.string)]),
    withType(type): { hostPath+: { type: type } }
  },
  '#iscsi':: d.obj(help='Represents an ISCSI disk. ISCSI volumes can only be mounted as read/write once. ISCSI volumes support ownership management and SELinux relabeling.'),
  iscsi: {
    '#secretRef':: d.obj(help='LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.'),
    secretRef: {
      '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
      withName(name): { iscsi+: { secretRef+: { name: name } } }
    },
    '#withChapAuthDiscovery':: d.fn(help='whether support iSCSI Discovery CHAP authentication', args=[d.arg(name='chapAuthDiscovery', type=d.T.boolean)]),
    withChapAuthDiscovery(chapAuthDiscovery): { iscsi+: { chapAuthDiscovery: chapAuthDiscovery } },
    '#withChapAuthSession':: d.fn(help='whether support iSCSI Session CHAP authentication', args=[d.arg(name='chapAuthSession', type=d.T.boolean)]),
    withChapAuthSession(chapAuthSession): { iscsi+: { chapAuthSession: chapAuthSession } },
    '#withFsType':: d.fn(help='Filesystem type of the volume that you want to mount. Tip: Ensure that the filesystem type is supported by the host operating system. Examples: "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified. More info: https://kubernetes.io/docs/concepts/storage/volumes#iscsi', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { iscsi+: { fsType: fsType } },
    '#withInitiatorName':: d.fn(help='Custom iSCSI Initiator Name. If initiatorName is specified with iscsiInterface simultaneously, new iSCSI interface <target portal>:<volume name> will be created for the connection.', args=[d.arg(name='initiatorName', type=d.T.string)]),
    withInitiatorName(initiatorName): { iscsi+: { initiatorName: initiatorName } },
    '#withIqn':: d.fn(help='Target iSCSI Qualified Name.', args=[d.arg(name='iqn', type=d.T.string)]),
    withIqn(iqn): { iscsi+: { iqn: iqn } },
    '#withIscsiInterface':: d.fn(help="iSCSI Interface Name that uses an iSCSI transport. Defaults to 'default' (tcp).", args=[d.arg(name='iscsiInterface', type=d.T.string)]),
    withIscsiInterface(iscsiInterface): { iscsi+: { iscsiInterface: iscsiInterface } },
    '#withLun':: d.fn(help='iSCSI Target Lun number.', args=[d.arg(name='lun', type=d.T.integer)]),
    withLun(lun): { iscsi+: { lun: lun } },
    '#withPortals':: d.fn(help='iSCSI Target Portal List. The portal is either an IP or ip_addr:port if the port is other than default (typically TCP ports 860 and 3260).', args=[d.arg(name='portals', type=d.T.array)]),
    withPortals(portals): { iscsi+: { portals: if std.isArray(v=portals) then portals else [portals] } },
    '#withPortalsMixin':: d.fn(help='iSCSI Target Portal List. The portal is either an IP or ip_addr:port if the port is other than default (typically TCP ports 860 and 3260).\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='portals', type=d.T.array)]),
    withPortalsMixin(portals): { iscsi+: { portals+: if std.isArray(v=portals) then portals else [portals] } },
    '#withReadOnly':: d.fn(help='ReadOnly here will force the ReadOnly setting in VolumeMounts. Defaults to false.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { iscsi+: { readOnly: readOnly } },
    '#withTargetPortal':: d.fn(help='iSCSI Target Portal. The Portal is either an IP or ip_addr:port if the port is other than default (typically TCP ports 860 and 3260).', args=[d.arg(name='targetPortal', type=d.T.string)]),
    withTargetPortal(targetPortal): { iscsi+: { targetPortal: targetPortal } }
  },
  '#nfs':: d.obj(help='Represents an NFS mount that lasts the lifetime of a pod. NFS volumes do not support ownership management or SELinux relabeling.'),
  nfs: {
    '#withPath':: d.fn(help='Path that is exported by the NFS server. More info: https://kubernetes.io/docs/concepts/storage/volumes#nfs', args=[d.arg(name='path', type=d.T.string)]),
    withPath(path): { nfs+: { path: path } },
    '#withReadOnly':: d.fn(help='ReadOnly here will force the NFS export to be mounted with read-only permissions. Defaults to false. More info: https://kubernetes.io/docs/concepts/storage/volumes#nfs', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { nfs+: { readOnly: readOnly } },
    '#withServer':: d.fn(help='Server is the hostname or IP address of the NFS server. More info: https://kubernetes.io/docs/concepts/storage/volumes#nfs', args=[d.arg(name='server', type=d.T.string)]),
    withServer(server): { nfs+: { server: server } }
  },
  '#persistentVolumeClaim':: d.obj(help="PersistentVolumeClaimVolumeSource references the user's PVC in the same namespace. This volume finds the bound PV and mounts that volume for the pod. A PersistentVolumeClaimVolumeSource is, essentially, a wrapper around another type of volume that is owned by someone else (the system)."),
  persistentVolumeClaim: {
    '#withClaimName':: d.fn(help='ClaimName is the name of a PersistentVolumeClaim in the same namespace as the pod using this volume. More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims', args=[d.arg(name='claimName', type=d.T.string)]),
    withClaimName(claimName): { persistentVolumeClaim+: { claimName: claimName } },
    '#withReadOnly':: d.fn(help='Will force the ReadOnly setting in VolumeMounts. Default false.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { persistentVolumeClaim+: { readOnly: readOnly } }
  },
  '#photonPersistentDisk':: d.obj(help='Represents a Photon Controller persistent disk resource.'),
  photonPersistentDisk: {
    '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified.', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { photonPersistentDisk+: { fsType: fsType } },
    '#withPdID':: d.fn(help='ID that identifies Photon Controller persistent disk', args=[d.arg(name='pdID', type=d.T.string)]),
    withPdID(pdID): { photonPersistentDisk+: { pdID: pdID } }
  },
  '#portworxVolume':: d.obj(help='PortworxVolumeSource represents a Portworx volume resource.'),
  portworxVolume: {
    '#withFsType':: d.fn(help='FSType represents the filesystem type to mount Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs". Implicitly inferred to be "ext4" if unspecified.', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { portworxVolume+: { fsType: fsType } },
    '#withReadOnly':: d.fn(help='Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { portworxVolume+: { readOnly: readOnly } },
    '#withVolumeID':: d.fn(help='VolumeID uniquely identifies a Portworx volume', args=[d.arg(name='volumeID', type=d.T.string)]),
    withVolumeID(volumeID): { portworxVolume+: { volumeID: volumeID } }
  },
  '#projected':: d.obj(help='Represents a projected volume source'),
  projected: {
    '#withDefaultMode':: d.fn(help='Mode bits to use on created files by default. Must be a value between 0 and 0777. Directories within the path are not affected by this setting. This might be in conflict with other options that affect the file mode, like fsGroup, and the result can be other mode bits set.', args=[d.arg(name='defaultMode', type=d.T.integer)]),
    withDefaultMode(defaultMode): { projected+: { defaultMode: defaultMode } },
    '#withSources':: d.fn(help='list of volume projections', args=[d.arg(name='sources', type=d.T.array)]),
    withSources(sources): { projected+: { sources: if std.isArray(v=sources) then sources else [sources] } },
    '#withSourcesMixin':: d.fn(help='list of volume projections\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='sources', type=d.T.array)]),
    withSourcesMixin(sources): { projected+: { sources+: if std.isArray(v=sources) then sources else [sources] } }
  },
  '#quobyte':: d.obj(help='Represents a Quobyte mount that lasts the lifetime of a pod. Quobyte volumes do not support ownership management or SELinux relabeling.'),
  quobyte: {
    '#withGroup':: d.fn(help='Group to map volume access to Default is no group', args=[d.arg(name='group', type=d.T.string)]),
    withGroup(group): { quobyte+: { group: group } },
    '#withReadOnly':: d.fn(help='ReadOnly here will force the Quobyte volume to be mounted with read-only permissions. Defaults to false.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { quobyte+: { readOnly: readOnly } },
    '#withRegistry':: d.fn(help='Registry represents a single or multiple Quobyte Registry services specified as a string as host:port pair (multiple entries are separated with commas) which acts as the central registry for volumes', args=[d.arg(name='registry', type=d.T.string)]),
    withRegistry(registry): { quobyte+: { registry: registry } },
    '#withTenant':: d.fn(help='Tenant owning the given Quobyte volume in the Backend Used with dynamically provisioned Quobyte volumes, value is set by the plugin', args=[d.arg(name='tenant', type=d.T.string)]),
    withTenant(tenant): { quobyte+: { tenant: tenant } },
    '#withUser':: d.fn(help='User to map volume access to Defaults to serivceaccount user', args=[d.arg(name='user', type=d.T.string)]),
    withUser(user): { quobyte+: { user: user } },
    '#withVolume':: d.fn(help='Volume is a string that references an already created Quobyte volume by name.', args=[d.arg(name='volume', type=d.T.string)]),
    withVolume(volume): { quobyte+: { volume: volume } }
  },
  '#rbd':: d.obj(help='Represents a Rados Block Device mount that lasts the lifetime of a pod. RBD volumes support ownership management and SELinux relabeling.'),
  rbd: {
    '#secretRef':: d.obj(help='LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.'),
    secretRef: {
      '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
      withName(name): { rbd+: { secretRef+: { name: name } } }
    },
    '#withFsType':: d.fn(help='Filesystem type of the volume that you want to mount. Tip: Ensure that the filesystem type is supported by the host operating system. Examples: "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified. More info: https://kubernetes.io/docs/concepts/storage/volumes#rbd', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { rbd+: { fsType: fsType } },
    '#withImage':: d.fn(help='The rados image name. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it', args=[d.arg(name='image', type=d.T.string)]),
    withImage(image): { rbd+: { image: image } },
    '#withKeyring':: d.fn(help='Keyring is the path to key ring for RBDUser. Default is /etc/ceph/keyring. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it', args=[d.arg(name='keyring', type=d.T.string)]),
    withKeyring(keyring): { rbd+: { keyring: keyring } },
    '#withMonitors':: d.fn(help='A collection of Ceph monitors. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it', args=[d.arg(name='monitors', type=d.T.array)]),
    withMonitors(monitors): { rbd+: { monitors: if std.isArray(v=monitors) then monitors else [monitors] } },
    '#withMonitorsMixin':: d.fn(help='A collection of Ceph monitors. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='monitors', type=d.T.array)]),
    withMonitorsMixin(monitors): { rbd+: { monitors+: if std.isArray(v=monitors) then monitors else [monitors] } },
    '#withPool':: d.fn(help='The rados pool name. Default is rbd. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it', args=[d.arg(name='pool', type=d.T.string)]),
    withPool(pool): { rbd+: { pool: pool } },
    '#withReadOnly':: d.fn(help='ReadOnly here will force the ReadOnly setting in VolumeMounts. Defaults to false. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { rbd+: { readOnly: readOnly } },
    '#withUser':: d.fn(help='The rados user name. Default is admin. More info: https://releases.k8s.io/HEAD/examples/volumes/rbd/README.md#how-to-use-it', args=[d.arg(name='user', type=d.T.string)]),
    withUser(user): { rbd+: { user: user } }
  },
  '#scaleIO':: d.obj(help='ScaleIOVolumeSource represents a persistent ScaleIO volume'),
  scaleIO: {
    '#secretRef':: d.obj(help='LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.'),
    secretRef: {
      '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
      withName(name): { scaleIO+: { secretRef+: { name: name } } }
    },
    '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". Default is "xfs".', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { scaleIO+: { fsType: fsType } },
    '#withGateway':: d.fn(help='The host address of the ScaleIO API Gateway.', args=[d.arg(name='gateway', type=d.T.string)]),
    withGateway(gateway): { scaleIO+: { gateway: gateway } },
    '#withProtectionDomain':: d.fn(help='The name of the ScaleIO Protection Domain for the configured storage.', args=[d.arg(name='protectionDomain', type=d.T.string)]),
    withProtectionDomain(protectionDomain): { scaleIO+: { protectionDomain: protectionDomain } },
    '#withReadOnly':: d.fn(help='Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { scaleIO+: { readOnly: readOnly } },
    '#withSslEnabled':: d.fn(help='Flag to enable/disable SSL communication with Gateway, default false', args=[d.arg(name='sslEnabled', type=d.T.boolean)]),
    withSslEnabled(sslEnabled): { scaleIO+: { sslEnabled: sslEnabled } },
    '#withStorageMode':: d.fn(help='Indicates whether the storage for a volume should be ThickProvisioned or ThinProvisioned. Default is ThinProvisioned.', args=[d.arg(name='storageMode', type=d.T.string)]),
    withStorageMode(storageMode): { scaleIO+: { storageMode: storageMode } },
    '#withStoragePool':: d.fn(help='The ScaleIO Storage Pool associated with the protection domain.', args=[d.arg(name='storagePool', type=d.T.string)]),
    withStoragePool(storagePool): { scaleIO+: { storagePool: storagePool } },
    '#withSystem':: d.fn(help='The name of the storage system as configured in ScaleIO.', args=[d.arg(name='system', type=d.T.string)]),
    withSystem(system): { scaleIO+: { system: system } },
    '#withVolumeName':: d.fn(help='The name of a volume already created in the ScaleIO system that is associated with this volume source.', args=[d.arg(name='volumeName', type=d.T.string)]),
    withVolumeName(volumeName): { scaleIO+: { volumeName: volumeName } }
  },
  '#secret':: d.obj(help="Adapts a Secret into a volume.\n\nThe contents of the target Secret's Data field will be presented in a volume as files using the keys in the Data field as the file names. Secret volumes support ownership management and SELinux relabeling."),
  secret: {
    '#withDefaultMode':: d.fn(help='Optional: mode bits to use on created files by default. Must be a value between 0 and 0777. Defaults to 0644. Directories within the path are not affected by this setting. This might be in conflict with other options that affect the file mode, like fsGroup, and the result can be other mode bits set.', args=[d.arg(name='defaultMode', type=d.T.integer)]),
    withDefaultMode(defaultMode): { secret+: { defaultMode: defaultMode } },
    '#withItems':: d.fn(help="If unspecified, each key-value pair in the Data field of the referenced Secret will be projected into the volume as a file whose name is the key and content is the value. If specified, the listed keys will be projected into the specified paths, and unlisted keys will not be present. If a key is specified which is not present in the Secret, the volume setup will error unless it is marked optional. Paths must be relative and may not contain the '..' path or start with '..'.", args=[d.arg(name='items', type=d.T.array)]),
    withItems(items): { secret+: { items: if std.isArray(v=items) then items else [items] } },
    '#withItemsMixin':: d.fn(help="If unspecified, each key-value pair in the Data field of the referenced Secret will be projected into the volume as a file whose name is the key and content is the value. If specified, the listed keys will be projected into the specified paths, and unlisted keys will not be present. If a key is specified which is not present in the Secret, the volume setup will error unless it is marked optional. Paths must be relative and may not contain the '..' path or start with '..'.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='items', type=d.T.array)]),
    withItemsMixin(items): { secret+: { items+: if std.isArray(v=items) then items else [items] } },
    '#withOptional':: d.fn(help="Specify whether the Secret or it's keys must be defined", args=[d.arg(name='optional', type=d.T.boolean)]),
    withOptional(optional): { secret+: { optional: optional } },
    '#withSecretName':: d.fn(help="Name of the secret in the pod's namespace to use. More info: https://kubernetes.io/docs/concepts/storage/volumes#secret", args=[d.arg(name='secretName', type=d.T.string)]),
    withSecretName(secretName): { secret+: { secretName: secretName } }
  },
  '#storageos':: d.obj(help='Represents a StorageOS persistent volume resource.'),
  storageos: {
    '#secretRef':: d.obj(help='LocalObjectReference contains enough information to let you locate the referenced object inside the same namespace.'),
    secretRef: {
      '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
      withName(name): { storageos+: { secretRef+: { name: name } } }
    },
    '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified.', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { storageos+: { fsType: fsType } },
    '#withReadOnly':: d.fn(help='Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
    withReadOnly(readOnly): { storageos+: { readOnly: readOnly } },
    '#withVolumeName':: d.fn(help='VolumeName is the human-readable name of the StorageOS volume.  Volume names are only unique within a namespace.', args=[d.arg(name='volumeName', type=d.T.string)]),
    withVolumeName(volumeName): { storageos+: { volumeName: volumeName } },
    '#withVolumeNamespace':: d.fn(help="VolumeNamespace specifies the scope of the volume within StorageOS.  If no namespace is specified then the Pod's namespace will be used.  This allows the Kubernetes name scoping to be mirrored within StorageOS for tighter integration. Set VolumeName to any name to override the default behaviour. Set to 'default' if you are not using namespaces within StorageOS. Namespaces that do not pre-exist within StorageOS will be created.", args=[d.arg(name='volumeNamespace', type=d.T.string)]),
    withVolumeNamespace(volumeNamespace): { storageos+: { volumeNamespace: volumeNamespace } }
  },
  '#vsphereVolume':: d.obj(help='Represents a vSphere volume resource.'),
  vsphereVolume: {
    '#withFsType':: d.fn(help='Filesystem type to mount. Must be a filesystem type supported by the host operating system. Ex. "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified.', args=[d.arg(name='fsType', type=d.T.string)]),
    withFsType(fsType): { vsphereVolume+: { fsType: fsType } },
    '#withStoragePolicyID':: d.fn(help='Storage Policy Based Management (SPBM) profile ID associated with the StoragePolicyName.', args=[d.arg(name='storagePolicyID', type=d.T.string)]),
    withStoragePolicyID(storagePolicyID): { vsphereVolume+: { storagePolicyID: storagePolicyID } },
    '#withStoragePolicyName':: d.fn(help='Storage Policy Based Management (SPBM) profile name.', args=[d.arg(name='storagePolicyName', type=d.T.string)]),
    withStoragePolicyName(storagePolicyName): { vsphereVolume+: { storagePolicyName: storagePolicyName } },
    '#withVolumePath':: d.fn(help='Path that identifies vSphere volume vmdk', args=[d.arg(name='volumePath', type=d.T.string)]),
    withVolumePath(volumePath): { vsphereVolume+: { volumePath: volumePath } }
  },
  '#withName':: d.fn(help="Volume's name. Must be a DNS_LABEL and unique within the pod. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names", args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#mixin': 'ignore',
  mixin: self
}