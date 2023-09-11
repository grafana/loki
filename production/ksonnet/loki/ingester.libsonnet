local k = import 'ksonnet-util/kausal.libsonnet';
{
  local container = k.core.v1.container,
  local pvc = k.core.v1.persistentVolumeClaim,
  local volumeMount = k.core.v1.volumeMount,
  local statefulSet = k.apps.v1.statefulSet,
  local podDisruptionBudget = k.policy.v1.podDisruptionBudget,

  local name = 'ingester',

  // The ingesters should persist TSDB blocks and WAL on a persistent
  // volume in order to be crash resilient.
  local ingester_data_pvc =
    pvc.new() +
    pvc.mixin.spec.resources.withRequests({ storage: $._config.ingester_data_disk_size }) +
    pvc.mixin.spec.withAccessModes(['ReadWriteOnce']) +
    pvc.mixin.spec.withStorageClassName($._config.ingester_data_disk_class) +
    pvc.mixin.metadata.withName('ingester-data'),

  newIngesterStatefulSet(name, container, with_anti_affinity=true)::
    $.newLokiStatefulSet(name, 3, container, ingester_data_pvc) +
    // When the ingester needs to flush blocks to the storage, it may take quite a lot of time.
    // For this reason, we grant an high termination period (80 minutes).
    statefulSet.mixin.spec.template.spec.withTerminationGracePeriodSeconds(4800) +
    // $.lokiVolumeMounts +
    $.util.podPriority('high') +
    (if with_anti_affinity then $.util.antiAffinity else {}),

  ingester_args::
    $._config.commonArgs {
      target: 'ingester',
    },

  ingester_ports: $.util.defaultPorts,

  ingester_container::
    container.new('ingester', $._images.ingester) +
    container.withPorts($.ingester_ports) +
    container.withArgsMixin(k.util.mapToFlags($.ingester_args)) +
    container.mixin.readinessProbe.httpGet.withPath('/ready') +
    container.mixin.readinessProbe.httpGet.withPort($._config.http_listen_port) +
    container.mixin.readinessProbe.withInitialDelaySeconds(15) +
    container.mixin.readinessProbe.withTimeoutSeconds(1) +
    k.util.resourcesRequests('1', '5Gi') +
    k.util.resourcesLimits('2', '10Gi') +
    container.withEnvMixin($._config.commonEnvs) +
    container.withVolumeMountsMixin([
      volumeMount.new('ingester-data', '/data'),
    ]),

  ingester_statefulset:
    self.newIngesterStatefulSet('ingester', $.ingester_container, !$._config.ingester_allow_multiple_replicas_on_same_node),

  ingester_service:
    k.util.serviceFor($.ingester_statefulset, $._config.service_ignored_labels),

  ingester_pdb:
    podDisruptionBudget.new('loki-ingester-pdb') +
    podDisruptionBudget.mixin.metadata.withLabels({ name: 'loki-ingester-pdb' }) +
    podDisruptionBudget.mixin.spec.selector.withMatchLabels({ name: name }) +
    podDisruptionBudget.mixin.spec.withMaxUnavailable(1),
}
