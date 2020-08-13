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
    cpu_limits:: '3',
    connection_limit:: 1024,
    memory_request_bytes::
      std.ceil((self.memory_limit_mb * self.overprovision_factor) + 100) * 1024 * 1024,
    memory_limits_bytes::
      self.memory_limit_mb * 1.5 * 1024 * 1024,

    local container = $.core.v1.container,
    local containerPort = $.core.v1.containerPort,

    memcached_container::
      container.new('memcached', $._images.memcached) +
      container.withPorts([containerPort.new('client', 11211)]) +
      container.withArgs([
        '-m %(memory_limit_mb)s' % self,
        '-I %(max_item_size)s' % self,
        '-c %(connection_limit)s' % self,
        '-v',
      ]) +
      $.util.resourcesRequests('500m', $.util.bytesToK8sQuantity(self.memory_request_bytes)) +
      $.util.resourcesLimits(self.cpu_limits, $.util.bytesToK8sQuantity(self.memory_limits_bytes)),

    memcached_exporter::
      container.new('exporter', $._images.memcachedExporter) +
      container.withPorts([containerPort.new('http-metrics', 9150)]) +
      container.withArgs([
        '--memcached.address=localhost:11211',
        '--web.listen-address=0.0.0.0:9150',
      ]),

    local statefulSet = $.apps.v1.statefulSet,

    statefulSet:
      statefulSet.new(self.name, $._config.memcached_replicas, [
        self.memcached_container,
        self.memcached_exporter,
      ], []) +
      statefulSet.spec.withServiceName(self.name) +
      $.util.antiAffinity,

    local service = $.core.v1.service,

    service:
      $.util.serviceFor(self.statefulSet) +
      service.spec.withClusterIp('None'),
  },
}
