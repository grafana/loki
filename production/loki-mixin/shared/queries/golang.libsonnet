// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    local sel = selector();
    if std.isArray(component) then
      sel.resources(component)
    else
      sel.resource(component),

  // Counter rates
  // Gets the rate of memory frees
  frees_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(go_memstats_frees_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of memory allocations
  alloc_bytes_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(go_memstats_alloc_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of mallocs
  mallocs_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(go_memstats_mallocs_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gauge metrics
  // Gets the current bytes allocated
  alloc_bytes(component, by=config.labels.resource_selector)::
    |||
      go_memstats_alloc_bytes{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the heap bytes allocated
  heap_alloc(component, by=config.labels.resource_selector)::
    |||
      go_memstats_heap_alloc_bytes{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the heap bytes idle
  heap_idle(component, by=config.labels.resource_selector)::
    |||
      go_memstats_heap_idle_bytes{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the heap bytes in use
  heap_inuse(component, by=config.labels.resource_selector)::
    |||
      go_memstats_heap_inuse_bytes{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the heap bytes released
  heap_released(component, by=config.labels.resource_selector)::
    |||
      go_memstats_heap_released_bytes{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the heap bytes from system
  heap_sys(component, by=config.labels.resource_selector)::
    |||
      go_memstats_heap_sys_bytes{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the total objects in heap
  heap_objects(component, by=config.labels.resource_selector)::
    |||
      go_memstats_heap_objects{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the bucket hash system bytes
  bucket_hash_sys_bytes(component, by=config.labels.resource_selector)::
    |||
      go_memstats_buck_hash_sys_bytes{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the GC system bytes
  gc_sys_bytes(component, by=config.labels.resource_selector)::
    |||
      go_memstats_gc_sys_bytes{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the mcache in use bytes
  mcache_inuse_bytes(component, by=config.labels.resource_selector)::
    |||
      go_memstats_mcache_inuse_bytes{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the last GC time
  last_gc_time(component, by=config.labels.resource_selector)::
    |||
      go_memstats_last_gc_time_seconds{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the memory limit bytes
  memory_limit_bytes(component, by=config.labels.resource_selector)::
    |||
      go_gc_gomemlimit_bytes{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the number of goroutines
  goroutines(component, by=config.labels.resource_selector)::
    |||
      go_goroutines{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the Go version info
  info(component, by=config.labels.resource_selector)::
    |||
      go_info{%s}
    ||| % [self._resourceSelector(component).build()],

  // Histogram metrics
  // Gets the GC duration percentile
  gc_duration_percentile(component, percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(go_gc_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of GC duration
  gc_duration_p99(component, by=config.labels.resource_selector)::
    self.gc_duration_percentile(component, 0.99, by),

  // Gets the 95th percentile of GC duration
  gc_duration_p95(component, by=config.labels.resource_selector)::
    self.gc_duration_percentile(component, 0.95, by),

  // Gets the 90th percentile of GC duration
  gc_duration_p90(component, by=config.labels.resource_selector)::
    self.gc_duration_percentile(component, 0.90, by),

  // Gets the 50th percentile of GC duration
  gc_duration_p50(component, by=config.labels.resource_selector)::
    self.gc_duration_percentile(component, 0.50, by),

  // Gets the average GC duration
  gc_duration_average(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(go_gc_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(go_gc_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Calculated metrics
  // Gets the time since last GC
  time_since_last_gc(component, by=config.labels.resource_selector)::
    |||
      time() - go_memstats_last_gc_time_seconds{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the heap allocation rate
  heap_allocation_rate(component, by=config.labels.resource_selector)::
    |||
      rate(go_memstats_heap_alloc_bytes{%s}[$__rate_interval])
    ||| % [self._resourceSelector(component).build()],
}
