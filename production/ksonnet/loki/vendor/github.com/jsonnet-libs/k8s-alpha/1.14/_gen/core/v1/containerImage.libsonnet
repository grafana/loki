{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='containerImage', url='', help='Describe a container image'),
  '#withNames':: d.fn(help='Names by which this image is known. e.g. ["k8s.gcr.io/hyperkube:v1.0.7", "dockerhub.io/google_containers/hyperkube:v1.0.7"]', args=[d.arg(name='names', type=d.T.array)]),
  withNames(names): { names: if std.isArray(v=names) then names else [names] },
  '#withNamesMixin':: d.fn(help='Names by which this image is known. e.g. ["k8s.gcr.io/hyperkube:v1.0.7", "dockerhub.io/google_containers/hyperkube:v1.0.7"]\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='names', type=d.T.array)]),
  withNamesMixin(names): { names+: if std.isArray(v=names) then names else [names] },
  '#withSizeBytes':: d.fn(help='The size of the image in bytes.', args=[d.arg(name='sizeBytes', type=d.T.integer)]),
  withSizeBytes(sizeBytes): { sizeBytes: sizeBytes },
  '#mixin': 'ignore',
  mixin: self
}