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

// BuildIngester builds the k8s objects required to run Loki Ingester
func BuildIngester(opts Options) []client.Object {
	return []client.Object{
		NewIngesterStatefulSet(opts),
		NewIngesterGRPCService(opts),
		NewIngesterHTTPService(opts),
		NewIngesterServiceMonitor(opts.Name, opts.Namespace),
	}
}

// NewIngesterStatefulSet creates a deployment object for an ingester
func NewIngesterStatefulSet(opt Options) *appsv1.StatefulSet {
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
				Name:  "loki-ingester",
				Resources: corev1.ResourceRequirements{
					Limits:   opt.ResourceRequirements.Ingester.Limits,
					Requests: opt.ResourceRequirements.Ingester.Requests,
				},
				Args: []string{
					"-target=ingester",
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

	if opt.Stack.Template != nil && opt.Stack.Template.Ingester != nil {
		podSpec.Tolerations = opt.Stack.Template.Ingester.Tolerations
		podSpec.NodeSelector = opt.Stack.Template.Ingester.NodeSelector
	}

	l := ComponentLabels(LabelIngesterComponent, opt.Name)
	a := commonAnnotations(opt.ConfigSHA1)

	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   IngesterName(opt.Name),
			Labels: l,
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy:  appsv1.OrderedReadyPodManagement,
			RevisionHistoryLimit: pointer.Int32Ptr(10),
			Replicas:             pointer.Int32Ptr(opt.Stack.Template.Ingester.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("loki-ingester-%s", opt.Name),
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
								corev1.ResourceStorage: opt.ResourceRequirements.Ingester.PVCSize,
							},
						},
						StorageClassName: pointer.StringPtr(opt.Stack.StorageClassName),
					},
				},
			},
		},
	}
}

// NewIngesterGRPCService creates a k8s service for the ingester GRPC endpoint
func NewIngesterGRPCService(opt Options) *corev1.Service {
	l := ComponentLabels("ingester", opt.Name)
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceNameIngesterGRPC(opt.Name),
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

// NewIngesterHTTPService creates a k8s service for the ingester HTTP endpoint
func NewIngesterHTTPService(opt Options) *corev1.Service {
	l := ComponentLabels("ingester", opt.Name)
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceNameIngesterHTTP(opt.Name),
			Labels: l,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "metrics",
					Port: httpPort,
				},
			},
			Selector: l,
		},
	}
}

// NewIngesterServiceMonitor creates a k8s service monitor for the ingester component
func NewIngesterServiceMonitor(stackName, namespace string) *monitoringv1.ServiceMonitor {
	l := ComponentLabels(LabelIngesterComponent, stackName)

	serviceMonitorName := fmt.Sprintf("monitor-%s", IngesterName(stackName))
	serviceName := serviceNameIngesterHTTP(stackName)
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
