package manifests

import (
	"github.com/ViaQ/logerr/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildAll builds all manifests required to run a Loki Stack
// TODO add options parameter to enable resource sizing, and other configurations
func BuildAll(stackName, namespace string) ([]client.Object, error) {
	res := make([]client.Object, 0)

	log.Info("building configmap")
	cm, err := LokiConfigMap(stackName, namespace)
	if err != nil {
		return nil, err
	}
	res = append(res, cm)

	log.Info("building distributor deployment")
	res = append(res, DistributorDeployment(stackName))

	log.Info("building distributor services")
	for _, svc := range DistributorServices(stackName) {
		res = append(res, svc)
	}

	log.Info("building ingester deployment")
	res = append(res, IngesterDeployment(stackName))

	log.Info("building ingester services")
	for _, svc := range IngesterServices(stackName) {
		res = append(res, svc)
	}

	return res, nil
}
