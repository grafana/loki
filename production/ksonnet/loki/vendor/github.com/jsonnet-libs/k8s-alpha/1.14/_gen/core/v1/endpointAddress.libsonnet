{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='endpointAddress', url='', help='EndpointAddress is a tuple that describes single IP address.'),
  '#targetRef':: d.obj(help='ObjectReference contains enough information to let you inspect or modify the referred object.'),
  targetRef: {
    '#withFieldPath':: d.fn(help='If referring to a piece of an object instead of an entire object, this string should contain a valid JSON/Go field access statement, such as desiredState.manifest.containers[2]. For example, if the object reference is to a container within a pod, this would take on a value like: "spec.containers{name}" (where "name" refers to the name of the container that triggered the event) or if no container name is specified "spec.containers[2]" (container with index 2 in this pod). This syntax is chosen only to have some well-defined way of referencing a part of an object.', args=[d.arg(name='fieldPath', type=d.T.string)]),
    withFieldPath(fieldPath): { targetRef+: { fieldPath: fieldPath } },
    '#withKind':: d.fn(help='Kind of the referent. More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#types-kinds', args=[d.arg(name='kind', type=d.T.string)]),
    withKind(kind): { targetRef+: { kind: kind } },
    '#withName':: d.fn(help='Name of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { targetRef+: { name: name } },
    '#withNamespace':: d.fn(help='Namespace of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/', args=[d.arg(name='namespace', type=d.T.string)]),
    withNamespace(namespace): { targetRef+: { namespace: namespace } },
    '#withResourceVersion':: d.fn(help='Specific resourceVersion to which this reference is made, if any. More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#concurrency-control-and-consistency', args=[d.arg(name='resourceVersion', type=d.T.string)]),
    withResourceVersion(resourceVersion): { targetRef+: { resourceVersion: resourceVersion } },
    '#withUid':: d.fn(help='UID of the referent. More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#uids', args=[d.arg(name='uid', type=d.T.string)]),
    withUid(uid): { targetRef+: { uid: uid } }
  },
  '#withHostname':: d.fn(help='The Hostname of this endpoint', args=[d.arg(name='hostname', type=d.T.string)]),
  withHostname(hostname): { hostname: hostname },
  '#withIp':: d.fn(help='The IP of this endpoint. May not be loopback (127.0.0.0/8), link-local (169.254.0.0/16), or link-local multicast ((224.0.0.0/24). IPv6 is also accepted but not fully supported on all platforms. Also, certain kubernetes components, like kube-proxy, are not IPv6 ready.', args=[d.arg(name='ip', type=d.T.string)]),
  withIp(ip): { ip: ip },
  '#withNodeName':: d.fn(help='Optional: Node hosting this endpoint. This can be used to determine endpoints local to a node.', args=[d.arg(name='nodeName', type=d.T.string)]),
  withNodeName(nodeName): { nodeName: nodeName },
  '#mixin': 'ignore',
  mixin: self
}