local consul = import "consul/consul.libsonnet";

consul {
  consul_container+::
    $.util.resourcesRequests('100m', '500Mi') +
    $.util.resourcesLimits(null, null),
}