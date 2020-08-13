{
  local d = (import 'doc-util/main.libsonnet'),
  '#':: d.pkg(name='podDNSConfig', url='', help='PodDNSConfig defines the DNS parameters of a pod in addition to those generated from DNSPolicy.'),
  '#withNameservers':: d.fn(help='A list of DNS name server IP addresses. This will be appended to the base nameservers generated from DNSPolicy. Duplicated nameservers will be removed.', args=[d.arg(name='nameservers', type=d.T.array)]),
  withNameservers(nameservers): { nameservers: if std.isArray(v=nameservers) then nameservers else [nameservers] },
  '#withNameserversMixin':: d.fn(help='A list of DNS name server IP addresses. This will be appended to the base nameservers generated from DNSPolicy. Duplicated nameservers will be removed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='nameservers', type=d.T.array)]),
  withNameserversMixin(nameservers): { nameservers+: if std.isArray(v=nameservers) then nameservers else [nameservers] },
  '#withOptions':: d.fn(help='A list of DNS resolver options. This will be merged with the base options generated from DNSPolicy. Duplicated entries will be removed. Resolution options given in Options will override those that appear in the base DNSPolicy.', args=[d.arg(name='options', type=d.T.array)]),
  withOptions(options): { options: if std.isArray(v=options) then options else [options] },
  '#withOptionsMixin':: d.fn(help='A list of DNS resolver options. This will be merged with the base options generated from DNSPolicy. Duplicated entries will be removed. Resolution options given in Options will override those that appear in the base DNSPolicy.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='options', type=d.T.array)]),
  withOptionsMixin(options): { options+: if std.isArray(v=options) then options else [options] },
  '#withSearches':: d.fn(help='A list of DNS search domains for host-name lookup. This will be appended to the base search paths generated from DNSPolicy. Duplicated search paths will be removed.', args=[d.arg(name='searches', type=d.T.array)]),
  withSearches(searches): { searches: if std.isArray(v=searches) then searches else [searches] },
  '#withSearchesMixin':: d.fn(help='A list of DNS search domains for host-name lookup. This will be appended to the base search paths generated from DNSPolicy. Duplicated search paths will be removed.\n\n**Note:** This function appends passed data to existing values', args=[d.arg(name='searches', type=d.T.array)]),
  withSearchesMixin(searches): { searches+: if std.isArray(v=searches) then searches else [searches] },
  '#mixin': 'ignore',
  mixin: self
}