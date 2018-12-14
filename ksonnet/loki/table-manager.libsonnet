{
  local container = $.core.v1.container,

  table_manager_args:: {
    'bigtable.project': $._config.bigtable_project,
    'bigtable.instance': $._config.bigtable_instance,
    'chunk.storage-client': $._config.storage_backend,

    'dynamodb.original-table-name': '%s_index' % $._config.table_prefix,
    'dynamodb.use-periodic-tables': true,
    'dynamodb.periodic-table.prefix': '%s_index_' % $._config.table_prefix,
    'dynamodb.chunk-table.prefix': '%s_chunks_' % $._config.table_prefix,
    'dynamodb.periodic-table.from': $._config.schema_start_date,
    'dynamodb.chunk-table.from': $._config.schema_start_date,
    'dynamodb.v9-schema-from': $._config.schema_start_date,
    // Cassandra / BigTable doesn't use these fields, so set them to zero
    'dynamodb.chunk-table.inactive-read-throughput': 0,
    'dynamodb.chunk-table.inactive-write-throughput': 0,
    'dynamodb.chunk-table.read-throughput': 0,
    'dynamodb.chunk-table.write-throughput': 0,
    'dynamodb.periodic-table.inactive-read-throughput': 0,
    'dynamodb.periodic-table.inactive-write-throughput': 0,
    'dynamodb.periodic-table.read-throughput': 0,
    'dynamodb.periodic-table.write-throughput': 0,
  },

  table_manager_container::
    container.new('table-manager', $._images.tableManager) +
    container.withPorts($.util.defaultPorts) +
    container.withArgsMixin($.util.mapToFlags($.table_manager_args)) +
    $.util.resourcesRequests('100m', '100Mi') +
    $.util.resourcesLimits('200m', '200Mi'),

  local deployment = $.apps.v1beta1.deployment,

  table_manager_deployment:
    deployment.new('table-manager', 1, [$.table_manager_container]),

  table_manager_service:
    $.util.serviceFor($.table_manager_deployment),
}
