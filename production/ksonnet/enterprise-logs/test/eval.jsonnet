(import '../main.libsonnet') {
  _config+:: {
    shared_store: self.storage_backend,
    gcs_bucket_name: 'test-gcs-bucket-name',
    storage_backend: 'gcs',

    namespace: 'test-namespace',
    commonArgs+: {
      'cluster-name': 'test-cluster-name',
    },
  },
}
