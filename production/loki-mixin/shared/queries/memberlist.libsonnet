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

  // Counter rates for TCP metrics
  // Gets the rate of TCP accepts
  tcp_accept_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(counter_memberlist_tcp_accept{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of TCP connects
  tcp_connect_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(counter_memberlist_tcp_connect{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of TCP sent messages
  tcp_sent_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(counter_memberlist_tcp_sent{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of UDP sent messages
  udp_sent_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(counter_memberlist_udp_sent{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Counter rates for message states
  // Gets the rate of alive messages
  msg_alive_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(counter_memberlist_msg_alive{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of dead messages
  msg_dead_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(counter_memberlist_msg_dead{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of suspect messages
  msg_suspect_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(counter_memberlist_msg_suspect{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of degraded probes
  degraded_probe_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(counter_memberlist_degraded_probe{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Client metrics
  // Gets the rate of CAS attempts
  cas_attempt_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_cas_attempt_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of CAS failures
  cas_failure_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_cas_failure_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of CAS successes
  cas_success_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_cas_success_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of broadcast messages dropped
  broadcasts_dropped_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_received_broadcasts_dropped_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of invalid broadcast messages
  broadcasts_invalid_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_received_broadcasts_invalid_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of received broadcast messages
  broadcasts_received_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_received_broadcasts_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of received broadcast bytes
  broadcasts_received_bytes_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_received_broadcasts_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // TCP Transport metrics
  // Gets the rate of dropped packets
  tcp_packets_dropped_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_packets_dropped_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of received packets
  tcp_packets_received_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_packets_received_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of received packet errors
  tcp_packets_received_errors_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_packets_received_errors_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of sent packets
  tcp_packets_sent_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_packets_sent_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of sent packet errors
  tcp_packets_sent_errors_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_packets_sent_errors_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gauge metrics
  // Gets the health score
  health_score(component, by=config.labels.resource_selector)::
    |||
      gauge_memberlist_health_score{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the number of cluster members
  cluster_members_count(component, by=config.labels.resource_selector)::
    |||
      loki_memberlist_client_cluster_members_count{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the cluster node health score
  cluster_node_health_score(component, by=config.labels.resource_selector)::
    |||
      loki_memberlist_client_cluster_node_health_score{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the number of key-value store entries
  kv_store_count(component, by=config.labels.resource_selector)::
    |||
      loki_memberlist_client_kv_store_count{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the messages in broadcast queue
  messages_in_broadcast_queue(component, by=config.labels.resource_selector)::
    |||
      loki_memberlist_client_messages_in_broadcast_queue{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the bytes in broadcast queue
  messages_in_broadcast_queue_bytes(component, by=config.labels.resource_selector)::
    |||
      loki_memberlist_client_messages_in_broadcast_queue_bytes{%s}
    ||| % [self._resourceSelector(component).build()],

  // Timer metrics with histogram support
  // Gets the gossip timer percentile
  gossip_timer_percentile(component, percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(timer_memberlist_gossip_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the probe node timer percentile
  probe_node_timer_percentile(component, percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(timer_memberlist_probeNode_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the push/pull node timer percentile
  push_pull_node_timer_percentile(component, percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(timer_memberlist_pushPullNode_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],
}
