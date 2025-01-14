// imports
local lib = import '../../../lib/_imports.libsonnet';
local config = import '../../../config.libsonnet';
local variables = import '../../variables.libsonnet';
local selector = (import '../../selectors.libsonnet').new;

{
  // gets the pod limit or requests for a given component and resource
  _resource(type, component, resource)::
    local resourceSelector =
      selector()
        .label(config.labels.container).neq('')
        .label('resource').eq(resource)
        .pod(component)
        .build();
    |||
      sum(
        max by (%s, %s, %s, %s, %s, resource) (
          kube_pod_container_resource_%s{%s}
        )
      )
    ||| % [
      config.labels.cluster,
      config.labels.node,
      config.labels.namespace,
      config.labels.pod,
      config.labels.container,
      type,
      resourceSelector
    ],

  // gets the pod resource limit for a given component
  _limits(component, resource)::
    self._resource('limits', component, resource),

  // gets the pod resource requests for a given component
  _requests(component, resource)::
    self._resource('requests', component, resource),

  // gets the pod cpu usage for a given component
  cpu_usage(component, by=config.labels.pod)::
    local resourceSelector =
      selector()
        .label(config.labels.container).neq('')
        .pod(component)
        .build();
    |||
      sum by(%s) (
        rate(
          container_cpu_usage_seconds_total{%s}[$__rate_interval]
        )
      )
    ||| % [by, resourceSelector],

  // gets the pod cpu limit for a given component
  cpu_limits(component)::
    self._limits(component, 'cpu'),

  // gets the pod cpu requests for a given component
  cpu_requests(component)::
    self._requests(component, 'cpu'),

  memory_usage(component, by=config.labels.pod)::
    local resourceSelector =
      selector()
        .label(config.labels.container).neq('')
        .pod(component)
        .build();
    |||
      max by(%s) (
        container_memory_working_set_bytes{%s}
      )
    ||| % [by, resourceSelector],

  // gets the pod memory limits for a given component
  memory_limits(component)::
    self._limits(component, 'memory'),

  // gets the pod memory requests for a given component
  memory_requests(component)::
    self._requests(component, 'memory'),

  disk_writes(component)::
    local nodeSelector =
      selector(false)
        .cluster()
        .build();
    local podSelector =
      selector()
        .pod(component)
        .container(component)
        .build();
    |||
      sum by(instance, device) (
        rate(node_disk_written_bytes_total{%s}[$__rate_interval])
      )
      +
      ignoring(%s) group_right() (
        label_replace(
          count by(instance, %s, device) (
            container_fs_writes_bytes_total{%s, device!~".*sda.*"}
          ), "device", "$1", "device", "/dev/(.*)"
        ) * 0
      )
    ||| % [
      nodeSelector,
      config.labels.pod,
      config.labels.pod,
      podSelector,
    ],

  disk_reads(component)::
    local nodeSelector =
      selector(false)
        .cluster()
        .build();
    local podSelector =
      selector()
        .pod(component)
        .container(component)
        .build();
    |||
      sum by(instance, device) (
        rate(node_disk_read_bytes_total{%s}[$__rate_interval])
      )
      +
      ignoring(%s) group_right() (
        label_replace(
          count by(instance, %s, device) (
            container_fs_reads_bytes_total{%s, device!~".*sda.*"}
          ), "device", "$1", "device", "/dev/(.*)"
        ) * 0
      )
    ||| % [
      nodeSelector,
      config.labels.pod,
      config.labels.pod,
      podSelector,
    ],
}
