// imports
local config = import '../../../../config.libsonnet';
local lib = import '../../../../lib/_imports.libsonnet';
local common = import '../../../common/_imports.libsonnet';
local shared = import '../../../shared/_imports.libsonnet';

// local variables
local timeSeries = lib.panels.timeSeries;
local override = lib.panels.helpers.override('timeSeries');
local custom = lib.panels.helpers.custom('timeSeries');
local color = lib.panels.helpers.color('timeSeries');
local transformation = lib.panels.helpers.transformation('timeSeries');

local overrideColor(name, value) =
  override.byName.new(name)
    + override.byName.withPropertiesFromOptions(
        color.withMode('fixed')
        + color.withFixedColor(value)
      );

{
  new(
    title = 'QPS',
    targets = [],
    datasource = common.variables.metrics_datasource.name,
  )::
    lib.panels.timeSeries.qps({
      title: title,
      datasource: datasource,
      targets: targets,
      overrides: [
        overrideColor('1xx', '#EAB839'),
        overrideColor('2xx', '#7EB26D'),
        overrideColor('3xx', '#6ED0E0'),
        overrideColor('4xx', '#EF843C'),
        overrideColor('5xx', '#E24D42'),
        overrideColor('OK', '#7EB26D'),
        overrideColor('cancel', '#A9A9A9'),
        overrideColor('error', '#E24D42'),
        overrideColor('success', '#7EB26D'),
      ],
    })
    + custom.withFillOpacity(100)
    + custom.withLineWidth(0)
    + custom.withShowPoints('never')
    + custom.withStacking({
      group: targets[0].refId,
      mode: "normal"
    })
}
