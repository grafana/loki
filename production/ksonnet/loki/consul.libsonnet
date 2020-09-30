local consul = import "consul/consul.libsonnet";

consul {
  consul_container+::
    $.util.resourcesRequests(
      $._config.consul_resources_requests_cpu,
      $._config.consul_resources_requests_memory) +
    $.util.resourcesLimits(
      $._config.consul_resources_limits_cpu,
      $._config.consul_resources_limits_memory),
}