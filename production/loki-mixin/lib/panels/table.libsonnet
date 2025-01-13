// imports
local g = import '../grafana.libsonnet';
local Base = import './_base.libsonnet';
local utils = import '../utils.libsonnet';

// local variables
local table = g.panel.table;

// local variables
local defaultParams = {};

table + Base + {
  new(params)::
    local merged = defaultParams + params;
    local footerKeys = utils.keyNamesFromMethods(table.options.footer);
    super.new(
      type='table',
      params=defaultParams + params,
    )
      + utils.applyOptions(table.options.footer, footerKeys, merged),
}
