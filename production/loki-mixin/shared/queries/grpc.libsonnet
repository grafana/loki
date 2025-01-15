// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the rate of handled gRPC client requests
  client_handled_rate(component, by='')::
    |||
      sum by (%s) (
        rate(grpc_client_handled_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of received gRPC client messages
  client_msg_received_rate(component, by='')::
    |||
      sum by (%s) (
        rate(grpc_client_msg_received_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of sent gRPC client messages
  client_msg_sent_rate(component, by='')::
    |||
      sum by (%s) (
        rate(grpc_client_msg_sent_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of started gRPC client requests
  client_started_rate(component, by='')::
    |||
      sum by (%s) (
        rate(grpc_client_started_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the success ratio of gRPC client requests
  client_success_ratio(component, by='')::
    |||
      sum by (%s) (rate(grpc_client_handled_total{%s, grpc_code="OK"}[$__rate_interval]))
      /
      sum by (%s) (rate(grpc_client_handled_total{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),
}
