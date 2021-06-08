package manifests

import (
	"fmt"
	"path"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/ViaQ/loki-operator/internal/manifests/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildQuerier returns a list of k8s objects for Loki Querier
func BuildQuerier(opt Options) []client.Object {
	return []client.Object{
		NewQuerierStatefulSet(opt),
		NewQuerierGRPCService(opt.Name),
		NewQuerierHTTPService(opt.Name),
		NewQuerierServiceMonitor(opt.Name, opt.Namespace),
	}
}

// NewQuerierStatefulSet creates a deployment object for a querier
func NewQuerierStatefulSet(opt Options) *appsv1.StatefulSet {
	podSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: configVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: lokiConfigMapName(opt.Name),
						},
					},
				},
			},
		},
		Containers: []corev1.Container{
			{
				Image: opt.Image,
				Name:  "loki-querier",
				Resources: corev1.ResourceRequirements{
					Limits:   opt.ResourceRequirements.Querier.Limits,
					Requests: opt.ResourceRequirements.Querier.Requests,
				},
				Args: []string{
					"-target=querier",
					fmt.Sprintf("-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiConfigFileName)),
					fmt.Sprintf("-runtime-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiRuntimeConfigFileName)),
				},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/ready",
							Port:   intstr.FromInt(httpPort),
							Scheme: corev1.URISchemeHTTP,
						},
					},
					InitialDelaySeconds: 15,
					TimeoutSeconds:      1,
				},
				LivenessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/metrics",
							Port:   intstr.FromInt(httpPort),
							Scheme: corev1.URISchemeHTTP,
						},
					},
					TimeoutSeconds:   2,
					PeriodSeconds:    30,
					FailureThreshold: 10,
				},
				Ports: []corev1.ContainerPort{
					{
						Name:          "metrics",
						ContainerPort: httpPort,
					},
					{
						Name:          "grpc",
						ContainerPort: grpcPort,
					},
					{
						Name:          "gossip-ring",
						ContainerPort: gossipPort,
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      configVolumeName,
						ReadOnly:  false,
						MountPath: config.LokiConfigMountDir,
					},
					{
						Name:      storageVolumeName,
						ReadOnly:  false,
						MountPath: dataDirectory,
					},
				},
			},
		},
	}

	if opt.Stack.Template != nil && opt.Stack.Template.Querier != nil {
		podSpec.Tolerations = opt.Stack.Template.Querier.Tolerations
		podSpec.NodeSelector = opt.Stack.Template.Querier.NodeSelector
	}

	l := ComponentLabels(LabelQuerierComponent, opt.Name)
	a := commonAnnotations(opt.ConfigSHA1)

	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   QuerierName(opt.Name),
			Labels: l,
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy:  appsv1.OrderedReadyPodManagement,
			RevisionHistoryLimit: pointer.Int32Ptr(10),
			Replicas:             pointer.Int32Ptr(opt.Stack.Template.Querier.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("loki-querier-%s", opt.Name),
					Labels:      labels.Merge(l, GossipLabels()),
					Annotations: a,
				},
				Spec: podSpec,
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Labels: l,
						Name:   storageVolumeName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							// TODO: should we verify that this is possible with the given storage class first?
							corev1.ReadWriteOnce,
						},
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: opt.ResourceRequirements.Querier.PVCSize,
							},
						},
						StorageClassName: pointer.StringPtr(opt.Stack.StorageClassName),
					},
				},
			},
		},
	}
}

// NewQuerierGRPCService creates a k8s service for the querier GRPC endpoint
func NewQuerierGRPCService(stackName string) *corev1.Service {
	l := ComponentLabels("querier", stackName)
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceNameQuerierGRPC(stackName),
			Labels: l,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{
					Name: "grpc",
					Port: grpcPort,
				},
			},
			Selector: l,
		},
	}
}

// NewQuerierHTTPService creates a k8s service for the querier HTTP endpoint
func NewQuerierHTTPService(stackName string) *corev1.Service {
	l := ComponentLabels("querier", stackName)
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceNameQuerierHTTP(stackName),
			Labels: l,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "http",
					Port: httpPort,
				},
			},
			Selector: l,
		},
	}
}

// NewQuerierServiceMonitor creates a k8s service monitor for the querier component
func NewQuerierServiceMonitor(stackName, namespace string) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelQuerierComponent, stackName)

	serviceMonitorName := fmt.Sprintf("monitor-%s", QuerierName(stackName))
	serviceName := serviceNameQuerierHTTP(stackName)
	lokiEndpoint := serviceMonitorLokiEndPoint(stackName, serviceName, namespace)

	return &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.ServiceMonitorsKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceMonitorName,
			Namespace: namespace,
			Labels:    l,
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			JobLabel:  labelJobComponent,
			Endpoints: []monitoringv1.Endpoint{lokiEndpoint},
			Selector: metav1.LabelSelector{
				MatchLabels: l,
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{namespace},
			},
		},
	}
}
