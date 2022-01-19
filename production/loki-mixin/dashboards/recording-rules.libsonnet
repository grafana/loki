local raw = (import './dashboard-recording-rules.json');
local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+:
    {
      'loki-mixin-recording-rules.json': raw,
    },
}
