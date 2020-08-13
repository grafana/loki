{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='azureFilePersistentVolumeSource', url='', help='AzureFile represents an Azure File Service mount on the host and bind mount to the pod.'),
  '#withReadOnly':: d.fn(help='Defaults to false (read/write). ReadOnly here will force the ReadOnly setting in VolumeMounts.', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#withSecretName':: d.fn(help='the name of secret that contains Azure Storage Account Name and Key', args=[d.arg(name='secretName', type=d.T.string)]),
  withSecretName(secretName): { secretName: secretName },
  '#withSecretNamespace':: d.fn(help='the namespace of the secret that contains Azure Storage Account Name and Key default is the same as the Pod', args=[d.arg(name='secretNamespace', type=d.T.string)]),
  withSecretNamespace(secretNamespace): { secretNamespace: secretNamespace },
  '#withShareName':: d.fn(help='Share Name', args=[d.arg(name='shareName', type=d.T.string)]),
  withShareName(shareName): { shareName: shareName },
  '#mixin': 'ignore',
  mixin: self
}