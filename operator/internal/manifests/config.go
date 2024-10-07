package manifests

import (
	"crypto/sha1"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
)

// LokiConfigMap creates the single configmap containing the loki configuration for the whole cluster
func LokiConfigMap(opt Options) (*corev1.ConfigMap, string, error) {
	cfg := ConfigOptions(opt)

	if opt.Stack.Tenants != nil {
		if err := ConfigureOptionsForMode(&cfg, opt); err != nil {
			return nil, "", err
		}
	}

	c, rc, err := config.Build(cfg)
	if err != nil {
		return nil, "", err
	}

	s := sha1.New()
	_, err = s.Write(c)
	if err != nil {
		return nil, "", err
	}
	_, err = s.Write(rc)
	if err != nil {
		return nil, "", err
	}
	sha1C := fmt.Sprintf("%x", s.Sum(nil))

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   lokiConfigMapName(opt.Name),
			Labels: commonLabels(opt.Name),
		},
		Data: map[string]string{
			config.LokiConfigFileName:        string(c),
			config.LokiRuntimeConfigFileName: string(rc),
		},
	}, sha1C, nil
}

// ConfigOptions converts Options to config.Options
func ConfigOptions(opt Options) config.Options {
	rulerEnabled := opt.Stack.Rules != nil && opt.Stack.Rules.Enabled
	stackLimitsEnabled := opt.Stack.Limits != nil && len(opt.Stack.Limits.Tenants) > 0
	rulerLimitsEnabled := rulerEnabled && opt.Ruler.Spec != nil && len(opt.Ruler.Spec.Overrides) > 0

	var (
		evalInterval, pollInterval string
		amConfig                   *config.AlertManagerConfig
		rwConfig                   *config.RemoteWriteConfig
		overrides                  map[string]config.LokiOverrides
	)

	if rulerEnabled {
		// Map alertmanager config from CRD to config options
		if opt.Ruler.Spec != nil {
			evalInterval = string(opt.Ruler.Spec.EvalutionInterval)
			pollInterval = string(opt.Ruler.Spec.PollInterval)
			amConfig = alertManagerConfig(opt.Ruler.Spec.AlertManagerSpec)
		}

		// Map remote write config from CRD to config options
		if opt.Ruler.Spec != nil && opt.Ruler.Secret != nil {
			rwConfig = remoteWriteConfig(opt.Ruler.Spec.RemoteWriteSpec, opt.Ruler.Secret)
		}
	}

	if stackLimitsEnabled || rulerLimitsEnabled {
		overrides = map[string]config.LokiOverrides{}
	}

	if stackLimitsEnabled {
		for tenant, limits := range opt.Stack.Limits.Tenants {
			so := overrides[tenant]
			so.Limits = limits
			overrides[tenant] = so
		}
	}
	if rulerLimitsEnabled {
		for tenant, override := range opt.Ruler.Spec.Overrides {
			so := overrides[tenant]
			so.Ruler = config.RulerOverrides{
				AlertManager: alertManagerConfig(override.AlertManagerOverrides),
			}
			overrides[tenant] = so
		}
	}

	protocol := "http"
	if opt.Gates.HTTPEncryption {
		protocol = "https"
	}

	// nolint:staticcheck
	// Handle the deprecated field opt.Stack.ReplicationFactor.
	if (opt.Stack.Replication == nil || opt.Stack.Replication.Factor == 0) && opt.Stack.ReplicationFactor > 0 {
		if opt.Stack.Replication == nil {
			opt.Stack.Replication = &lokiv1.ReplicationSpec{}
		}

		opt.Stack.Replication.Factor = opt.Stack.ReplicationFactor
	}

	// Build a slice of with the shippers that are being used in the config
	// booleans used to prevent duplicates
	shippers := []string{}
	boltdb := false
	tsdb := false
	for _, schema := range opt.Stack.Storage.Schemas {
		if !boltdb && (schema.Version == lokiv1.ObjectStorageSchemaV11 || schema.Version == lokiv1.ObjectStorageSchemaV12) {
			shippers = append(shippers, "boltdb")
			boltdb = true
		} else if !tsdb {
			shippers = append(shippers, "tsdb")
			tsdb = true
		}
	}

	return config.Options{
		Stack: opt.Stack,
		Gates: opt.Gates,
		TLS: config.TLSOptions{
			Ciphers:       opt.TLSProfile.Ciphers,
			MinTLSVersion: opt.TLSProfile.MinTLSVersion,
			Paths: config.TLSFilePaths{
				CA: signingCAPath(),
				GRPC: config.TLSCertPath{
					Certificate: lokiServerGRPCTLSCert(),
					Key:         lokiServerGRPCTLSKey(),
				},
				HTTP: config.TLSCertPath{
					Certificate: lokiServerHTTPTLSCert(),
					Key:         lokiServerHTTPTLSKey(),
				},
			},
			ServerNames: config.TLSServerNames{
				GRPC: config.GRPCServerNames{
					Compactor:     fqdn(serviceNameCompactorGRPC(opt.Name), opt.Namespace),
					IndexGateway:  fqdn(serviceNameIndexGatewayGRPC(opt.Name), opt.Namespace),
					Ingester:      fqdn(serviceNameIngesterGRPC(opt.Name), opt.Namespace),
					QueryFrontend: fqdn(serviceNameQueryFrontendGRPC(opt.Name), opt.Namespace),
					Ruler:         fqdn(serviceNameRulerGRPC(opt.Name), opt.Namespace),
				},
				HTTP: config.HTTPServerNames{
					Querier: fqdn(serviceNameQuerierHTTP(opt.Name), opt.Namespace),
				},
			},
		},
		Namespace: opt.Namespace,
		Name:      opt.Name,
		Compactor: config.Address{
			FQDN: fqdn(NewCompactorGRPCService(opt).GetName(), opt.Namespace),
			Port: grpcPort,
		},
		FrontendWorker: config.Address{
			FQDN: fqdn(NewQueryFrontendGRPCService(opt).GetName(), opt.Namespace),
			Port: grpcPort,
		},
		GossipRing: gossipRingConfig(opt.Name, opt.Namespace, opt.Stack.HashRing, opt.Stack.Replication),
		Querier: config.Address{
			Protocol: protocol,
			FQDN:     fqdn(NewQuerierHTTPService(opt).GetName(), opt.Namespace),
			Port:     httpPort,
		},
		IndexGateway: config.Address{
			FQDN: fqdn(NewIndexGatewayGRPCService(opt).GetName(), opt.Namespace),
			Port: grpcPort,
		},
		StorageDirectory: dataDirectory,
		MaxConcurrent: config.MaxConcurrent{
			AvailableQuerierCPUCores: int32(opt.ResourceRequirements.Querier.Requests.Cpu().Value()),
		},
		WriteAheadLog: config.WriteAheadLog{
			Directory:             walDirectory,
			IngesterMemoryRequest: opt.ResourceRequirements.Ingester.Requests.Memory().Value(),
		},
		Shippers:              shippers,
		ObjectStorage:         opt.ObjectStorage,
		HTTPTimeouts:          opt.Timeouts.Loki,
		EnableRemoteReporting: opt.Gates.GrafanaLabsUsageReport,
		Ruler: config.Ruler{
			Enabled:               rulerEnabled,
			RulesStorageDirectory: rulesStorageDirectory,
			EvaluationInterval:    evalInterval,
			PollInterval:          pollInterval,
			AlertManager:          amConfig,
			RemoteWrite:           rwConfig,
		},
		Retention:      retentionConfig(&opt.Stack),
		OTLPAttributes: otlpAttributeConfig(&opt.Stack),
		Overrides:      overrides,
	}
}

