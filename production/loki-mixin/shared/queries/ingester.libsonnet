// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local common = import './common.libsonnet';
local selector = (import '../selectors.libsonnet').new;

{
  // Number of unique log streams
  memory_streams(by=config.labels.pod)::
    local ingesterSelector =
      selector()
        .ingester()
        .build();
    |||
      sum by (%s) (
        loki_ingester_memory_streams{%s}
      )
    ||| % [
      by,
      ingesterSelector,
    ],
}
