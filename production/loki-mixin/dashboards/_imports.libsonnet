local pathRequests = import './read/dashboard.libsonnet';

{
  backendRequests: pathRequests.new(path='backend'),
  readRequests: pathRequests.new(path='read'),
  tenant: (import './tenant/dashboard.libsonnet'),
  writeRequests: pathRequests.new(path='write'),
}
