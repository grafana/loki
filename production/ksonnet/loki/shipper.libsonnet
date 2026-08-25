{
  _config+:: {
    stateful_queriers: if !self.use_index_gateway then true else super.stateful_queriers,
    index_period_hours: 24,

    loki+: {
      storage_config+: {
        tsdb_shipper+: {
          active_index_directory: '/data/tsdb-index',
          cache_location: '/data/tsdb-cache',
        },
      },
    },
  },
}
