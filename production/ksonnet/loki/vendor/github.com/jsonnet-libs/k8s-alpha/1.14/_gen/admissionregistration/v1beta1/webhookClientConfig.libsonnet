{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='webhookClientConfig', url='', help='WebhookClientConfig contains the information to make a TLS connection with the webhook'),
  '#service':: d.obj(help='ServiceReference holds a reference to Service.legacy.k8s.io'),
  service: {
    '#withName':: d.fn(help='`name` is the name of the service. Required', args=[d.arg(name='name', type=d.T.string)]),
    withName(name): { service+: { name: name } },
    '#withNamespace':: d.fn(help='`namespace` is the namespace of the service. Required', args=[d.arg(name='namespace', type=d.T.string)]),
    withNamespace(namespace): { service+: { namespace: namespace } },
    '#withPath':: d.fn(help='`path` is an optional URL path which will be sent in any request to this service.', args=[d.arg(name='path', type=d.T.string)]),
    withPath(path): { service+: { path: path } }
  },
  '#withCaBundle':: d.fn(help="`caBundle` is a PEM encoded CA bundle which will be used to validate the webhook's server certificate. If unspecified, system trust roots on the apiserver are used.", args=[d.arg(name='caBundle', type=d.T.string)]),
  withCaBundle(caBundle): { caBundle: caBundle },
  '#withUrl':: d.fn(help='`url` gives the location of the webhook, in standard URL form (`scheme://host:port/path`). Exactly one of `url` or `service` must be specified.\n\nThe `host` should not refer to a service running in the cluster; use the `service` field instead. The host might be resolved via external DNS in some apiservers (e.g., `kube-apiserver` cannot resolve in-cluster DNS as that would be a layering violation). `host` may also be an IP address.\n\nPlease note that using `localhost` or `127.0.0.1` as a `host` is risky unless you take great care to run this webhook on all hosts which run an apiserver which might need to make calls to this webhook. Such installs are likely to be non-portable, i.e., not easy to turn up in a new cluster.\n\nThe scheme must be "https"; the URL must begin with "https://".\n\nA path is optional, and if present may be any string permissible in a URL. You may use the path to pass an arbitrary string to the webhook, for example, a cluster identifier.\n\nAttempting to use a user or basic auth e.g. "user:password@" is not allowed. Fragments ("#...") and query parameters ("?...") are not allowed, either.', args=[d.arg(name='url', type=d.T.string)]),
  withUrl(url): { url: url },
  '#mixin': 'ignore',
  mixin: self
}