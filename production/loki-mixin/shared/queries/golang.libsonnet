// imports
local lib = import '../../lib/_imports.libsonnet';
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;

{
  // wrapper for the resource selector
  _resourceSelector(component)::
    selector()
      .label(config.labels.container).neq('')
      .resource(component)
      .build(),

  // wrapper for go_memstats_*_bytes
  _bytes(component, metric, by=config.labels.pod)::
    |||
      sum by(%s) (
        go_memstats_%s_bytes{%s}
      )
    ||| % [by, metric, self._resourceSelector(component)],

  // wrapper for go_memstats_*_total
  _rate(component, metric, by=config.labels.pod)::
    |||
      sum by(%s) (
        rate(go_memstats_%s_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, metric, self._resourceSelector(component)],

  frees_rate(component, by=config.labels.pod)::
    // metric: go_memstats_frees_total
    self._rate(component, 'frees', by),

  // gets the total number of bytes currently allocated for a given component
  alloc_bytes(component, by=config.labels.pod)::
    // metric: go_memstats_alloc_bytes
    self._bytes(component, 'alloc', by),

  alloc_bytes_rate(component, by=config.labels.pod)::
    // metric: go_memstats_alloc_bytes_total
    self._rate(component, 'alloc_bytes', by),

  // gets the heap memory bytes for a given component
  heap_bytes(component, type, by=config.labels.pod)::
    // metric: go_memstats_heap_*_bytes
    self._bytes(component, 'heap_%s' % type, by),

  // gets the heap memory allocation for a given component
  heap_alloc(component, by=config.labels.pod)::
    // metric: go_memstats_heap_alloc_bytes
    self.heap_bytes(component, 'alloc', by),

  // gets the heap memory idle for a given component
  heap_idle(component, by=config.labels.pod)::
    // metric: go_memstats_heap_idle_bytes
    self.heap_bytes(component, 'idle', by),

  // gets the heap memory inuse for a given component
  heap_inuse(component, by=config.labels.pod)::
    // metric: go_memstats_heap_inuse_bytes
    self.heap_bytes(component, 'inuse', by),

  // gets the heap memory released for a given component
  heap_released(component, by=config.labels.pod)::
    // metric: go_memstats_heap_released_bytes
    self.heap_bytes(component, 'released', by),

  // gets the heap memory sys for a given component
  heap_sys(component, by=config.labels.pod)::
    // metric: go_memstats_heap_sys_bytes
    self.heap_bytes(component, 'sys', by),

  // gets the total number of objects in the heap for a given component
  heap_objects(component, by=config.labels.pod)::
    |||
      sum by(%s) (
        go_memstats_heap_objects{%s}
      )
    ||| % [by, self._resourceSelector(component)],

  // gets the bucket hash sys bytes for a given component
  bucket_hash_sys_bytes(component, by=config.labels.pod)::
    // metric: go_memstats_buck_hash_sys_bytes
    self._bytes(component, 'buck_hash', by),

  // gets the gc sys bytes for a given component
  gc_sys_bytes(component, by=config.labels.pod)::
    // metric: go_memstats_gc_sys_bytes
    self._bytes(component, 'gc', by),

  // gets the mcache inuse bytes for a given component
  mcache_inuse_bytes(component, by=config.labels.pod)::
    // metric: go_memstats_mcache_inuse_bytes
    self._bytes(component, 'mcache', by),

  mallocs_rate(component, by=config.labels.pod)::
    // metric: go_memstats_mallocs_total
    self._rate(component, 'mallocs', by),

  // gets the last gc time for a given component
  last_gc_time(component, by=config.labels.pod)::
    |||
      max by(%s) (
        go_memstats_last_gc_time_seconds{%s}
      )
    ||| % [by, self._resourceSelector(component)],

  // gets the go memory limit bytes for a given component
  gc_go_mem_limit_bytes(component, by=config.labels.pod)::
    |||
      sum by(%s) (
        go_gc_gomemlimit_bytes{%s}
      )
    ||| % [by, self._resourceSelector(component)],

  // gets the number of goroutines for a given component
  go_routines(component, by=config.labels.pod)::
    |||
      sum by(%s) (
        go_goroutines{%s}
      )
    ||| % [by, self._resourceSelector(component)],

  // gets the go info for a given component
  go_info(component, by=config.labels.pod)::
    |||
      max by(%s) (
        go_info{%s}
      )
    ||| % [by, self._resourceSelector(component)],


  // Gets the histogram data for request latencies of a given component
  gc_histogram(component, by=config.labels.pod)::
    |||
      sum by (le) (
        rate(go_gc_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component)],

  // Calculates a specific latency percentile for requests to a component
  // valid quantiles are: 0.0, 0.25, 0.5, 0.75, 1.0
  gc_duration_quantile(component, quantile=0.5, by=config.labels.pod)::
    local resourceSelector =
      selector()
        .label(config.labels.container).neq('')
        .resource(component)
        .label('quantile').eq(quantile)
        .build();
    |||
      go_gc_duration_seconds{%s}
    ||| % [resourceSelector],

  // Calculates the 25th percentile of request latencies
  gc_duration_quantile_0(component, by=config.labels.pod)::
    self.gc_duration_quantile(component, 0.0, by),

  // Calculates the 25th percentile of request latencies
  gc_duration_quantile_25(component, by=config.labels.pod)::
    self.gc_duration_quantile(component, 0.25, by),

  // Calculates the 50th percentile of request latencies
  gc_duration_quantile_50(component, by=config.labels.pod)::
    self.gc_duration_quantile(component, 0.5, by),

  // Calculates the 75th percentile of request latencies
  gc_duration_quantile_75(component, by=config.labels.pod)::
    self.gc_duration_quantile(component, 0.75, by),

  // Calculates the 50th percentile (median) of request latencies
  gc_duration_quantile_100(component, by=config.labels.pod)::
    self.gc_duration_quantile(component, 1.0, by),

  // Calculates the average gc duration for a component
  gc_duration_average(component, by=config.labels.pod)::
    |||
      go_gc_duration_seconds_sum{%s}
      /
      go_gc_duration_seconds_count{%s}
    ||| % std.repeat(self._resourceSelector(component), 2),
}
