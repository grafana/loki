local k = import 'ksonnet-util/kausal.libsonnet';

k {
  util+:: {
    // Convert number to k8s "quantity" (ie 1.5Gi -> "1536Mi")
    // as per https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/apimachinery/pkg/api/resource/quantity.go
    bytesToK8sQuantity(i)::
      local remove_factors_exponent(x, y) =
        if x % y > 0
        then 0
        else remove_factors_exponent(x / y, y) + 1;
      local remove_factors_remainder(x, y) =
        if x % y > 0
        then x
        else remove_factors_remainder(x / y, y);
      local suffixes = ['', 'Ki', 'Mi', 'Gi'];
      local suffix = suffixes[remove_factors_exponent(i, 1024)];
      '%d%s' % [remove_factors_remainder(i, 1024), suffix],
  },

  memcached:: {
    name:: error 'must specify name',
    max_item_size:: '1m',
    memory_limit_mb:: 1024,
    overprovision_factor:: 1.2,
    cpu_requests:: '500m',
    cpu_limits:: '3',
    connection_limit:: 1024,
    memory_request_overhead_mb:: 100,
    memory_request_bytes::
      std.ceil((self.memory_limit_mb * self.overprovision_factor) + self.memory_request_overhead_mb) * 1024 * 1024,
    memory_limits_bytes::
      std.max(self.memory_limit_mb * 1.5 * 1024 * 1024, self.memory_request_bytes),
    exporter_cpu_requests:: null,
    exporter_cpu_limits:: null,
    exporter_memory_requests:: null,
    exporter_memory_limits:: null,
    use_topology_spread:: false,
    topology_spread_max_skew:: 1,
    min_ready_seconds:: null,
    extended_options:: [],

    local container = $.core.v1.container,
    local containerPort = $.core.v1.containerPort,

    memcached_container::
      container.new('memcached', $._images.memcached) +
      container.withPorts([containerPort.new('client', 11211)]) +
      container.withArgs(
        [
          '-m %(memory_limit_mb)s' % self,
          '-I %(max_item_size)s' % self,
          '-c %(connection_limit)s' % self,
          '-v',
        ] +
        if std.length(self.extended_options) != 0 then ['--extended=' + std.join(',', self.extended_options)] else []
      ) +
      $.util.resourcesRequests(self.cpu_requests, $.util.bytesToK8sQuantity(self.memory_request_bytes)) +
      $.util.resourcesLimits(self.cpu_limits, $.util.bytesToK8sQuantity(self.memory_limits_bytes)),

    memcached_exporter::
      container.new('exporter', $._images.memcachedExporter) +
      container.withPorts([containerPort.new('http-metrics', 9150)]) +
      container.withArgs([
        '--memcached.address=localhost:11211',
        '--web.listen-address=0.0.0.0:9150',
      ]) +
      // Only add requests or limits if they've been set to a non-null value
      (if self.exporter_cpu_requests == null && self.exporter_memory_requests == null then {} else $.util.resourcesRequests(self.exporter_cpu_requests, self.exporter_memory_requests)) +
      (if self.exporter_cpu_limits == null && self.exporter_memory_limits == null then {} else $.util.resourcesLimits(self.exporter_cpu_limits, self.exporter_memory_limits)),

    local statefulSet = $.apps.v1.statefulSet,
    local topologySpreadConstraints = k.core.v1.topologySpreadConstraint,

    statefulSet:
      statefulSet.new(self.name, $._config.memcached_replicas, [
        self.memcached_container,
        self.memcached_exporter,
      ], []) +
      statefulSet.spec.withServiceName(self.name) +
      (if self.min_ready_seconds == null then {} else statefulSet.mixin.spec.withMinReadySeconds(self.min_ready_seconds)) +
      if self.use_topology_spread then
        local pod_name = self.name;
        statefulSet.spec.template.spec.withTopologySpreadConstraints(
          // Evenly spread pods among available nodes.
          topologySpreadConstraints.labelSelector.withMatchLabels({ name: pod_name }) +
          topologySpreadConstraints.withTopologyKey('kubernetes.io/hostname') +
          topologySpreadConstraints.withWhenUnsatisfiable('ScheduleAnyway') +
          topologySpreadConstraints.withMaxSkew(self.topology_spread_max_skew),
        )
      else
        $.util.antiAffinity,

    local service = $.core.v1.service,

    service:
      $.util.serviceFor(self.statefulSet) +
      service.spec.withClusterIp('None'),
  },
}
