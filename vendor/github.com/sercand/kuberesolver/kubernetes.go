package kuberesolver

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	serviceAccountToken  = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	serviceAccountCACert = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

// K8sClient is minimal kubernetes client interface
type K8sClient interface {
	Do(req *http.Request) (*http.Response, error)
	GetRequest(url string) (*http.Request, error)
	Host() string
}

type k8sClient struct {
	host       string
	token      string
	httpClient *http.Client
}

func (kc *k8sClient) GetRequest(url string) (*http.Request, error) {
	if !strings.HasPrefix(url, kc.host) {
		url = fmt.Sprintf("%s/%s", kc.host, url)
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if len(kc.token) > 0 {
		req.Header.Set("Authorization", "Bearer "+kc.token)
	}
	return req, nil
}

func (kc *k8sClient) Do(req *http.Request) (*http.Response, error) {
	return kc.httpClient.Do(req)
}

func (kc *k8sClient) Host() string {
	return kc.host
}

// NewInClusterK8sClient creates K8sClient if it is inside Kubernetes
func NewInClusterK8sClient() (K8sClient, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if len(host) == 0 || len(port) == 0 {
		return nil, fmt.Errorf("unable to load in-cluster configuration, KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT must be defined")
	}
	token, err := ioutil.ReadFile(serviceAccountToken)
	if err != nil {
		return nil, err
	}
	ca, err := ioutil.ReadFile(serviceAccountCACert)
	if err != nil {
		return nil, err
	}
	certPool := x509.NewCertPool()
	certPool.AppendCertsFromPEM(ca)
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS10,
		RootCAs:    certPool,
	}}
	httpClient := &http.Client{Transport: transport, Timeout: time.Nanosecond * 0}

	return &k8sClient{
		host:       "https://" + net.JoinHostPort(host, port),
		token:      string(token),
		httpClient: httpClient,
	}, nil
}

// NewInsecureK8sClient creates an insecure k8s client which is suitable
// to connect kubernetes api behind proxy
func NewInsecureK8sClient(apiURL string) K8sClient {
	return &k8sClient{
		host:       apiURL,
		httpClient: http.DefaultClient,
	}
}

func getEndpoints(client K8sClient, namespace, targetName string) (Endpoints, error) {
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/namespaces/%s/endpoints/%s",
		client.Host(), namespace, targetName))
	if err != nil {
		return Endpoints{}, err
	}
	req, err := client.GetRequest(u.String())
	if err != nil {
		return Endpoints{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Endpoints{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Endpoints{}, fmt.Errorf("invalid response code %d", resp.StatusCode)
	}
	result := Endpoints{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

func watchEndpoints(client K8sClient, namespace, targetName string) (watchInterface, error) {
	u, err := url.Parse(fmt.Sprintf("%s/api/v1/watch/namespaces/%s/endpoints/%s",
		client.Host(), namespace, targetName))
	if err != nil {
		return nil, err
	}
	req, err := client.GetRequest(u.String())
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, fmt.Errorf("invalid response code %d", resp.StatusCode)
	}
	return newStreamWatcher(resp.Body), nil
}
