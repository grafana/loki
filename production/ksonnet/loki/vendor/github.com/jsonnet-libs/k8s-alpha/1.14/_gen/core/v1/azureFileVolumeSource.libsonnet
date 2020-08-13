{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='azureFileVolumeSource', url='', help='AzureFile represents an Azure File Service mount on the host and bind mount to the pod.'),
  '#withReadOnly':: d.fn(help='Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#withSecretName':: d.fn(help='the name of secret that contains Azure Storage Account Name and Key', args=[d.arg(name='secretName', type=d.T.string)]),
  withSecretName(secretName): { secretName: secretName },
  '#withShareName':: d.fn(help='Share Name', args=[d.arg(name='shareName', type=d.T.string)]),
  withShareName(shareName): { shareName: shareName },
  '#mixin': 'ignore',
  mixin: self
}