{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='allowedFlexVolume', url='', help='AllowedFlexVolume represents a single Flexvolume that is allowed to be used. Deprecated: use AllowedFlexVolume from policy API Group instead.'),
  '#withDriver':: d.fn(help='driver is the name of the Flexvolume driver.', args=[d.arg(name='driver', type=d.T.string)]),
  withDriver(driver): { driver: driver },
  '#mixin': 'ignore',
  mixin: self
}