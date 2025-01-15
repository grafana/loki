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

  // Gets the rate of degraded probes
  degraded_probe_rate(component, by='')::
    |||
      sum by (%s) (
        rate(counter_memberlist_degraded_probe{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of alive messages
  msg_alive_rate(component, by='')::
    |||
      sum by (%s) (
        rate(counter_memberlist_msg_alive{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of dead messages
  msg_dead_rate(component, by='')::
    |||
      sum by (%s) (
        rate(counter_memberlist_msg_dead{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of suspect messages
  msg_suspect_rate(component, by='')::
    |||
      sum by (%s) (
        rate(counter_memberlist_msg_suspect{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of TCP accepts
  tcp_accept_rate(component, by='')::
    |||
      sum by (%s) (
        rate(counter_memberlist_tcp_accept{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of TCP connects
  tcp_connect_rate(component, by='')::
    |||
      sum by (%s) (
        rate(counter_memberlist_tcp_connect{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of TCP sends
  tcp_sent_rate(component, by='')::
    |||
      sum by (%s) (
        rate(counter_memberlist_tcp_sent{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of UDP sends
  udp_sent_rate(component, by='')::
    |||
      sum by (%s) (
        rate(counter_memberlist_udp_sent{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the health score
  health_score(component, by='')::
    |||
      sum by (%s) (
        gauge_memberlist_health_score{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of CAS attempts
  cas_attempt_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_cas_attempt_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of CAS failures
  cas_failure_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_cas_failure_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of CAS successes
  cas_success_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_cas_success_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the cluster members count
  cluster_members_count(component, by='')::
    |||
      sum by (%s) (
        loki_memberlist_client_cluster_members_count{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the cluster node health score
  cluster_node_health_score(component, by='')::
    |||
      sum by (%s) (
        loki_memberlist_client_cluster_node_health_score{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the KV store count
  kv_store_count(component, by='')::
    |||
      sum by (%s) (
        loki_memberlist_client_kv_store_count{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the KV store value tombstones
  kv_store_value_tombstones(component, by='')::
    |||
      sum by (%s) (
        loki_memberlist_client_kv_store_value_tombstones{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of KV store value tombstones removed
  kv_store_value_tombstones_removed_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_kv_store_value_tombstones_removed_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the messages in broadcast queue
  messages_in_broadcast_queue(component, by='')::
    |||
      sum by (%s) (
        loki_memberlist_client_messages_in_broadcast_queue{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the messages in broadcast queue bytes
  messages_in_broadcast_queue_bytes(component, by='')::
    |||
      sum by (%s) (
        loki_memberlist_client_messages_in_broadcast_queue_bytes{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of messages dropped from broadcast queue
  messages_to_broadcast_dropped_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_messages_to_broadcast_dropped_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of received broadcast bytes
  received_broadcasts_bytes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_received_broadcasts_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of dropped received broadcasts
  received_broadcasts_dropped_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_received_broadcasts_dropped_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of invalid received broadcasts
  received_broadcasts_invalid_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_received_broadcasts_invalid_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of received broadcasts
  received_broadcasts_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_received_broadcasts_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of state pull bytes
  state_pulls_bytes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_state_pulls_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of state pulls
  state_pulls_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_state_pulls_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of state push bytes
  state_pushes_bytes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_state_pushes_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of state pushes
  state_pushes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_client_state_pushes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of incoming TCP streams
  tcp_transport_incoming_streams_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_incoming_streams_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of outgoing TCP stream errors
  tcp_transport_outgoing_stream_errors_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_outgoing_stream_errors_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of outgoing TCP streams
  tcp_transport_outgoing_streams_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_outgoing_streams_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of dropped TCP packets
  tcp_transport_packets_dropped_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_packets_dropped_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of received TCP packet bytes
  tcp_transport_packets_received_bytes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_packets_received_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of TCP packet receive errors
  tcp_transport_packets_received_errors_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_packets_received_errors_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of received TCP packets
  tcp_transport_packets_received_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_packets_received_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of sent TCP packet bytes
  tcp_transport_packets_sent_bytes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_packets_sent_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of TCP packet send errors
  tcp_transport_packets_sent_errors_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_packets_sent_errors_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of sent TCP packets
  tcp_transport_packets_sent_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_packets_sent_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of unknown TCP connections
  tcp_transport_unknown_connections_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memberlist_tcp_transport_unknown_connections_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of gossip duration
  gossip_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(timer_memberlist_gossip{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of gossip duration
  gossip_duration_p99(component, by=''):: self.gossip_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of gossip duration
  gossip_duration_p95(component, by=''):: self.gossip_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of gossip duration
  gossip_duration_p90(component, by=''):: self.gossip_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of gossip duration
  gossip_duration_p50(component, by=''):: self.gossip_duration_percentile(component, 0.50, by),

  // Gets the average gossip duration
  gossip_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(timer_memberlist_gossip_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(timer_memberlist_gossip_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of gossip duration
  gossip_duration_histogram(component)::
    |||
      sum by (le) (
        rate(timer_memberlist_gossip{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the percentile of probe node duration
  probe_node_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(timer_memberlist_probeNode{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of probe node duration
  probe_node_duration_p99(component, by=''):: self.probe_node_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of probe node duration
  probe_node_duration_p95(component, by=''):: self.probe_node_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of probe node duration
  probe_node_duration_p90(component, by=''):: self.probe_node_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of probe node duration
  probe_node_duration_p50(component, by=''):: self.probe_node_duration_percentile(component, 0.50, by),

  // Gets the average probe node duration
  probe_node_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(timer_memberlist_probeNode_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(timer_memberlist_probeNode_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of probe node duration
  probe_node_duration_histogram(component)::
    |||
      sum by (le) (
        rate(timer_memberlist_probeNode{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the percentile of push pull node duration
  push_pull_node_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(timer_memberlist_pushPullNode{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of push pull node duration
  push_pull_node_duration_p99(component, by=''):: self.push_pull_node_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of push pull node duration
  push_pull_node_duration_p95(component, by=''):: self.push_pull_node_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of push pull node duration
  push_pull_node_duration_p90(component, by=''):: self.push_pull_node_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of push pull node duration
  push_pull_node_duration_p50(component, by=''):: self.push_pull_node_duration_percentile(component, 0.50, by),

  // Gets the average push pull node duration
  push_pull_node_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(timer_memberlist_pushPullNode_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(timer_memberlist_pushPullNode_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of push pull node duration
  push_pull_node_duration_histogram(component)::
    |||
      sum by (le) (
        rate(timer_memberlist_pushPullNode{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],
}
