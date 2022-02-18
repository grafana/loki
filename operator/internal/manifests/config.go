package manifests

import (
	"crypto/sha1"
	"fmt"

	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LokiConfigMap creates the single configmap containing the loki configuration for the whole cluster
func LokiConfigMap(opt Options) (*corev1.ConfigMap, string, error) {
	cfg := ConfigOptions(opt)
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
	var azureStorage *config.AzureObjectStorage
	var gcsStorage *config.GCSObjectStorage
	var s3Storage *config.S3ObjectStorage
	var swiftStorage *config.SwiftObjectStorage

	if opt.ObjectStorage.Azure != nil {
		azureStorage = &config.AzureObjectStorage{
			Env:         opt.ObjectStorage.Azure.Env,
			Container:   opt.ObjectStorage.Azure.Container,
			AccountName: opt.ObjectStorage.Azure.AccountName,
			AccountKey:  opt.ObjectStorage.Azure.AccountKey,
		}
	} else if opt.ObjectStorage.GCS != nil {
		gcsStorage = &config.GCSObjectStorage{
			Bucket: opt.ObjectStorage.GCS.Bucket,
		}
	} else if opt.ObjectStorage.S3 != nil {
		s3Storage = &config.S3ObjectStorage{
			Endpoint:        opt.ObjectStorage.S3.Endpoint,
			Region:          opt.ObjectStorage.S3.Region,
			Buckets:         opt.ObjectStorage.S3.Buckets,
			AccessKeyID:     opt.ObjectStorage.S3.AccessKeyID,
			AccessKeySecret: opt.ObjectStorage.S3.AccessKeySecret,
		}
	} else if opt.ObjectStorage.Swift != nil {
		swiftStorage = &config.SwiftObjectStorage{
			AuthURL:           opt.ObjectStorage.Swift.AuthURL,
			Username:          opt.ObjectStorage.Swift.Username,
			UserDomainName:    opt.ObjectStorage.Swift.UserDomainName,
			UserDomainID:      opt.ObjectStorage.Swift.UserDomainID,
			UserID:            opt.ObjectStorage.Swift.UserID,
			Password:          opt.ObjectStorage.Swift.Password,
			DomainID:          opt.ObjectStorage.Swift.DomainID,
			DomainName:        opt.ObjectStorage.Swift.DomainName,
			ProjectID:         opt.ObjectStorage.Swift.ProjectID,
			ProjectName:       opt.ObjectStorage.Swift.ProjectName,
			ProjectDomainID:   opt.ObjectStorage.Swift.ProjectDomainID,
			ProjectDomainName: opt.ObjectStorage.Swift.ProjectDomainName,
			Region:            opt.ObjectStorage.Swift.Region,
			Container:         opt.ObjectStorage.Swift.Container,
		}
	}

	return config.Options{
		Stack:     opt.Stack,
		Namespace: opt.Namespace,
		Name:      opt.Name,
		FrontendWorker: config.Address{
			FQDN: fqdn(NewQueryFrontendGRPCService(opt).GetName(), opt.Namespace),
			Port: grpcPort,
		},
		GossipRing: config.Address{
			FQDN: fqdn(BuildLokiGossipRingService(opt.Name).GetName(), opt.Namespace),
			Port: gossipPort,
		},
		Querier: config.Address{
			FQDN: fqdn(NewQuerierHTTPService(opt).GetName(), opt.Namespace),
			Port: httpPort,
		},
		IndexGateway: config.Address{
			FQDN: fqdn(NewIndexGatewayGRPCService(opt).GetName(), opt.Namespace),
			Port: grpcPort,
		},
		StorageDirectory:   dataDirectory,
		AzureObjectStorage: azureStorage,
		GCSObjectStorage:   gcsStorage,
		S3ObjectStorage:    s3Storage,
		SwiftObjectStorage: swiftStorage,
		QueryParallelism: config.Parallelism{
			QuerierCPULimits:      opt.ResourceRequirements.Querier.Requests.Cpu().Value(),
			QueryFrontendReplicas: opt.Stack.Template.QueryFrontend.Replicas,
		},
		WriteAheadLog: config.WriteAheadLog{
			Directory:             walDirectory,
			IngesterMemoryRequest: opt.ResourceRequirements.Ingester.Requests.Memory().Value(),
		},
	}
}

func lokiConfigMapName(stackName string) string {
	return fmt.Sprintf("loki-config-%s", stackName)
}
