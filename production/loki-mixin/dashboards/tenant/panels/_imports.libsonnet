{
  tenantOverrides:: import './tenant-overrides.libsonnet',
  mergedOverrides:: import './merged-overrides.libsonnet',
  bytesIn:: import './bytes-in.libsonnet',
  structuredMetadataBytesIn:: import './structured-metadata-bytes-in.libsonnet',
  linesIn:: import './lines-in.libsonnet',
  avgLineSize:: import './avg-line-size.libsonnet',
  discardedBytes:: import './discarded-bytes.libsonnet',
  discardedLines:: import './discarded-lines.libsonnet',
  activeStreams:: import './active-streams.libsonnet',
  queueLength:: import './queue-length.libsonnet',
  queueDuration:: import './queue-duration.libsonnet',
}