func alertManagerConfig(spec *lokiv1.AlertManagerSpec) *config.AlertManagerConfig {
	if spec == nil {
		return nil
	}

	conf := &config.AlertManagerConfig{
		ExternalURL:    spec.ExternalURL,
		ExternalLabels: spec.ExternalLabels,
		Hosts:          strings.Join(spec.Endpoints, ","),
		EnableV2:       spec.EnableV2,
	}

	if d := spec.DiscoverySpec; d != nil {
		conf.EnableDiscovery = d.EnableSRV
		conf.RefreshInterval = string(d.RefreshInterval)
	}

	if n := spec.NotificationQueueSpec; n != nil {
		conf.QueueCapacity = n.Capacity
		conf.Timeout = string(n.Timeout)
		conf.ForOutageTolerance = string(n.ForOutageTolerance)
		conf.ForGracePeriod = string(n.ForGracePeriod)
		conf.ResendDelay = string(n.ResendDelay)
	}

	for _, cfg := range spec.RelabelConfigs {
		conf.RelabelConfigs = append(conf.RelabelConfigs, config.RelabelConfig{
			SourceLabels: cfg.SourceLabels,
			Separator:    cfg.Separator,
			TargetLabel:  cfg.TargetLabel,
			Regex:        cfg.Regex,
			Modulus:      cfg.Modulus,
			Replacement:  cfg.Replacement,
			Action:       string(cfg.Action),
		})
	}

	if clt := spec.Client; clt != nil {
		conf.Notifier = &config.NotifierConfig{}
		if tls := clt.TLS; tls != nil {
			conf.Notifier.TLS = config.TLSConfig{
				CAPath:             tls.CAPath,
				ServerName:         tls.ServerName,
				InsecureSkipVerify: tls.InsecureSkipVerify,
				CertPath:           tls.CertPath,
				KeyPath:            tls.KeyPath,
			}
		}

		if ha := clt.HeaderAuth; ha != nil {
			conf.Notifier.HeaderAuth = config.HeaderAuth{
				Type:            ha.Type,
				Credentials:     ha.Credentials,
				CredentialsFile: ha.CredentialsFile,
			}
		}

		if ba := clt.BasicAuth; ba != nil {
			conf.Notifier.BasicAuth = config.BasicAuth{
				Username: ba.Username,
				Password: ba.Password,
			}
		}
	}

	return conf
}

