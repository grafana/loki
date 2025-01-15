// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the enterprise logs build info
  build_info(component)::
    |||
      enterprise_logs_build_info{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the license expiry timestamp
  license_expiry_timestamp(component)::
    |||
      max(
        grafana_labs_license_expiry_timestamp{%s}
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the rate of local license sync failures
  license_local_sync_failures_rate(component, by='')::
    |||
      sum by (%s) (
        rate(grafana_labs_license_local_sync_failures_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of local license syncs
  license_local_syncs_rate(component, by='')::
    |||
      sum by (%s) (
        rate(grafana_labs_license_local_syncs_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the success ratio of local license syncs
  license_local_sync_success_ratio(component, by='')::
    |||
      (
        sum by (%s) (rate(grafana_labs_license_local_syncs_total{%s}[$__rate_interval]))
        -
        sum by (%s) (rate(grafana_labs_license_local_sync_failures_total{%s}[$__rate_interval]))
      )
      /
      sum by (%s) (rate(grafana_labs_license_local_syncs_total{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector(component).build()], 3),
}
