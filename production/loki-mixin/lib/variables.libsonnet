/*
* Wrapper for variable, that allows arguments to be passed as a single object,
* and calls the appropriate with* methods automatically
* Docs: https://grafana.github.io/grafonnet/API/dashboard/variable.html
*/
// imports
local g = import './grafana.libsonnet';
local utils = import './utils.libsonnet';

// local variables
local variable = g.dashboard.variable;

local defaultParams = {
  name: null,
  type: null,
};

local newVariable(params) = (
    local merged =
      defaultParams
      + params
      + (
          if std.objectHas(params, 'label') && params.label != null then
            { label: params.label }
          else
            {}
        )
      + (
          if std.objectHas(params, 'description') && params.description != null then
            { description: params.description }
          else
            {}
        );

    local check = utils.required(merged, ['name', 'kind']);
    local check = utils.required(variable, merged.kind, 'variable type %s is not supported');

    local var = variable[merged.kind];
    local generalKeys = ['current', 'description', 'label'];
    local rootKeys = ['regex', 'datasourceFromVariable', 'sort'];
    local selectionKeys = ['multi']; // TODO: handle include all with custom all value

    (
      if merged.kind == 'adhoc' && utils.required(params, ['type', 'uid'])then
        var.new(merged.name, merged.type, merged.uid)
      else if merged.kind == 'constant' && utils.required(merged, ['value']) then
        var.new(merged.name, merged.value)
      else if (merged.kind == 'custom' || merged.kind == 'interval') && utils.required(merged, ['values']) then
        var.new(merged.name, merged.values)
      else if merged.kind == 'datasource' && utils.required(merged, ['type']) then
        var.new(merged.name, merged.type)
      else if merged.kind == 'query' then
        var.new(merged.name, merged.query)
          + (
            if std.objectHas(merged, 'refresh') && merged.refresh != null && std.objectHas(var.refresh, merged.refresh) then
              var.refresh[merged.refresh]()
            else
              {}
          )
          + (
            if std.objectHas(merged, 'queryTypes') && std.objectHas(merged.queryTypes, 'label') && merged.queryTypes.label != null then
              var.queryTypes.withLabelValues(
                merged.queryTypes.label,
                (
                  if std.objectHas(merged.queryTypes, 'metric') && merged.queryTypes.metric != null then
                    merged.queryTypes.metric
                  else
                    ''
                )
              )
            else
              {}
          )
      else if merged.kind == 'textbox' then
        var.new(merged.name, merged.default)
    )
      + utils.applyOptions(var, rootKeys, merged)
      + utils.applyOptions(var.generalOptions, generalKeys, merged)
      + (
        if std.objectHas(var, 'selectionOptions') then
          utils.applyOptions(var.selectionOptions, selectionKeys, merged)
        else
          {}
      )
      + (
        // handle showing on the dashboard
        if std.objectHas(merged, 'showOnDashboard') && merged.showOnDashboard != null then
          local methodName = utils.methodNameFromKey(merged.showOnDashboard);
          if std.objectHas(var.generalOptions.showOnDashboard, methodName) then
            var.generalOptions.showOnDashboard[methodName]()
          else
            {}
        else
          {}
      )
      + (
        // handle include all with custom all value
        if std.objectHas(var, 'selectionOptions') && std.objectHas(var.selectionOptions, 'withIncludeAll') && std.objectHas(merged, 'includeAll') && merged.includeAll != null then
          var.selectionOptions.withIncludeAll(
            merged.includeAll, (
            if std.objectHas(merged, 'customAllValue') && merged.customAllValue != null then
              merged.customAllValue
            else
              ''
            )
          )
        else
          {}
      )
  );

{
  adhoc(params)::
    newVariable(params + { kind: 'adhoc' }),

  constant(params)::
    newVariable(params + { kind: 'constant' }),

  custom(params)::
    newVariable(params + { kind: 'custom' }),

  datasource(params)::
    newVariable(params + { kind: 'datasource' }),

  interval(params)::
    newVariable(params + { kind: 'interval' }),

  query(params)::
    newVariable(params + { kind: 'query' }),

  textbox(params)::
    newVariable(params + { kind: 'textbox' }),
}
