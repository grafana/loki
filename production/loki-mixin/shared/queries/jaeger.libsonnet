// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the rate of baggage restrictions updates
  baggage_restrictions_updates_rate(component, by='')::
    |||
      sum by (%s) (
        rate(jaeger_tracer_baggage_restrictions_updates_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of baggage truncations
  baggage_truncations_rate(component, by='')::
    |||
      sum by (%s) (
        rate(jaeger_tracer_baggage_truncations_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of baggage updates
  baggage_updates_rate(component, by='')::
    |||
      sum by (%s) (
        rate(jaeger_tracer_baggage_updates_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of finished spans
  finished_spans_rate(component, by='')::
    |||
      sum by (%s) (
        rate(jaeger_tracer_finished_spans_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the current reporter queue length
  reporter_queue_length(component, by='')::
    |||
      sum by (%s) (
        jaeger_tracer_reporter_queue_length{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of reported spans
  reporter_spans_rate(component, by='')::
    |||
      sum by (%s) (
        rate(jaeger_tracer_reporter_spans_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of sampler queries
  sampler_queries_rate(component, by='')::
    |||
      sum by (%s) (
        rate(jaeger_tracer_sampler_queries_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of sampler updates
  sampler_updates_rate(component, by='')::
    |||
      sum by (%s) (
        rate(jaeger_tracer_sampler_updates_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of span context decoding errors
  span_context_decoding_errors_rate(component, by='')::
    |||
      sum by (%s) (
        rate(jaeger_tracer_span_context_decoding_errors_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of started spans
  started_spans_rate(component, by='')::
    |||
      sum by (%s) (
        rate(jaeger_tracer_started_spans_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of throttled debug spans
  throttled_debug_spans_rate(component, by='')::
    |||
      sum by (%s) (
        rate(jaeger_tracer_throttled_debug_spans_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of throttler updates
  throttler_updates_rate(component, by='')::
    |||
      sum by (%s) (
        rate(jaeger_tracer_throttler_updates_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of traces
  traces_rate(component, by='')::
    |||
      sum by (%s) (
        rate(jaeger_tracer_traces_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],
}
