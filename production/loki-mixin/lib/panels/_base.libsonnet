// imports
local g = import '../grafana.libsonnet';
local variables = import '../dashboards/common/variables.libsonnet';
local utils = import '../utils.libsonnet';

// local variables
local panel = g.panel;

// local helper functions
local defaultParams = {
  title: null,
  targets: null,
  unit: 'none',
  desc: null,
  datasource: 'metrics_datasource',
  datasourceType: null,
  datasourceUid: null,
  graphMode: null,
  colorMode: null,
  mode: null,
  fixedColor: null,
  min: null,
  max: null,
  noValue: null,
  h: null,
  w: null,
  x: null,
  y: null,
  textMode: null,
  fields: null,
  calcs: null,
  values: null,
  transformations: [],
  hideTimeOverride: null,
};

{
  new(type, params):: (
    local merged = defaultParams + params;

    panel[type].new(params.title)

    // this query option is not currently in the schema so we're adding support for it here
    // https://github.com/grafana/grafonnet/blob/main/generator/core.libsonnet#L248C9-L248C40
    + (
      if std.objectHas(merged, 'hideTimeOverride') && merged.hideTimeOverride != null then
        { hideTimeOverride: merged.hideTimeOverride }
      else
        {}
    )

    // panel options
    + (
      if std.objectHas(panel, type) && std.objectHas(panel[type], 'panelOptions') then
        local panelOptions = panel[type].panelOptions;
        local pnlKeys = ['description'];
        utils.applyOptions(panelOptions, pnlKeys, merged)
          // add multi argument params
          + panelOptions.withGridPos(
            h=(if std.objectHas(merged, 'h') then merged.h else null),
            w=(if std.objectHas(merged, 'w') then merged.w else null),
            x=(if std.objectHas(merged, 'x') then merged.x else null),
            y=(if std.objectHas(merged, 'y') then merged.y else null),
          )
      else
        {}
    )

    // standard options
    + (
      if std.objectHas(panel, type) && std.objectHas(panel[type], 'standardOptions') then
        local standardOptions = panel[type].standardOptions;
        local stdKeys = utils.keyNamesFromMethods(standardOptions);
        local colorKeys = if std.objectHasAll(standardOptions, 'color') then
          utils.keyNamesFromMethods(standardOptions.color)
        else
          [];
        // apply standard options, there are methods are g.panel.standardOptions and g.panel.standardOptions.color
        utils.applyOptions(standardOptions, stdKeys, merged)
        + (
          if std.objectHasAll(standardOptions, 'color') then
            utils.applyOptions(standardOptions.color, colorKeys, merged)
          else
            {}
        )
    )

    // query options
    + (
      if std.objectHas(panel, type) && std.objectHas(panel[type], 'queryOptions') then
        local queryOptions = panel[type].queryOptions;
        local qryKeys = utils.keyNamesFromMethods(queryOptions, exclude=['datasource']);
        // apply query options
        utils.applyOptions(queryOptions, qryKeys, merged)
        // set datasource options
        + (if std.objectHas(merged, 'datasource') && merged.datasource != null then queryOptions.withDatasource(variables[merged.datasource].query, '${%s}' % variables[merged.datasource].name) else {})
        + (if std.objectHas(merged, 'datasourceType') && merged.datasourceType != null && merged.datasourceUid != null then queryOptions.withDatasource(merged.datasourceType, merged.datasourceUid) else {})
      else
        {}
    )

    // options, legend and reduce
    + (
      if std.objectHas(panel, type) && std.objectHas(panel[type], 'options') then
        local options = panel[type].options;
        local optKeys = utils.keyNamesFromMethods(options);
        local reduceKeys = if std.objectHasAll(options, 'reduceOptions') then
          utils.keyNamesFromMethods(options.reduceOptions)
        else
          [];
        local legendKeys = if std.objectHasAll(options, 'legend') then
          utils.keyNamesFromMethods(options.legend)
        else
          [];

        // apply standard options, there are methods are g.panel.standardOptions and g.panel.standardOptions.color
        utils.applyOptions(options, optKeys, merged)
        + (
          if std.objectHasAll(options, 'reduceOptions') then
            utils.applyOptions(options.reduceOptions, reduceKeys, merged)
          else
            {}
        )
        + (
          if std.objectHasAll(options, 'legend') then
            utils.applyOptions(options.legend, legendKeys, merged)
          else
            {}
        )
      else
        {}
    )
  ),

  // these are wrapper functions to make it easier to create panels with common units
  short(params)::
    self.new(params + { unit: 'short' }),

  percent(params)::
    self.new(params + { unit: 'percent' }),

  currency(params)::
    self.new(params + { unit: 'currencyUSD' }),

  bytes(params)::
    self.new(params + { unit: 'bytes' }),

  bytesRate(params)::
    self.new(params + { unit: 'binBps' }),

  gbytes(params)::
    self.new(params + { unit: 'gbytes' }),

  // count per second
  cps(params)::
    self.new(params + { unit: 'cps' }),

  // requests per second
  reqps(params)::
    self.new(params + { unit: 'reqps' }),

  // queries per second
  qps(params)::
    self.reqps(params),

  seconds(params)::
    self.new(params + { unit: 's' }),

}
