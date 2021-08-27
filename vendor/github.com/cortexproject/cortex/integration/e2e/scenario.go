package e2e

import (
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"
)

const (
	ContainerSharedDir = "/shared"
)

type Service interface {
	Name() string
	Start(networkName, dir string) error
	WaitReady() error

	// It should be ok to Stop and Kill more than once, with next invokes being noop.
	Kill() error
	Stop() error
}

type Scenario struct {
	services []Service

	networkName string
	sharedDir   string
}

func NewScenario(networkName string) (*Scenario, error) {
	s := &Scenario{networkName: networkName}

	var err error
	s.sharedDir, err = GetTempDirectory()
	if err != nil {
		return nil, err
	}

	// Force a shutdown in order to cleanup from a spurious situation in case
	// the previous tests run didn't cleanup correctly.
	s.shutdown()

	// Setup the docker network.
	if out, err := RunCommandAndGetOutput("docker", "network", "create", networkName); err != nil {
		logger.Log(string(out))
		s.clean()
		return nil, errors.Wrapf(err, "create docker network '%s'", networkName)
	}

	return s, nil
}

// SharedDir returns the absolute path of the directory on the host that is shared with all services in docker.
func (s *Scenario) SharedDir() string {
	return s.sharedDir
}

// NetworkName returns the network name that scenario is responsible for.
func (s *Scenario) NetworkName() string {
	return s.networkName
}

func (s *Scenario) isRegistered(name string) bool {
	for _, service := range s.services {
		if service.Name() == name {
			return true
		}
	}
	return false
}

func (s *Scenario) StartAndWaitReady(services ...Service) error {
	if err := s.Start(services...); err != nil {
		return err
	}
	return s.WaitReady(services...)
}

func (s *Scenario) Start(services ...Service) error {
	for _, service := range services {
		logger.Log("Starting", service.Name())

		// Ensure another service with the same name doesn't exist.
		if s.isRegistered(service.Name()) {
			return fmt.Errorf("another service with the same name '%s' has already been started", service.Name())
		}

		// Start the service.
		if err := service.Start(s.networkName, s.SharedDir()); err != nil {
			return err
		}

		// Add to the list of services.
		s.services = append(s.services, service)
	}

	return nil
}

func (s *Scenario) Stop(services ...Service) error {
	for _, service := range services {
		if !s.isRegistered(service.Name()) {
			return fmt.Errorf("unable to stop service %s because it does not exist", service.Name())
		}
		if err := service.Stop(); err != nil {
			return err
		}

		// Remove the service from the list of services.
		for i, entry := range s.services {
			if entry.Name() == service.Name() {
				s.services = append(s.services[:i], s.services[i+1:]...)
				break
			}
		}
	}
	return nil
}

func (s *Scenario) WaitReady(services ...Service) error {
	for _, service := range services {
		if !s.isRegistered(service.Name()) {
			return fmt.Errorf("unable to wait for service %s because it does not exist", service.Name())
		}
		if err := service.WaitReady(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scenario) Close() {
	if s == nil {
		return
	}
	s.shutdown()
	s.clean()
}

// TODO(bwplotka): Add comments.
func (s *Scenario) clean() {
	if err := os.RemoveAll(s.sharedDir); err != nil {
		logger.Log("error while removing sharedDir", s.sharedDir, "err:", err)
	}
}

func (s *Scenario) shutdown() {
	// Kill the services in the opposite order.
	for i := len(s.services) - 1; i >= 0; i-- {
		if err := s.services[i].Kill(); err != nil {
			logger.Log("Unable to kill service", s.services[i].Name(), ":", err.Error())
		}
	}

	// Ensure there are no leftover containers.
	if out, err := RunCommandAndGetOutput(
		"docker",
		"ps",
		"-a",
		"--quiet",
		"--filter",
		fmt.Sprintf("network=%s", s.networkName),
	); err == nil {
		for _, containerID := range strings.Split(string(out), "\n") {
			containerID = strings.TrimSpace(containerID)
			if containerID == "" {
				continue
			}

			if out, err = RunCommandAndGetOutput("docker", "rm", "--force", containerID); err != nil {
				logger.Log(string(out))
				logger.Log("Unable to cleanup leftover container", containerID, ":", err.Error())
			}
		}
	} else {
		logger.Log(string(out))
		logger.Log("Unable to cleanup leftover containers:", err.Error())
	}

	// Teardown the docker network. In case the network does not exists (ie. this function
	// is called during the setup of the scenario) we skip the removal in order to not log
	// an error which may be misleading.
	if ok, err := existDockerNetwork(s.networkName); ok || err != nil {
		if out, err := RunCommandAndGetOutput("docker", "network", "rm", s.networkName); err != nil {
			logger.Log(string(out))
			logger.Log("Unable to remove docker network", s.networkName, ":", err.Error())
		}
	}
}

func existDockerNetwork(networkName string) (bool, error) {
	out, err := RunCommandAndGetOutput("docker", "network", "ls", "--quiet", "--filter", fmt.Sprintf("name=%s", networkName))
	if err != nil {
		logger.Log(string(out))
		logger.Log("Unable to check if docker network", networkName, "exists:", err.Error())
	}

	return strings.TrimSpace(string(out)) != "", nil
}
