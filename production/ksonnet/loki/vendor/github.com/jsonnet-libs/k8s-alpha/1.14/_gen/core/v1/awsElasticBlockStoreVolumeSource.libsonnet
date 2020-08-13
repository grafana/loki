{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='awsElasticBlockStoreVolumeSource', url='', help='Represents a Persistent Disk resource in AWS.\n\nAn AWS EBS disk must exist before mounting to a container. The disk must also be in the same AWS zone as the kubelet. An AWS EBS disk can only be mounted as read/write once. AWS EBS volumes support ownership management and SELinux relabeling.'),
  '#withFsType':: d.fn(help='Filesystem type of the volume that you want to mount. Tip: Ensure that the filesystem type is supported by the host operating system. Examples: "ext4", "xfs", "ntfs". Implicitly inferred to be "ext4" if unspecified. More info: https://kubernetes.io/docs/concepts/storage/volumes#awselasticblockstore', args=[d.arg(name='fsType', type=d.T.string)]),
  withFsType(fsType): { fsType: fsType },
  '#withPartition':: d.fn(help='The partition in the volume that you want to mount. If omitted, the default is to mount by volume name. Examples: For volume /dev/sda1, you specify the partition as "1". Similarly, the volume partition for /dev/sda is "0" (or you can leave the property empty).', args=[d.arg(name='partition', type=d.T.integer)]),
  withPartition(partition): { partition: partition },
  '#withReadOnly':: d.fn(help='Specify "true" to force and set the ReadOnly property in VolumeMounts to "true". If omitted, the default is "false". More info: https://kubernetes.io/docs/concepts/storage/volumes#awselasticblockstore', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#withVolumeID':: d.fn(help='Unique ID of the persistent disk resource in AWS (Amazon EBS volume). More info: https://kubernetes.io/docs/concepts/storage/volumes#awselasticblockstore', args=[d.arg(name='volumeID', type=d.T.string)]),
  withVolumeID(volumeID): { volumeID: volumeID },
  '#mixin': 'ignore',
  mixin: self
}