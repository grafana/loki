// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    local sel = selector();
    if std.isArray(component) then
      sel.resources(component)
    else
      sel.resource(component),

  // Counter rates
  // Gets the rate of member heartbeats
  member_heartbeats_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ring_member_heartbeats_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of ruler ring check errors
  ruler_ring_check_errors_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ruler_ring_check_errors_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gauge metrics
  // Gets the number of tokens owned by each member
  member_tokens_owned(component, by=config.labels.resource_selector)::
    |||
      loki_ring_member_tokens_owned{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the number of tokens that should be owned by each member
  member_tokens_to_own(component, by=config.labels.resource_selector)::
    |||
      loki_ring_member_tokens_to_own{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the total number of members in the ring
  members(component, by=config.labels.resource_selector)::
    |||
      loki_ring_members{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the timestamp of the oldest member in the ring
  oldest_member_timestamp(component, by=config.labels.resource_selector)::
    |||
      loki_ring_oldest_member_timestamp{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the total number of tokens in the ring
  tokens_total(component, by=config.labels.resource_selector)::
    |||
      loki_ring_tokens_total{%s}
    ||| % [self._resourceSelector(component).build()],

  // Calculated metrics
  // Gets the percentage of tokens owned vs tokens that should be owned
  tokens_owned_percent(component, by=config.labels.resource_selector)::
    |||
      (
        sum by (%s) (loki_ring_member_tokens_owned{%s})
        /
        sum by (%s) (loki_ring_member_tokens_to_own{%s})
      ) * 100
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the age of the oldest member in seconds
  oldest_member_age_seconds(component, by=config.labels.resource_selector)::
    |||
      (time() - loki_ring_oldest_member_timestamp{%s})
    ||| % [self._resourceSelector(component).build()],
}
