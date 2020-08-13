{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='containerState', url='', help='ContainerState holds a possible state of container. Only one of its members may be specified. If none of them is specified, the default one is ContainerStateWaiting.'),
  '#running':: d.obj(help='ContainerStateRunning is a running state of a container.'),
  running: {
    '#withStartedAt':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='startedAt', type=d.T.string)]),
    withStartedAt(startedAt): { running+: { startedAt: startedAt } }
  },
  '#terminated':: d.obj(help='ContainerStateTerminated is a terminated state of a container.'),
  terminated: {
    '#withContainerID':: d.fn(help="Container's ID in the format 'docker://<container_id>'", args=[d.arg(name='containerID', type=d.T.string)]),
    withContainerID(containerID): { terminated+: { containerID: containerID } },
    '#withExitCode':: d.fn(help='Exit status from the last termination of the container', args=[d.arg(name='exitCode', type=d.T.integer)]),
    withExitCode(exitCode): { terminated+: { exitCode: exitCode } },
    '#withFinishedAt':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='finishedAt', type=d.T.string)]),
    withFinishedAt(finishedAt): { terminated+: { finishedAt: finishedAt } },
    '#withMessage':: d.fn(help='Message regarding the last termination of the container', args=[d.arg(name='message', type=d.T.string)]),
    withMessage(message): { terminated+: { message: message } },
    '#withReason':: d.fn(help='(brief) reason from the last termination of the container', args=[d.arg(name='reason', type=d.T.string)]),
    withReason(reason): { terminated+: { reason: reason } },
    '#withSignal':: d.fn(help='Signal from the last termination of the container', args=[d.arg(name='signal', type=d.T.integer)]),
    withSignal(signal): { terminated+: { signal: signal } },
    '#withStartedAt':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='startedAt', type=d.T.string)]),
    withStartedAt(startedAt): { terminated+: { startedAt: startedAt } }
  },
  '#waiting':: d.obj(help='ContainerStateWaiting is a waiting state of a container.'),
  waiting: {
    '#withMessage':: d.fn(help='Message regarding why the container is not yet running.', args=[d.arg(name='message', type=d.T.string)]),
    withMessage(message): { waiting+: { message: message } },
    '#withReason':: d.fn(help='(brief) reason the container is not yet running.', args=[d.arg(name='reason', type=d.T.string)]),
    withReason(reason): { waiting+: { reason: reason } }
  },
  '#mixin': 'ignore',
  mixin: self
}