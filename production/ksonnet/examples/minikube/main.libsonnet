local loki = import 'loki/all_in_one.libsonnet';
local gateway = import 'loki/gateway.libsonnet';
local promtail = import 'promtail/promtail.libsonnet';

loki + promtail {
  _config+:: {
    namespace: 'default',

    promtail_config: {
      scheme: 'http',
      hostname: 'loki.%(namespace)s.svc' % $._config,
    },
    replication_factor: 1,
  },
}