func gossipRingConfig(stackName, stackNs string, spec *lokiv1.HashRingSpec, replication *lokiv1.ReplicationSpec) config.GossipRing {
	var (
		instanceAddr string
		enableIPv6   bool
	)
	if spec != nil && spec.Type == lokiv1.HashRingMemberList && spec.MemberList != nil {
		switch spec.MemberList.InstanceAddrType {
		case lokiv1.InstanceAddrPodIP:
			instanceAddr = gossipInstanceAddrEnvVarTemplate
		case lokiv1.InstanceAddrDefault:
			// Do nothing use loki defaults
		default:
			// Do nothing use loki defaults
		}

		// Always default to use the pod IP address when IPv6 enabled to ensure:
		// - On Single Stack IPv6: Skip interface checking
		// - On Dual Stack IPv4/6: Eliminate duplicate memberlist node registration
		if spec.MemberList.EnableIPv6 {
			enableIPv6 = true
			instanceAddr = gossipInstanceAddrEnvVarTemplate
		}
	}

	return config.GossipRing{
		EnableIPv6:                     enableIPv6,
		InstanceAddr:                   instanceAddr,
		InstancePort:                   grpcPort,
		BindPort:                       gossipPort,
		MembersDiscoveryAddr:           fqdn(BuildLokiGossipRingService(stackName).GetName(), stackNs),
		EnableInstanceAvailabilityZone: replication != nil && len(replication.Zones) > 0,
	}
}

func remoteWriteConfig(s *lokiv1.RemoteWriteSpec, rs *RulerSecret) *config.RemoteWriteConfig {
	if s == nil || rs == nil {
		return nil
	}

	c := &config.RemoteWriteConfig{
		Enabled:       s.Enabled,
		RefreshPeriod: string(s.RefreshPeriod),
	}

	if cls := s.ClientSpec; cls != nil {
		c.Client = &config.RemoteWriteClientConfig{
			Name:            cls.Name,
			URL:             cls.URL,
			RemoteTimeout:   string(cls.Timeout),
			ProxyURL:        cls.ProxyURL,
			Headers:         cls.AdditionalHeaders,
			FollowRedirects: cls.FollowRedirects,
		}

		switch cls.AuthorizationType {
		case lokiv1.BasicAuthorization:
			c.Client.BasicAuthUsername = rs.Username
			c.Client.BasicAuthPassword = rs.Password
		case lokiv1.BearerAuthorization:
			c.Client.BearerToken = rs.BearerToken
		}

		for _, cfg := range cls.RelabelConfigs {
			c.RelabelConfigs = append(c.RelabelConfigs, config.RelabelConfig{
				SourceLabels: cfg.SourceLabels,
				Separator:    cfg.Separator,
				TargetLabel:  cfg.TargetLabel,
				Regex:        cfg.Regex,
				Modulus:      cfg.Modulus,
				Replacement:  cfg.Replacement,
				Action:       string(cfg.Action),
			})
		}
	}

	if q := s.QueueSpec; q != nil {
		c.Queue = &config.RemoteWriteQueueConfig{
			Capacity:          q.Capacity,
			MaxShards:         q.MaxShards,
			MinShards:         q.MinShards,
			MaxSamplesPerSend: q.MaxSamplesPerSend,
			BatchSendDeadline: string(q.BatchSendDeadline),
			MinBackOffPeriod:  string(q.MinBackOffPeriod),
			MaxBackOffPeriod:  string(q.MaxBackOffPeriod),
		}
	}

	return c
}

