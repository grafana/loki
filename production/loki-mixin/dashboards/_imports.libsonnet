local requests = import './requests/dashboard.libsonnet';
local resources = import './resources/dashboard.libsonnet';
{
  backendRequests: requests.new(path='backend'),
  backendResources: resources.new(path='backend'),
  readRequests: requests.new(path='read'),
  readResources: resources.new(path='read'),
  tenant: (import './tenant/dashboard.libsonnet'),
  writeRequests: requests.new(path='write'),
  writeResources: resources.new(path='write'),
}
