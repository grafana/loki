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
    local optionKeys = ['cellHeight','sortBy'];
    local customKeys = ['filterable'];
    local footerKeys = ['enablePagination'];
    // table specific settings
    utils.applyOptions(table.options, optionKeys, merged)
    + utils.applyOptions(table.fieldConfig.defaults.custom, customKeys, merged)
    + utils.applyOptions(table.options.footer, footerKeys, merged)
    // there are 2 display mode types, look for one prefaced with field and call the explicit function
    + (
      if std.objectHas(merged, 'fieldDisplayMode') && merged.fieldDisplayMode != null then
        table.fieldConfig.defaults.custom.withDisplayMode(merged.fieldDisplayMode)
      else
        {}
    )
    + super.new(type = 'table', params = merged),
}
