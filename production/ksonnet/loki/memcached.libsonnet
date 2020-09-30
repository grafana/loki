local memcached = import 'memcached/memcached.libsonnet';

memcached {
  // Memcached instance used to cache chunks.
  memcached_chunks: $.memcached {
    name: 'memcached',
    max_item_size: $._config.memcached_chunks_max_item_size,
    memory_limit_mb: $._config.memcached_chunks_memory_limit_mb,
  },

  // Dedicated memcached instance used to temporarily cache index lookups.
  memcached_index_queries: $.memcached {
    name: 'memcached-index-queries',
    max_item_size: $._config.memcached_index_queries_max_item_size,
    memory_limit_mb: $._config.memcached_index_queries_memory_limit_mb,
  },

  // Dedicated memcached instance used to dedupe writes to the index.
  memcached_index_writes: $.memcached {
    name: 'memcached-index-writes',
    max_item_size: $._config.memcached_index_writes_max_item_size,
    memory_limit_mb: $._config.memcached_index_writes_memory_limit_mb,
  },

  // Dedicated memcached instance used to cache query results.
  memcached_frontend: $.memcached {
    name: 'memcached-frontend',
    max_item_size: $._config.memcached_frontend_max_item_size,
    memory_limit_mb: $._config.memcached_frontend_memory_limit_mb,
  },
}
