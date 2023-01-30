local k = import 'ksonnet-util/kausal.libsonnet';


//This is used to set up a headless DNS service for all instances
//in the deployment, so there will be a DNS record to use for
//memberlist
{
  local service = k.core.v1.service,

  headless_service:
    service.new(
      $._config.headless_service_name,
      {
        app: $._config.headless_service_name,
      },
      []
    ) +
    service.mixin.spec.withClusterIP('None') +
    service.mixin.spec.withPublishNotReadyAddresses(true),
}
