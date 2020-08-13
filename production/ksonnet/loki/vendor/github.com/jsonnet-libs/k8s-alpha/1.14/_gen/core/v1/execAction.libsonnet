{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='execAction', url='', help='ExecAction describes a "run in container" action.'),
  '#withCommand':: d.fn(help="Command is the command line to execute inside the container, the working directory for the command  is root ('/') in the container's filesystem. The command is simply exec'd, it is not run inside a shell, so traditional shell instructions ('|', etc) won't work. To use a shell, you need to explicitly call out to that shell. Exit status of 0 is treated as live/healthy and non-zero is unhealthy.", args=[d.arg(name='command', type=d.T.array)]),
  withCommand(command): { command: if std.isArray(v=command) then command else [command] },
  '#withCommandMixin':: d.fn(help="Command is the command line to execute inside the container, the working directory for the command  is root ('/') in the container's filesystem. The command is simply exec'd, it is not run inside a shell, so traditional shell instructions ('|', etc) won't work. To use a shell, you need to explicitly call out to that shell. Exit status of 0 is treated as live/healthy and non-zero is unhealthy.\n\n**Note:** This function appends passed data to existing values", args=[d.arg(name='command', type=d.T.array)]),
  withCommandMixin(command): { command+: if std.isArray(v=command) then command else [command] },
  '#mixin': 'ignore',
  mixin: self
}