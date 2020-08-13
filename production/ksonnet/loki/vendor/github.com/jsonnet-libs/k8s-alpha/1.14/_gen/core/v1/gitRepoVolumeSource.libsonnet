{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='gitRepoVolumeSource', url='', help="Represents a volume that is populated with the contents of a git repository. Git repo volumes do not support ownership management. Git repo volumes support SELinux relabeling.\n\nDEPRECATED: GitRepo is deprecated. To provision a container with a git repo, mount an EmptyDir into an InitContainer that clones the repo using git, then mount the EmptyDir into the Pod's container."),
  '#withDirectory':: d.fn(help="Target directory name. Must not contain or start with '..'.  If '.' is supplied, the volume directory will be the git repository.  Otherwise, if specified, the volume will contain the git repository in the subdirectory with the given name.", args=[d.arg(name='directory', type=d.T.string)]),
  withDirectory(directory): { directory: directory },
  '#withRepository':: d.fn(help='Repository URL', args=[d.arg(name='repository', type=d.T.string)]),
  withRepository(repository): { repository: repository },
  '#withRevision':: d.fn(help='Commit hash for the specified revision.', args=[d.arg(name='revision', type=d.T.string)]),
  withRevision(revision): { revision: revision },
  '#mixin': 'ignore',
  mixin: self
}