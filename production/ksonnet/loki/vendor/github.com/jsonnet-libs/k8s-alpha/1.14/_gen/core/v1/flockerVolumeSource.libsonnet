{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='flockerVolumeSource', url='', help='Represents a Flocker volume mounted by the Flocker agent. One and only one of datasetName and datasetUUID should be set. Flocker volumes do not support ownership management or SELinux relabeling.'),
  '#withDatasetName':: d.fn(help='Name of the dataset stored as metadata -> name on the dataset for Flocker should be considered as deprecated', args=[d.arg(name='datasetName', type=d.T.string)]),
  withDatasetName(datasetName): { datasetName: datasetName },
  '#withDatasetUUID':: d.fn(help='UUID of the dataset. This is unique identifier of a Flocker dataset', args=[d.arg(name='datasetUUID', type=d.T.string)]),
  withDatasetUUID(datasetUUID): { datasetUUID: datasetUUID },
  '#mixin': 'ignore',
  mixin: self
}