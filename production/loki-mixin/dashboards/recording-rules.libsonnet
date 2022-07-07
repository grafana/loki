local raw = (import './dashboard-recording-rules.json');
local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+:
    {
      local uid = if $._config.ssd.enabled then 'recording-rules-ssd' else 'recording-rules',
      'loki-mixin-recording-rules.json': raw + $.dashboard('Recording Rules', uid=uid),
    },
}
