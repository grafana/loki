// imports
local lib = import '../../lib/_imports.libsonnet';

{
  // gets the common overrides for request and limit for a given panel type
  requestLimits(type)::
    local override = lib.panels.helpers.override(type);
    local custom = lib.panels.helpers.custom(type);
    local color = lib.panels.helpers.color(type);
    local defaults =
      color.withMode('fixed')
      + custom.withShowPoints('never')
      + custom.withLineWidth(2)
      + custom.withFillOpacity(0)
      + custom.withHideFrom({
          "legend": true,
          "tooltip": false,
          "viz": false
        })
    ;
    [
      // request
      override.byName.new('requests')
      + override.byName.withPropertiesFromOptions(
          defaults
          + color.withFixedColor('#EAB839')
          + custom.withLineStyle({
              "dash": [0, 10],
              "fill": "dot",
            })
      ),
      // limit
      override.byName.new('limits')
      + override.byName.withPropertiesFromOptions(
          defaults
          + color.withFixedColor('#E02F44')
          + custom.withLineStyle({
              "dash": [10, 20],
              "fill": "dash",
            })
      )
    ]
}
