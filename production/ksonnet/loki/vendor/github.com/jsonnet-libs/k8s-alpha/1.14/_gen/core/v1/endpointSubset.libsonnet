{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='endpointSubset', url='', help='EndpointSubset is a group of addresses with a common set of ports. The expanded set of endpoints is the Cartesian product of Addresses x Ports. For example, given:\n  {\n    Addresses: [{"ip": "10.10.1.1"}, {"ip": "10.10.2.2"}],\n    Ports:     [{"name": "a", "port": 8675}, {"name": "b", "port": 309}]\n  }\nThe resulting set of endpoints can be viewed as:\n    a: [ 10.10.1.1:8675, 10.10.2.2:8675 ],\n    b: [ 10.10.1.1:309, 10.10.2.2:309 ]'),
  '#withAddresses':: d.fn(help='IP addresses which offer the related ports that are marked as ready. These endpoints should be considered safe for load balancers and clients to utilize.', args=[d.arg(name='addresses', type=d.T.array)]),
  withAddresses(addresses): { addresses: if std.isArray(v=addresses) then addresses else [addresses] },
  '#withAddressesMixin':: d.fn(help='IP addresses which offer the related ports that are marked as ready. These endpoints should be considered safe for load balancers and clients to utilize.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='addresses', type=d.T.array)]),
  withAddressesMixin(addresses): { addresses+: if std.isArray(v=addresses) then addresses else [addresses] },
  '#withNotReadyAddresses':: d.fn(help='IP addresses which offer the related ports but are not currently marked as ready because they have not yet finished starting, have recently failed a readiness check, or have recently failed a liveness check.', args=[d.arg(name='notReadyAddresses', type=d.T.array)]),
  withNotReadyAddresses(notReadyAddresses): { notReadyAddresses: if std.isArray(v=notReadyAddresses) then notReadyAddresses else [notReadyAddresses] },
  '#withNotReadyAddressesMixin':: d.fn(help='IP addresses which offer the related ports but are not currently marked as ready because they have not yet finished starting, have recently failed a readiness check, or have recently failed a liveness check.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='notReadyAddresses', type=d.T.array)]),
  withNotReadyAddressesMixin(notReadyAddresses): { notReadyAddresses+: if std.isArray(v=notReadyAddresses) then notReadyAddresses else [notReadyAddresses] },
  '#withPorts':: d.fn(help='Port numbers available on the related IP addresses.', args=[d.arg(name='ports', type=d.T.array)]),
  withPorts(ports): { ports: if std.isArray(v=ports) then ports else [ports] },
  '#withPortsMixin':: d.fn(help='Port numbers available on the related IP addresses.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='ports', type=d.T.array)]),
  withPortsMixin(ports): { ports+: if std.isArray(v=ports) then ports else [ports] },
  '#mixin': 'ignore',
  mixin: self
}