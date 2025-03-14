package manifests

import (
	"crypto/sha1"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
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
		DiscoverLogLevels:     discoverLogLevels(&opt.Stack),
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

func deleteWorkerCount(size lokiv1.LokiStackSizeType) uint {
	switch size {
	case lokiv1.SizeOneXSmall, lokiv1.SizeOneXMedium:
		return 150
	default:
		return 10
	}
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
		DeleteWorkerCount: deleteWorkerCount(ls.Size),
	}
}

func discoverLogLevels(ls *lokiv1.LokiStackSpec) bool {
	if ls.Tenants == nil {
		return true
	}

	if ls.Tenants.Mode == lokiv1.OpenshiftLogging ||
		ls.Tenants.Mode == lokiv1.OpenshiftNetwork {
		return false
	}

	return true
}
