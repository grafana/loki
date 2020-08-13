{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='containerStatus', url='', help='ContainerStatus contains details for the current status of this container.'),
  '#lastState':: d.obj(help='ContainerState holds a possible state of container. Only one of its members may be specified. If none of them is specified, the default one is ContainerStateWaiting.'),
  lastState: {
    '#running':: d.obj(help='ContainerStateRunning is a running state of a container.'),
    running: {
      '#withStartedAt':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='startedAt', type=d.T.string)]),
      withStartedAt(startedAt): { lastState+: { running+: { startedAt: startedAt } } }
    },
    '#terminated':: d.obj(help='ContainerStateTerminated is a terminated state of a container.'),
    terminated: {
      '#withContainerID':: d.fn(help="Container's ID in the format 'docker://<container_id>'", args=[d.arg(name='containerID', type=d.T.string)]),
      withContainerID(containerID): { lastState+: { terminated+: { containerID: containerID } } },
      '#withExitCode':: d.fn(help='Exit status from the last termination of the container', args=[d.arg(name='exitCode', type=d.T.integer)]),
      withExitCode(exitCode): { lastState+: { terminated+: { exitCode: exitCode } } },
      '#withFinishedAt':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='finishedAt', type=d.T.string)]),
      withFinishedAt(finishedAt): { lastState+: { terminated+: { finishedAt: finishedAt } } },
      '#withMessage':: d.fn(help='Message regarding the last termination of the container', args=[d.arg(name='message', type=d.T.string)]),
      withMessage(message): { lastState+: { terminated+: { message: message } } },
      '#withReason':: d.fn(help='(brief) reason from the last termination of the container', args=[d.arg(name='reason', type=d.T.string)]),
      withReason(reason): { lastState+: { terminated+: { reason: reason } } },
      '#withSignal':: d.fn(help='Signal from the last termination of the container', args=[d.arg(name='signal', type=d.T.integer)]),
      withSignal(signal): { lastState+: { terminated+: { signal: signal } } },
      '#withStartedAt':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='startedAt', type=d.T.string)]),
      withStartedAt(startedAt): { lastState+: { terminated+: { startedAt: startedAt } } }
    },
    '#waiting':: d.obj(help='ContainerStateWaiting is a waiting state of a container.'),
    waiting: {
      '#withMessage':: d.fn(help='Message regarding why the container is not yet running.', args=[d.arg(name='message', type=d.T.string)]),
      withMessage(message): { lastState+: { waiting+: { message: message } } },
      '#withReason':: d.fn(help='(brief) reason the container is not yet running.', args=[d.arg(name='reason', type=d.T.string)]),
      withReason(reason): { lastState+: { waiting+: { reason: reason } } }
    }
  },
  '#state':: d.obj(help='ContainerState holds a possible state of container. Only one of its members may be specified. If none of them is specified, the default one is ContainerStateWaiting.'),
  state: {
    '#running':: d.obj(help='ContainerStateRunning is a running state of a container.'),
    running: {
      '#withStartedAt':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='startedAt', type=d.T.string)]),
      withStartedAt(startedAt): { state+: { running+: { startedAt: startedAt } } }
    },
    '#terminated':: d.obj(help='ContainerStateTerminated is a terminated state of a container.'),
    terminated: {
      '#withContainerID':: d.fn(help="Container's ID in the format 'docker://<container_id>'", args=[d.arg(name='containerID', type=d.T.string)]),
      withContainerID(containerID): { state+: { terminated+: { containerID: containerID } } },
      '#withExitCode':: d.fn(help='Exit status from the last termination of the container', args=[d.arg(name='exitCode', type=d.T.integer)]),
      withExitCode(exitCode): { state+: { terminated+: { exitCode: exitCode } } },
      '#withFinishedAt':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='finishedAt', type=d.T.string)]),
      withFinishedAt(finishedAt): { state+: { terminated+: { finishedAt: finishedAt } } },
      '#withMessage':: d.fn(help='Message regarding the last termination of the container', args=[d.arg(name='message', type=d.T.string)]),
      withMessage(message): { state+: { terminated+: { message: message } } },
      '#withReason':: d.fn(help='(brief) reason from the last termination of the container', args=[d.arg(name='reason', type=d.T.string)]),
      withReason(reason): { state+: { terminated+: { reason: reason } } },
      '#withSignal':: d.fn(help='Signal from the last termination of the container', args=[d.arg(name='signal', type=d.T.integer)]),
      withSignal(signal): { state+: { terminated+: { signal: signal } } },
      '#withStartedAt':: d.fn(help='Time is a wrapper around time.Time which supports correct marshaling to YAML and JSON.  Wrappers are provided for many of the factory methods that the time package offers.', args=[d.arg(name='startedAt', type=d.T.string)]),
      withStartedAt(startedAt): { state+: { terminated+: { startedAt: startedAt } } }
    },
    '#waiting':: d.obj(help='ContainerStateWaiting is a waiting state of a container.'),
    waiting: {
      '#withMessage':: d.fn(help='Message regarding why the container is not yet running.', args=[d.arg(name='message', type=d.T.string)]),
      withMessage(message): { state+: { waiting+: { message: message } } },
      '#withReason':: d.fn(help='(brief) reason the container is not yet running.', args=[d.arg(name='reason', type=d.T.string)]),
      withReason(reason): { state+: { waiting+: { reason: reason } } }
    }
  },
  '#withContainerID':: d.fn(help="Container's ID in the format 'docker://<container_id>'.", args=[d.arg(name='containerID', type=d.T.string)]),
  withContainerID(containerID): { containerID: containerID },
  '#withImage':: d.fn(help='The image the container is running. More info: https://kubernetes.io/docs/concepts/containers/images', args=[d.arg(name='image', type=d.T.string)]),
  withImage(image): { image: image },
  '#withImageID':: d.fn(help="ImageID of the container's image.", args=[d.arg(name='imageID', type=d.T.string)]),
  withImageID(imageID): { imageID: imageID },
  '#withName':: d.fn(help='This must be a DNS_LABEL. Each container in a pod must have a unique name. Cannot be updated.', args=[d.arg(name='name', type=d.T.string)]),
  withName(name): { name: name },
  '#withReady':: d.fn(help='Specifies whether the container has passed its readiness probe.', args=[d.arg(name='ready', type=d.T.boolean)]),
  withReady(ready): { ready: ready },
  '#withRestartCount':: d.fn(help='The number of times the container has been restarted, currently based on the number of dead containers that have not yet been removed. Note that this is calculated from dead containers. But those containers are subject to garbage collection. This value will get capped at 5 by GC.', args=[d.arg(name='restartCount', type=d.T.integer)]),
  withRestartCount(restartCount): { restartCount: restartCount },
  '#mixin': 'ignore',
  mixin: self
}