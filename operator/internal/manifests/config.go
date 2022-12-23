package manifests

import (
	"crypto/sha1"
	"fmt"
	"strings"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		BinaryData: map[string][]byte{
			config.LokiConfigFileName:        c,
			config.LokiRuntimeConfigFileName: rc,
		},
	}, sha1C, nil
}

// ConfigOptions converts Options to config.Options
func ConfigOptions(opt Options) config.Options {
	rulerEnabled := opt.Stack.Rules != nil && opt.Stack.Rules.Enabled

	var (
		evalInterval, pollInterval string
		amConfig                   *config.AlertManagerConfig
		rwConfig                   *config.RemoteWriteConfig
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

	protocol := "http"
	if opt.Gates.HTTPEncryption {
		protocol = "https"
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
			FQDN:     fqdn(NewCompactorHTTPService(opt).GetName(), opt.Namespace),
			Port:     httpPort,
			Protocol: protocol,
		},
		FrontendWorker: config.Address{
			FQDN: fqdn(NewQueryFrontendGRPCService(opt).GetName(), opt.Namespace),
			Port: grpcPort,
		},
		GossipRing: config.Address{
			FQDN: fqdn(BuildLokiGossipRingService(opt.Name).GetName(), opt.Namespace),
			Port: gossipPort,
		},
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
		ObjectStorage:         opt.ObjectStorage,
		EnableRemoteReporting: opt.Gates.GrafanaLabsUsageReport,
		Ruler: config.Ruler{
			Enabled:               rulerEnabled,
			RulesStorageDirectory: rulesStorageDirectory,
			EvaluationInterval:    evalInterval,
			PollInterval:          pollInterval,
			AlertManager:          amConfig,
			RemoteWrite:           rwConfig,
		},
		Retention: retentionConfig(&opt.Stack),
	}
}

func alertManagerConfig(s *lokiv1beta1.AlertManagerSpec) *config.AlertManagerConfig {
	if s == nil {
		return nil
	}

	c := &config.AlertManagerConfig{
		ExternalURL:    s.ExternalURL,
		ExternalLabels: s.ExternalLabels,
		Hosts:          strings.Join(s.Endpoints, ","),
		EnableV2:       s.EnableV2,
	}

	if d := s.DiscoverySpec; d != nil {
		c.EnableDiscovery = d.EnableSRV
		c.RefreshInterval = string(d.RefreshInterval)
	}

	if n := s.NotificationQueueSpec; n != nil {
		c.QueueCapacity = n.Capacity
		c.Timeout = string(n.Timeout)
		c.ForOutageTolerance = string(n.ForOutageTolerance)
		c.ForGracePeriod = string(n.ForGracePeriod)
		c.ResendDelay = string(n.ResendDelay)
	}

	for _, cfg := range s.RelabelConfigs {
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

	return c
}

func remoteWriteConfig(s *lokiv1beta1.RemoteWriteSpec, rs *RulerSecret) *config.RemoteWriteConfig {
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
		case lokiv1beta1.BasicAuthorization:
			c.Client.BasicAuthUsername = rs.Username
			c.Client.BasicAuthPassword = rs.Password
		case lokiv1beta1.BearerAuthorization:
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
