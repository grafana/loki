local k = import 'ksonnet-util/kausal.libsonnet';

{
  local max_outstanding = if $._config.queryFrontend.sharded_queries_enabled then 1024 else 256,

  // Override frontend and querier configuration
  _config+:: {
    loki+: if $._config.query_scheduler_enabled then {
      frontend+: {
        scheduler_address: 'query-scheduler-discovery.%s.svc.cluster.local.:9095' % $._config.namespace,
      },
      frontend_worker+: {
        scheduler_address: 'query-scheduler-discovery.%s.svc.cluster.local.:9095' % $._config.namespace,
      },
      query_scheduler+: {
        max_outstanding_requests_per_tenant: max_outstanding,
      },
    } else {
      frontend+: {
        max_outstanding_per_tenant: max_outstanding,
      },
      frontend_worker+: {
        frontend_address: 'query-frontend-headless.%s.svc.cluster.local.:9095' % $._config.namespace,
      },
    },
  },

  query_scheduler_args:: if $._config.query_scheduler_enabled then
    $._config.commonArgs {
      target: 'query-scheduler',
      'log.level': 'debug',
    }
  else {},

  query_scheduler_ports:: $.util.grpclbDefaultPorts,

  local container = k.core.v1.container,
  query_scheduler_container:: if $._config.query_scheduler_enabled then
    container.new('query-scheduler', $._images.query_scheduler) +
    container.withPorts($.query_scheduler_ports) +
    container.withArgsMixin(k.util.mapToFlags($.query_scheduler_args)) +
    $.jaeger_mixin +
    k.util.resourcesRequests('2', '600Mi') +
    k.util.resourcesLimits(null, '1200Mi') +
    container.withEnvMixin($._config.commonEnvs)
  else {},

  local deployment = k.apps.v1.deployment,
  query_scheduler_deployment: if $._config.query_scheduler_enabled then
    deployment.new('query-scheduler', 2, [$.query_scheduler_container]) +
    $.config_hash_mixin +
    k.util.configVolumeMount('loki', '/etc/loki/config') +
    k.util.configVolumeMount(
      $._config.overrides_configmap_mount_name,
      $._config.overrides_configmap_mount_path,
    ) +
    k.util.antiAffinity +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxSurge(5) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxUnavailable(1)
  else {},

  local service = k.core.v1.service,

  // When this module was created the headless service was named
  // 'query-scheduler-discovery' which diverges from the naming convention in
  // query-frontend, this hidden attribute provides backward compatibility while
  // allowing users to solve themselves this inconsistency
  query_scheduler_headless_service_name:: 'query-scheduler-discovery',

  // A headless service for discovering IPs of each query-scheduler pod.
  // It leaves it up to the client to do any load-balancing of requests,
  // so if the intention is to use the k8s service for load balancing,
  // it is advised to use the below `query-scheduler` service instead.
  query_scheduler_headless_service: if !$._config.query_scheduler_enabled then {} else
    // headlessService will make ensure two things:
    //   1. Set clusterIP to "None": this makes it so that query scheduler worker,
    //      running in the querier, resolve each query-scheduler pod IP instead
    //      of the service IP. clusterIP set to "None" allow this by making it
    //      so that when the service DNS is resolved it returns the set of
    //      query-scheduler IPs.
    //   2. Set withPublishNotReadyAddresses to true: query scheduler will not
    //      become ready until at least one querier connects which creates a
    //      chicken and egg scenario if we don't publish the query-scheduler
    //      address before it's ready.
    $.util.headlessService($.query_scheduler_deployment, $.query_scheduler_headless_service_name),

  query_scheduler_service: if !$._config.query_scheduler_enabled then {} else
    $.util.grpclbServiceFor($.query_scheduler_deployment),
}
