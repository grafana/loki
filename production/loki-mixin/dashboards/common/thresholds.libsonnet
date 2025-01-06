// imports
local g = import '../lib/grafana.libsonnet';

// local variables
local panel = g.panel;

{
  usage(type, color = 'blue')::
    if color == 'palette-classic' then
      panel[type].standardOptions.thresholds.withMode('percentage')
        + panel[type].standardOptions.thresholds.withSteps([
          panel[type].standardOptions.threshold.step.withValue(null) + panel[type].standardOptions.threshold.step.withColor('purple'),
          panel[type].standardOptions.threshold.step.withValue(20) + panel[type].standardOptions.threshold.step.withColor('blue'),
          panel[type].standardOptions.threshold.step.withValue(40) + panel[type].standardOptions.threshold.step.withColor('green'),
          panel[type].standardOptions.threshold.step.withValue(60) + panel[type].standardOptions.threshold.step.withColor('yellow'),
          panel[type].standardOptions.threshold.step.withValue(80) + panel[type].standardOptions.threshold.step.withColor('orange'),
          panel[type].standardOptions.threshold.step.withValue(99) + panel[type].standardOptions.threshold.step.withColor('red'),
        ])
    else
      panel[type].standardOptions.thresholds.withMode('percentage')
        + panel[type].standardOptions.thresholds.withSteps([
          panel[type].standardOptions.threshold.step.withValue(null) + panel[type].standardOptions.threshold.step.withColor('super-light-%s' % color),
          panel[type].standardOptions.threshold.step.withValue(1) + panel[type].standardOptions.threshold.step.withColor('light-%s' % color),
          panel[type].standardOptions.threshold.step.withValue(10) + panel[type].standardOptions.threshold.step.withColor('%s' % color),
          panel[type].standardOptions.threshold.step.withValue(50) + panel[type].standardOptions.threshold.step.withColor('semi-dark-%s' % color),
          panel[type].standardOptions.threshold.step.withValue(100) + panel[type].standardOptions.threshold.step.withColor('dark-%s' % color),
        ]),

  utilization(type)::
    panel[type].standardOptions.thresholds.withMode('percentage')
      + panel[type].standardOptions.thresholds.withSteps([
        panel[type].standardOptions.threshold.step.withValue(null) + panel[type].standardOptions.threshold.step.withColor('green'),
        panel[type].standardOptions.threshold.step.withValue(50) + panel[type].standardOptions.threshold.step.withColor('yellow'),
        panel[type].standardOptions.threshold.step.withValue(75) + panel[type].standardOptions.threshold.step.withColor('orange'),
        panel[type].standardOptions.threshold.step.withValue(90) + panel[type].standardOptions.threshold.step.withColor('red'),
      ]),
}
