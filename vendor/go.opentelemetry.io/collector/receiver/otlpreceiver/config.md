# "otlp" Receiver Reference

Config defines configuration for OTLP receiver.


### Config

| Name | Type | Default | Docs |
| ---- | ---- | ------- | ---- |
| protocols |[otlpreceiver-Protocols](#otlpreceiver-Protocols)| <no value> | Protocols is the configuration for the supported protocols, currently gRPC and HTTP (Proto and JSON).  |

### otlpreceiver-Protocols

| Name | Type | Default | Docs |
| ---- | ---- | ------- | ---- |
| grpc |[configgrpc-GRPCServerSettings](#configgrpc-GRPCServerSettings)| <no value> | GRPCServerSettings defines common settings for a gRPC server configuration.  |
| http |[confighttp-HTTPServerSettings](#confighttp-HTTPServerSettings)| <no value> | HTTPServerSettings defines settings for creating an HTTP server.  |

### configgrpc-GRPCServerSettings

| Name | Type | Default | Docs |
| ---- | ---- | ------- | ---- |
| endpoint |string| 0.0.0.0:4317 | Endpoint configures the address for this network connection. For TCP and UDP networks, the address has the form "host:port". The host must be a literal IP address, or a host name that can be resolved to IP addresses. The port must be a literal port number or a service name. If the host is a literal IPv6 address it must be enclosed in square brackets, as in "[2001:db8::1]:80" or "[fe80::1%zone]:80". The zone specifies the scope of the literal IPv6 address as defined in RFC 4007.  |
| transport |string| tcp | Transport to use. Known protocols are "tcp", "tcp4" (IPv4-only), "tcp6" (IPv6-only), "udp", "udp4" (IPv4-only), "udp6" (IPv6-only), "ip", "ip4" (IPv4-only), "ip6" (IPv6-only), "unix", "unixgram" and "unixpacket".  |
| tls |[configtls-TLSServerSetting](#configtls-TLSServerSetting)| <no value> | Configures the protocol to use TLS. The default value is nil, which will cause the protocol to not use TLS.  |
| max_recv_msg_size_mib |uint64| <no value> | MaxRecvMsgSizeMiB sets the maximum size (in MiB) of messages accepted by the server.  |
| max_concurrent_streams |uint32| <no value> | MaxConcurrentStreams sets the limit on the number of concurrent streams to each ServerTransport. It has effect only for streaming RPCs.  |
| read_buffer_size |int| 524288 | ReadBufferSize for gRPC server. See grpc.ReadBufferSize (https://godoc.org/google.golang.org/grpc#ReadBufferSize).  |
| write_buffer_size |int| <no value> | WriteBufferSize for gRPC server. See grpc.WriteBufferSize (https://godoc.org/google.golang.org/grpc#WriteBufferSize).  |
| keepalive |[configgrpc-KeepaliveServerConfig](#configgrpc-KeepaliveServerConfig)| <no value> | Keepalive anchor for all the settings related to keepalive.  |
| auth |[configauth-Authentication](#configauth-Authentication)| <no value> | Auth for this receiver  |

### configtls-TLSServerSetting

| Name | Type | Default | Docs |
| ---- | ---- | ------- | ---- |
| ca_file |string| <no value> | Path to the CA cert. For a client this verifies the server certificate. For a server this verifies client certificates. If empty uses system root CA. (optional)  |
| cert_file |string| <no value> | Path to the TLS cert to use for TLS required connections. (optional)  |
| key_file |string| <no value> | Path to the TLS key to use for TLS required connections. (optional)  |
| client_ca_file |string| <no value> | Path to the TLS cert to use by the server to verify a client certificate. (optional) This sets the ClientCAs and ClientAuth to RequireAndVerifyClientCert in the TLSConfig. Please refer to https://godoc.org/crypto/tls#Config for more information. (optional)  |

### configgrpc-KeepaliveServerConfig

| Name | Type | Default | Docs |
| ---- | ---- | ------- | ---- |
| server_parameters |[configgrpc-KeepaliveServerParameters](#configgrpc-KeepaliveServerParameters)| <no value> | KeepaliveServerParameters allow configuration of the keepalive.ServerParameters. The same default values as keepalive.ServerParameters are applicable and get applied by the server. See https://godoc.org/google.golang.org/grpc/keepalive#ServerParameters for details.  |
| enforcement_policy |[configgrpc-KeepaliveEnforcementPolicy](#configgrpc-KeepaliveEnforcementPolicy)| <no value> | KeepaliveEnforcementPolicy allow configuration of the keepalive.EnforcementPolicy. The same default values as keepalive.EnforcementPolicy are applicable and get applied by the server. See https://godoc.org/google.golang.org/grpc/keepalive#EnforcementPolicy for details.  |

### configgrpc-KeepaliveServerParameters

| Name | Type | Default | Docs |
| ---- | ---- | ------- | ---- |
| max_connection_idle |[time-Duration](#time-Duration)| <no value> |  |
| max_connection_age |[time-Duration](#time-Duration)| <no value> |  |
| max_connection_age_grace |[time-Duration](#time-Duration)| <no value> |  |
| time |[time-Duration](#time-Duration)| <no value> |  |
| timeout |[time-Duration](#time-Duration)| <no value> |  |

### configgrpc-KeepaliveEnforcementPolicy

| Name | Type | Default | Docs |
| ---- | ---- | ------- | ---- |
| min_time |[time-Duration](#time-Duration)| <no value> |  |
| permit_without_stream |bool| <no value> |  |

### configauth-Authentication

| Name | Type | Default | Docs |
| ---- | ---- | ------- | ---- |
| authenticator |string| <no value> | AuthenticatorName specifies the name of the extension to use in order to authenticate the incoming data point.  |

### confighttp-HTTPServerSettings

| Name                  | Type                                                      | Default      | Docs                                                                                                                                    |
| ----                  | ----                                                      | -------      | ----                                                                                                                                    |
| endpoint              | string                                                    | 0.0.0.0:4318 | Endpoint configures the listening address for the server.                                                                               |
| tls                   | [configtls-TLSServerSetting](#configtls-TLSServerSetting) | <no value>   | TLSSetting struct exposes TLS client configuration.                                                                                     |
| cors                  | [confighttp-CORSSettings](#confighttp-CORSSettings)       | <no value>   | CORSSettings configures a receiver for HTTP cross-origin resource sharing (CORS).                                                       |
| max_request_body_size | int                                                       | 0            | MaxRequestBodySize configures the maximum allowed body size in bytes for a single request. The default `0` means there's no restriction |

### confighttp-CORSSettings

| Name | Type | Default | Docs |
| ---- | ---- | ------- | ---- |
| allowed_origins |[]string| <no value> | AllowedOrigins sets the allowed values of the Origin header for HTTP/JSON requests to an OTLP receiver. An origin may contain a wildcard (`*`) to replace 0 or more characters (e.g., `"https://*.example.com"`, or `"*"` to allow any origin). |
| allowed_headers |[]string| <no value> | AllowedHeaders sets what headers will be allowed in CORS requests. The Accept, Accept-Language, Content-Type, and Content-Language headers are implicitly allowed. If no headers are listed, X-Requested-With will also be accepted by default. Include `"*"` to allow any request header. |
| max_age |int| <no value> | MaxAge sets the value of the Access-Control-Max-Age response header.  Set it to the number of seconds that browsers should cache a CORS preflight response for. |

### configtls-TLSServerSetting

| Name | Type | Default | Docs |
| ---- | ---- | ------- | ---- |
| ca_file |string| <no value> | Path to the CA cert. For a client this verifies the server certificate. For a server this verifies client certificates. If empty uses system root CA. (optional)  |
| cert_file |string| <no value> | Path to the TLS cert to use for TLS required connections. (optional)  |
| key_file |string| <no value> | Path to the TLS key to use for TLS required connections. (optional)  |
| client_ca_file |string| <no value> | Path to the TLS cert to use by the server to verify a client certificate. (optional) This sets the ClientCAs and ClientAuth to RequireAndVerifyClientCert in the TLSConfig. Please refer to https://godoc.org/crypto/tls#Config for more information. (optional)  |

### time-Duration 
An optionally signed sequence of decimal numbers, each with a unit suffix, such as `300ms`, `-1.5h`, or `2h45m`. Valid time units are `ns`, `us`, `ms`, `s`, `m`, `h`.