var deleteWorkerCountMap = map[lokiv1.LokiStackSizeType]uint{
	lokiv1.SizeOneXDemo:       10,
	lokiv1.SizeOneXExtraSmall: 10,
	lokiv1.SizeOneXSmall:      150,
	lokiv1.SizeOneXMedium:     150,
}

func retentionConfig(ls *lokiv1.LokiStackSpec) config.RetentionOptions {
	if ls.Limits == nil {
		return config.RetentionOptions{}
	}

	globalRetention := ls.Limits.Global != nil && ls.Limits.Global.Retention != nil
	tenantRetention := false
	for _, t := range ls.Limits.Tenants {
		if t.Retention != nil {
			tenantRetention = true
			break
		}
	}

	if !globalRetention && !tenantRetention {
		return config.RetentionOptions{}
	}

	return config.RetentionOptions{
		Enabled:           true,
		DeleteWorkerCount: deleteWorkerCountMap[ls.Size],
	}
}

func defaultOTLPAttributeConfig(ts *lokiv1.TenantsSpec) config.OTLPAttributeConfig {
	if ts == nil || ts.Mode != lokiv1.OpenshiftLogging {
		return config.OTLPAttributeConfig{}
	}

	// TODO decide which of these can be disabled by using "disableRecommendedAttributes"
	// TODO decide whether we want to split the default configuration by tenant
	result := config.OTLPAttributeConfig{
		DefaultIndexLabels: []string{
			"openshift.cluster.uid",
			"openshift.log.source",
			"log_source",
			"openshift.log.type",
			"log_type",

			"k8s.node.name",
			"k8s.node.uid",
			"k8s.namespace.name",
			"kubernetes.namespace_name",
			"k8s.container.name",
			"kubernetes.container_name",
			"k8s.pod.name",
			"k8s.pod.uid",
			"kubernetes.pod_name",
		},
		Global: &config.OTLPTenantAttributeConfig{
			ResourceAttributes: []config.OTLPAttribute{
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Regex:  "openshift\\.labels\\..+",
				},
				{
					Action: config.OTLPAttributeActionMetadata,
					Regex:  "k8s\\.pod\\.labels\\..+",
				},
				{
					Action: config.OTLPAttributeActionMetadata,
					Names: []string{
						"k8s.cronjob.name",
						"k8s.daemonset.name",
						"k8s.deployment.name",
						"k8s.job.name",
						"k8s.replicaset.name",
						"k8s.statefulset.name",
						"process.executable.name",
						"process.executable.path",
						"process.command_line",
						"process.pid",
						"service.name",
					},
				},
			},
			LogAttributes: []config.OTLPAttribute{
				{
					Action: config.OTLPAttributeActionMetadata,
					Names: []string{
						"log.iostream",
						"k8s.event.level",
						"k8s.event.stage",
						"k8s.event.user_agent",
						"k8s.event.request.uri",
						"k8s.event.response.code",
						"k8s.event.object_ref.resource",
						"k8s.event.object_ref.name",
						"k8s.event.object_ref.api.group",
						"k8s.event.object_ref.api.version",
						"k8s.user.username",
						"k8s.user.groups",
					},
				},
				{
					Action: config.OTLPAttributeActionMetadata,
					Regex:  "k8s\\.event\\.annotations\\..+",
				},
				{
					Action: config.OTLPAttributeActionMetadata,
					Regex:  "systemd\\.t\\..+",
				},
				{
					Action: config.OTLPAttributeActionMetadata,
					Regex:  "systemd\\.u\\..+",
				},
			},
		},
	}

	return result
}

