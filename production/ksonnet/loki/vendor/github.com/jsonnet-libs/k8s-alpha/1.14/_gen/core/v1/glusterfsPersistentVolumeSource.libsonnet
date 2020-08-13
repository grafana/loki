{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='glusterfsPersistentVolumeSource', url='', help='Represents a Glusterfs mount that lasts the lifetime of a pod. Glusterfs volumes do not support ownership management or SELinux relabeling.'),
  '#withEndpoints':: d.fn(help='EndpointsName is the endpoint name that details Glusterfs topology. More info: https://releases.k8s.io/HEAD/examples/volumes/glusterfs/README.md#create-a-pod', args=[d.arg(name='endpoints', type=d.T.string)]),
  withEndpoints(endpoints): { endpoints: endpoints },
  '#withEndpointsNamespace':: d.fn(help='EndpointsNamespace is the namespace that contains Glusterfs endpoint. If this field is empty, the EndpointNamespace defaults to the same namespace as the bound PVC. More info: https://releases.k8s.io/HEAD/examples/volumes/glusterfs/README.md#create-a-pod', args=[d.arg(name='endpointsNamespace', type=d.T.string)]),
  withEndpointsNamespace(endpointsNamespace): { endpointsNamespace: endpointsNamespace },
  '#withPath':: d.fn(help='Path is the Glusterfs volume path. More info: https://releases.k8s.io/HEAD/examples/volumes/glusterfs/README.md#create-a-pod', args=[d.arg(name='path', type=d.T.string)]),
  withPath(path): { path: path },
  '#withReadOnly':: d.fn(help='ReadOnly here will force the Glusterfs volume to be mounted with read-only permissions. Defaults to false. More info: https://releases.k8s.io/HEAD/examples/volumes/glusterfs/README.md#create-a-pod', args=[d.arg(name='readOnly', type=d.T.boolean)]),
  withReadOnly(readOnly): { readOnly: readOnly },
  '#mixin': 'ignore',
  mixin: self
}