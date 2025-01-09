local base = import '../../../../lib/panels/time-series/base.libsonnet';

// local variables
local defaultParams = {
  tooltip: {
    mode: 'multi',
    sort: 'desc'
  },
  calcs: ['min', 'max', 'mean', 'sum'],
  displayMode: 'table',
  interval: '1h',
  unit: 'bytes',
  showPoints: 'never',
};

base
+
{
  bytesIn:: base.new({
    tooltip: {
      mode: 'multi',
      sort: 'desc'
    },
    calcs: ['min', 'max', 'mean', 'sum'],
    displayMode: 'table',
    interval: '1h',
    unit: 'bytes',
    showPoints: 'never',
  })
}