func otlpAttributeConfig(ls *lokiv1.LokiStackSpec) config.OTLPAttributeConfig {
	// TODO provide default stream labels
	result := defaultOTLPAttributeConfig(ls.Tenants)

	if ls.Limits != nil {
		if ls.Limits.Global != nil && ls.Limits.Global.OTLP != nil {
			globalOTLP := ls.Limits.Global.OTLP

			if globalOTLP.StreamLabels != nil {
				regularExpressions := []string{}
				for _, attr := range globalOTLP.StreamLabels.ResourceAttributes {
					if attr.Regex {
						regularExpressions = append(regularExpressions, attr.Name)
						continue
					}

					result.DefaultIndexLabels = append(result.DefaultIndexLabels, attr.Name)
				}

				if len(regularExpressions) > 0 {
					result.Global = &config.OTLPTenantAttributeConfig{}

					for _, re := range regularExpressions {
						result.Global.ResourceAttributes = append(result.Global.ResourceAttributes, config.OTLPAttribute{
							Action: config.OTLPAttributeActionStreamLabel,
							Regex:  re,
						})
					}
				}
			}

			if structuredMetadata := globalOTLP.StructuredMetadata; structuredMetadata != nil {
				if result.Global == nil {
					result.Global = &config.OTLPTenantAttributeConfig{}
				}

				if resAttr := structuredMetadata.ResourceAttributes; len(resAttr) > 0 {
					regularExpressions, names := collectAttributes(resAttr)
					result.Global.ResourceAttributes = append(result.Global.ResourceAttributes, config.OTLPAttribute{
						Action: config.OTLPAttributeActionMetadata,
						Names:  names,
					})

					for _, re := range regularExpressions {
						result.Global.ResourceAttributes = append(result.Global.ResourceAttributes, config.OTLPAttribute{
							Action: config.OTLPAttributeActionMetadata,
							Regex:  re,
						})
					}
				}

				if scopeAttr := structuredMetadata.ScopeAttributes; len(scopeAttr) > 0 {
					regularExpressions, names := collectAttributes(scopeAttr)
					result.Global.ScopeAttributes = append(result.Global.ScopeAttributes, config.OTLPAttribute{
						Action: config.OTLPAttributeActionMetadata,
						Names:  names,
					})

					for _, re := range regularExpressions {
						result.Global.ScopeAttributes = append(result.Global.ScopeAttributes, config.OTLPAttribute{
							Action: config.OTLPAttributeActionMetadata,
							Regex:  re,
						})
					}
				}

				if logAttr := structuredMetadata.LogAttributes; len(logAttr) > 0 {
					regularExpressions, names := collectAttributes(logAttr)
					result.Global.LogAttributes = append(result.Global.LogAttributes, config.OTLPAttribute{
						Action: config.OTLPAttributeActionMetadata,
						Names:  names,
					})

					for _, re := range regularExpressions {
						result.Global.LogAttributes = append(result.Global.LogAttributes, config.OTLPAttribute{
							Action: config.OTLPAttributeActionMetadata,
							Regex:  re,
						})
					}
				}
			}
		}

		for tenant, tenantLimits := range ls.Limits.Tenants {
			if tenantLimits.OTLP != nil {
				tenantOTLP := tenantLimits.OTLP
				tenantResult := &config.OTLPTenantAttributeConfig{
					IgnoreGlobalStreamLabels: tenantOTLP.IgnoreGlobalStreamLabels,
				}

				// TODO stream labels and metadata for tenant

				result.Tenants[tenant] = tenantResult
			}
		}
	}

	return result
}

func collectAttributes(attrs []lokiv1.OTLPAttributeReference) (regularExpressions, names []string) {
	for _, attr := range attrs {
		if attr.Regex {
			regularExpressions = append(regularExpressions, attr.Name)
			continue
		}

		names = append(names, attr.Name)
	}

	return regularExpressions, names
}
