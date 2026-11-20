package e2edb

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/grafana/e2e"
	"github.com/grafana/e2e/images"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

type KafkaService struct {
	*e2e.HTTPService
	cfg KafkaConfig
	*kafkaTLSCA
}

// KafkaOption customizes the KafkaConfig used to create a Kafka service.
type KafkaOption func(*KafkaConfig)

// WithKafkaName overrides the Kafka service name and hostname, which default to "kafka". Use it to
// run more than one Kafka cluster within the same network.
func WithKafkaName(name string) KafkaOption {
	return func(c *KafkaConfig) { c.name = name }
}

func NewKafka(opts ...KafkaOption) *KafkaService {
	cfg := KafkaConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg.New()
}

type KafkaConfig struct {
	AuthMode    KafkaAuthMode
	DexEndpoint string

	// name is the Kafka service name and hostname. An empty value defaults to "kafka".
	name string
}

// serviceName returns the Kafka service name, defaulting to "kafka".
func (c KafkaConfig) serviceName() string {
	if c.name == "" {
		return "kafka"
	}
	return c.name
}

func (c KafkaConfig) ports() (clientPort, readinessPort int) {
	clientPort = 9092
	readinessPort = clientPort
	if c.AuthMode != KafkaAuthNone {
		// Use a separate PLAINTEXT listener for readiness probes since the main
		// listener requires authentication.
		readinessPort = 9093
	}
	return clientPort, readinessPort
}

func (c KafkaConfig) New() *KafkaService {
	clientPort, readinessPort := c.ports()
	var otherPorts []int
	if readinessPort != clientPort {
		otherPorts = []int{readinessPort}
	}

	return &KafkaService{
		HTTPService: e2e.NewHTTPService(
			c.serviceName(),
			images.Kafka,
			nil, // No custom command.
			c.NewReadinessProbe(),
			clientPort,
			otherPorts...,
		),
		cfg: c,
	}
}

type KafkaAuthMode int

const (
	KafkaAuthNone KafkaAuthMode = iota
	KafkaAuthSASLPlain
	KafkaAuthSASLScramSHA256
	KafkaAuthSASLScramSHA512
	KafkaAuthSASLOAuthToken
	KafkaAuthSASLOAuthTokenFile
	KafkaAuthMTLS
)

const (
	KafkaSASLUsername = "kafkauser"
	KafkaSASLPassword = "kafkapassword"

	scramIterations = 4096
)

func (s *KafkaService) Start(networkName, sharedDir string) (err error) {
	// brokerHost is the broker's address within the network. The controller quorum voter uses the
	// container hostname (the service name), which resolves to the broker itself.
	name := s.cfg.serviceName()
	brokerHost := fmt.Sprintf("%s-%s", networkName, name)

	vars := map[string]string{
		// Configure Kafka to run in KRaft mode (without Zookeeper).
		"CLUSTER_ID":                      "NqnEdODVKkiLTfJvqd1uqQ==", // A random ID (16 bytes of a base64-encoded UUID).
		"KAFKA_BROKER_ID":                 "1",
		"KAFKA_NODE_ID":                   "1",
		"KAFKA_PROCESS_ROLES":             "broker,controller",
		"KAFKA_CONTROLLER_QUORUM_VOTERS":  fmt.Sprintf("1@%s:29093", name),
		"KAFKA_CONTROLLER_LISTENER_NAMES": "CONTROLLER",

		// RF=1.
		"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":         "1",
		"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":            "1",
		"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1",
		"KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS":         "0",

		// Enough partitions for integration tests.
		"KAFKA_NUM_PARTITIONS": "3",

		"LOG4J_ROOT_LOGLEVEL": "WARN",
	}

	switch s.cfg.AuthMode {
	case KafkaAuthNone:
		vars["KAFKA_LISTENERS"] = "PLAINTEXT://0.0.0.0:9092,CONTROLLER://0.0.0.0:29093,PLAINTEXT_HOST://localhost:29092"
		vars["KAFKA_ADVERTISED_LISTENERS"] = fmt.Sprintf("PLAINTEXT://%s:9092,PLAINTEXT_HOST://localhost:29092", brokerHost)
		vars["KAFKA_LISTENER_SECURITY_PROTOCOL_MAP"] = "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT,PLAINTEXT_HOST:PLAINTEXT"
		vars["KAFKA_INTER_BROKER_LISTENER_NAME"] = "PLAINTEXT"

	case KafkaAuthSASLPlain:
		vars["KAFKA_LISTENERS"] = "SASLPLAIN://0.0.0.0:9092,CONTROLLER://0.0.0.0:29093,PLAINTEXT://0.0.0.0:9093"
		vars["KAFKA_ADVERTISED_LISTENERS"] = fmt.Sprintf("SASLPLAIN://%s:9092,PLAINTEXT://%s:9093", brokerHost, brokerHost)
		vars["KAFKA_LISTENER_SECURITY_PROTOCOL_MAP"] = "CONTROLLER:PLAINTEXT,SASLPLAIN:SASL_PLAINTEXT,PLAINTEXT:PLAINTEXT"
		vars["KAFKA_INTER_BROKER_LISTENER_NAME"] = "PLAINTEXT"
		vars["KAFKA_SASL_ENABLED_MECHANISMS"] = "PLAIN"
		vars["KAFKA_LISTENER_NAME_SASLPLAIN_PLAIN_SASL_JAAS_CONFIG"] = fmt.Sprintf(
			`org.apache.kafka.common.security.plain.PlainLoginModule required user_%s="%s";`,
			KafkaSASLUsername, KafkaSASLPassword,
		)

	case KafkaAuthSASLScramSHA256:
		vars["KAFKA_LISTENERS"] = "SASLSCRAM://0.0.0.0:9092,CONTROLLER://0.0.0.0:29093,PLAINTEXT://0.0.0.0:9093"
		vars["KAFKA_ADVERTISED_LISTENERS"] = fmt.Sprintf("SASLSCRAM://%s:9092,PLAINTEXT://%s:9093", brokerHost, brokerHost)
		vars["KAFKA_LISTENER_SECURITY_PROTOCOL_MAP"] = "CONTROLLER:PLAINTEXT,SASLSCRAM:SASL_PLAINTEXT,PLAINTEXT:PLAINTEXT"
		vars["KAFKA_INTER_BROKER_LISTENER_NAME"] = "PLAINTEXT"
		vars["KAFKA_SASL_ENABLED_MECHANISMS"] = "SCRAM-SHA-256"

	case KafkaAuthSASLScramSHA512:
		vars["KAFKA_LISTENERS"] = "SASLSCRAM://0.0.0.0:9092,CONTROLLER://0.0.0.0:29093,PLAINTEXT://0.0.0.0:9093"
		vars["KAFKA_ADVERTISED_LISTENERS"] = fmt.Sprintf("SASLSCRAM://%s:9092,PLAINTEXT://%s:9093", brokerHost, brokerHost)
		vars["KAFKA_LISTENER_SECURITY_PROTOCOL_MAP"] = "CONTROLLER:PLAINTEXT,SASLSCRAM:SASL_PLAINTEXT,PLAINTEXT:PLAINTEXT"
		vars["KAFKA_INTER_BROKER_LISTENER_NAME"] = "PLAINTEXT"
		vars["KAFKA_SASL_ENABLED_MECHANISMS"] = "SCRAM-SHA-512"

	case KafkaAuthSASLOAuthToken, KafkaAuthSASLOAuthTokenFile:
		vars["KAFKA_LISTENERS"] = "SASLOAUTH://0.0.0.0:9092,CONTROLLER://0.0.0.0:29093,PLAINTEXT://0.0.0.0:9093"
		vars["KAFKA_ADVERTISED_LISTENERS"] = fmt.Sprintf("SASLOAUTH://%s:9092,PLAINTEXT://%s:9093", brokerHost, brokerHost)
		vars["KAFKA_LISTENER_SECURITY_PROTOCOL_MAP"] = "CONTROLLER:PLAINTEXT,SASLOAUTH:SASL_PLAINTEXT,PLAINTEXT:PLAINTEXT"
		vars["KAFKA_INTER_BROKER_LISTENER_NAME"] = "PLAINTEXT"
		vars["KAFKA_SASL_ENABLED_MECHANISMS"] = "OAUTHBEARER"
		vars["KAFKA_LISTENER_NAME_SASLOAUTH_OAUTHBEARER_SASL_JAAS_CONFIG"] = "org.apache.kafka.common.security.oauthbearer.OAuthBearerLoginModule required;"
		vars["KAFKA_LISTENER_NAME_SASLOAUTH_OAUTHBEARER_SASL_SERVER_CALLBACK_HANDLER_CLASS"] = "org.apache.kafka.common.security.oauthbearer.OAuthBearerValidatorCallbackHandler"
		vars["KAFKA_LISTENER_NAME_SASLOAUTH_SASL_OAUTHBEARER_JWKS_ENDPOINT_URL"] = fmt.Sprintf("http://%s/dex/keys", s.cfg.DexEndpoint)
		vars["KAFKA_LISTENER_NAME_SASLOAUTH_SASL_OAUTHBEARER_EXPECTED_AUDIENCE"] = DexClientID
		// Kafka 4.0+ requires JWKS URLs to be explicitly allowed via this JVM system property.
		vars["KAFKA_OPTS"] = fmt.Sprintf("-Dorg.apache.kafka.sasl.oauthbearer.allowed.urls=http://%s/dex/keys", s.cfg.DexEndpoint)

	case KafkaAuthMTLS:
		// Generate a CA and a server certificate, write them as PEM files
		// to the shared directory, then convert them to JKS keystores via
		// a one-off Docker container (using openssl + keytool from the
		// Kafka image). The resulting JKS files are mounted into the
		// Kafka container at /etc/kafka/secrets, following the official
		// Kafka Docker SSL example:
		// https://github.com/apache/kafka/blob/trunk/docker/examples/README.md#running-in-ssl-mode

		if err := s.generateTLSCerts(networkName, sharedDir); err != nil {
			return fmt.Errorf("generating TLS certificates: %w", err)
		}
		if err := convertPEMToJKS(sharedDir); err != nil {
			return err
		}
		s.SetVolumes(map[string]string{
			filepath.Join(sharedDir, kafkaTLSSecretsDir): "/etc/kafka/secrets",
		})

		vars["KAFKA_LISTENERS"] = "SSL://0.0.0.0:9092,CONTROLLER://0.0.0.0:29093,PLAINTEXT://0.0.0.0:9093"
		vars["KAFKA_ADVERTISED_LISTENERS"] = fmt.Sprintf("SSL://%s:9092,PLAINTEXT://%s:9093", brokerHost, brokerHost)
		vars["KAFKA_LISTENER_SECURITY_PROTOCOL_MAP"] = "CONTROLLER:PLAINTEXT,SSL:SSL,PLAINTEXT:PLAINTEXT"
		vars["KAFKA_INTER_BROKER_LISTENER_NAME"] = "PLAINTEXT"
		vars["KAFKA_SSL_CLIENT_AUTH"] = "required"
		vars["KAFKA_SSL_KEYSTORE_FILENAME"] = "kafka.keystore.jks"
		vars["KAFKA_SSL_KEYSTORE_CREDENTIALS"] = "keystore_creds"
		vars["KAFKA_SSL_KEY_CREDENTIALS"] = "key_creds"
		vars["KAFKA_SSL_TRUSTSTORE_FILENAME"] = "kafka.truststore.jks"
		vars["KAFKA_SSL_TRUSTSTORE_CREDENTIALS"] = "truststore_creds"
		// Disable hostname verification for test certificates.
		vars["KAFKA_SSL_ENDPOINT_IDENTIFICATION_ALGORITHM"] = ""
	}

	// SCRAM config cannot be passed as env vars because it contains hyphens,
	// which the apache/kafka image can't handle.
	if s.cfg.AuthMode == KafkaAuthSASLScramSHA256 || s.cfg.AuthMode == KafkaAuthSASLScramSHA512 {
		jaasContent := fmt.Sprintf(
			"saslscram.KafkaServer {\n    org.apache.kafka.common.security.scram.ScramLoginModule required username=%q password=%q;\n};\n",
			KafkaSASLUsername, KafkaSASLPassword,
		)
		jaasPath := filepath.Join(sharedDir, "jaas.conf")
		if err := os.WriteFile(jaasPath, []byte(jaasContent), 0644); err != nil {
			return fmt.Errorf("writing kafka JAAS config: %w", err)
		}

		vars["KAFKA_OPTS"] = "-Djava.security.auth.login.config=" + filepath.Join(e2e.ContainerSharedDir, "jaas.conf")
	}

	// Configures Kafka right before starting it so that we have the networkName to correctly compute
	// the advertised host.
	s.SetEnvVars(vars)

	return s.HTTPService.Start(networkName, sharedDir)
}

// KafkaReadinessProbe checks readiness by ensure a Kafka broker is up and running.
type KafkaReadinessProbe struct {
	cfg KafkaConfig
}

func (c KafkaConfig) NewReadinessProbe() *KafkaReadinessProbe {
	return &KafkaReadinessProbe{cfg: c}
}

func (p *KafkaReadinessProbe) Ready(service *e2e.ConcreteService) (err error) {
	const timeout = 5 * time.Second

	_, readinessPort := p.cfg.ports()

	endpoint := service.Endpoint(readinessPort)
	switch endpoint {
	case "":
		return fmt.Errorf("cannot get service endpoint for port %d", readinessPort)
	case "stopped":
		return errors.New("service has stopped")
	}

	// Connect to the readiness port, which is always PLAINTEXT (no auth
	// required). For non-auth modes, the readiness port is the same as the
	// client port; for auth modes, it's a separate PLAINTEXT listener.
	opts := []kgo.Opt{kgo.SeedBrokers(endpoint), kgo.DialTimeout(timeout)}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return err
	}

	// Ensure we close the client once done.
	defer client.Close()

	admin := kadm.NewClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	_, err = admin.ApiVersions(ctx)
	if err != nil {
		return err
	}

	if err := p.createSCRAMCredentials(service); err != nil {
		return fmt.Errorf("creating SCRAM credentials: %w", err)
	}

	return nil
}

// createSCRAMCredentials creates SCRAM user credentials by running
// kafka-configs.sh inside the Kafka container. It is a no-op for non-SCRAM
// auth modes.
//
// We use Exec rather than the admin client API because the readiness probe
// runs on the host, and the admin client's AlterUserSCRAMs request requires
// talking to the controller, whose address is discovered via metadata. The
// metadata returns the Docker-internal advertised address which is not
// resolvable from the host.
func (p *KafkaReadinessProbe) createSCRAMCredentials(service *e2e.ConcreteService) error {
	if p.cfg.AuthMode != KafkaAuthSASLScramSHA256 && p.cfg.AuthMode != KafkaAuthSASLScramSHA512 {
		return nil
	}

	var mechanism string
	switch p.cfg.AuthMode {
	case KafkaAuthSASLScramSHA256:
		mechanism = "SCRAM-SHA-256"
	case KafkaAuthSASLScramSHA512:
		mechanism = "SCRAM-SHA-512"
	default:
		return fmt.Errorf("unexpected auth mode: %v", p.cfg.AuthMode)
	}

	_, readinessPort := p.cfg.ports()

	stdout, stderr, err := service.Exec(e2e.NewCommandWithoutEntrypoint(
		"/opt/kafka/bin/kafka-configs.sh",
		fmt.Sprintf("--bootstrap-server=localhost:%d", readinessPort),
		"--alter",
		fmt.Sprintf("--add-config=%s=[iterations=%d,password=%s]", mechanism, scramIterations, KafkaSASLPassword),
		"--entity-type=users",
		fmt.Sprintf("--entity-name=%s", KafkaSASLUsername),
	))
	if err != nil {
		return fmt.Errorf("kafka-configs.sh failed: %w; stdout: %s; stderr: %s", err, stdout, stderr)
	}
	return nil
}

const (
	// KafkaTLSCACertFile is the path (relative to the scenario's shared
	// directory) where the Kafka TLS CA certificate is written.
	KafkaTLSCACertFile = "kafka-tls/ca.crt"

	kafkaTLSDir        = "kafka-tls"
	kafkaTLSSecretsDir = "kafka-secrets"

	// kafkaSSLStorePassword is the password used for the generated JKS
	// keystore and truststore files. Since these are ephemeral test
	// artifacts, a simple well-known password is fine.
	kafkaSSLStorePassword = "changeit"
)

// generateTLSCerts creates a CA and a server certificate for the Kafka
// broker, writing PEM files to the kafka-tls/ subdirectory of the shared
// directory. Callers can then generate client certificates via WriteCertificate().
func (s *KafkaService) generateTLSCerts(networkName, sharedDir string) error {
	ca, err := newKafkaTLSCA()
	if err != nil {
		return err
	}
	s.kafkaTLSCA = ca

	tlsDir := filepath.Join(sharedDir, kafkaTLSDir)
	if err := os.MkdirAll(tlsDir, 0777); err != nil {
		return fmt.Errorf("creating TLS directory: %w", err)
	}

	if err := ca.writeCACertificate(filepath.Join(tlsDir, "ca.crt")); err != nil {
		return fmt.Errorf("writing CA certificate: %w", err)
	}

	name := s.cfg.serviceName()
	brokerHostname := fmt.Sprintf("%s-%s", networkName, name)
	serverKeyPath := filepath.Join(tlsDir, "server.key")
	if err := ca.WriteCertificate(
		&x509.Certificate{
			Subject:     pkix.Name{CommonName: name},
			DNSNames:    []string{brokerHostname},
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		},
		filepath.Join(tlsDir, "server.crt"),
		serverKeyPath,
	); err != nil {
		return fmt.Errorf("writing server certificate: %w", err)
	}

	// Make the key readable by the Docker container.
	if err := os.Chmod(serverKeyPath, 0644); err != nil {
		return fmt.Errorf("chmod server key: %w", err)
	}

	return nil
}

// convertPEMToJKS runs a one-off Docker container (using the Kafka image,
// which ships openssl and keytool) to convert the PEM certificates in
// kafka-tls/ into JKS keystores in kafka-secrets/. This is analogous to
// createSCRAMCredentials but runs before the Kafka container starts,
// because the JKS files must exist when Kafka boots.
func convertPEMToJKS(sharedDir string) error {
	secretsDir := filepath.Join(sharedDir, kafkaTLSSecretsDir)
	if err := os.MkdirAll(secretsDir, 0777); err != nil {
		return fmt.Errorf("creating secrets directory: %w", err)
	}

	script := fmt.Sprintf(`set -euo pipefail
STORE_PASS=%s
TLS_DIR=%s/%s
SECRETS_DIR=%s/%s

openssl pkcs12 -export \
    -in "$TLS_DIR/server.crt" \
    -inkey "$TLS_DIR/server.key" \
    -out /tmp/kafka.keystore.p12 \
    -name broker \
    -passout "pass:$STORE_PASS"

keytool -importkeystore \
    -srckeystore /tmp/kafka.keystore.p12 \
    -srcstoretype PKCS12 \
    -srcstorepass "$STORE_PASS" \
    -destkeystore "$SECRETS_DIR/kafka.keystore.jks" \
    -deststoretype JKS \
    -deststorepass "$STORE_PASS" \
    -noprompt

keytool -importcert \
    -file "$TLS_DIR/ca.crt" \
    -keystore "$SECRETS_DIR/kafka.truststore.jks" \
    -storepass "$STORE_PASS" \
    -alias ca \
    -noprompt

printf '%%s' "$STORE_PASS" > "$SECRETS_DIR/keystore_creds"
printf '%%s' "$STORE_PASS" > "$SECRETS_DIR/key_creds"
printf '%%s' "$STORE_PASS" > "$SECRETS_DIR/truststore_creds"

chmod -R a+r "$SECRETS_DIR"`,
		kafkaSSLStorePassword,
		e2e.ContainerSharedDir, kafkaTLSDir,
		e2e.ContainerSharedDir, kafkaTLSSecretsDir,
	)

	output, err := e2e.RunCommandAndGetOutput(
		"docker", "run", "--rm",
		"--user", "0:0",
		"-v", fmt.Sprintf("%s:%s:z", sharedDir, e2e.ContainerSharedDir),
		images.Kafka,
		"/bin/bash", "-c", script,
	)
	if err != nil {
		return fmt.Errorf("converting PEM to JKS via docker: %w\noutput: %s", err, output)
	}
	return nil
}

type kafkaTLSCA struct {
	cert    *x509.Certificate
	certDER []byte
	key     *ecdsa.PrivateKey
	serial  atomic.Int64
}

func newKafkaTLSCA() (*kafkaTLSCA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating CA key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Kafka e2e Test CA"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		return nil, fmt.Errorf("creating CA certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("parsing CA certificate: %w", err)
	}

	ca := &kafkaTLSCA{
		cert:    cert,
		certDER: certDER,
		key:     key,
	}
	ca.serial.Store(1) // first signed cert will get serial 2
	return ca, nil
}

// WriteCertificate generates a new key pair, signs a certificate with the CA,
// and writes the certificate and private key as PEM files. The private key is
// encoded in PKCS#8 format.
func (ca *kafkaTLSCA) WriteCertificate(template *x509.Certificate, certPath, keyPath string) error {
	if ca == nil {
		return errors.New("WriteCertificate requires KafkaConfig.AuthMode: KafkaAuthMTLS and calling Start first")
	}

	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generating key: %w", err)
	}

	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshaling private key: %w", err)
	}
	if err := writePEMFile(keyPath, "PRIVATE KEY", 0600, keyBytes); err != nil {
		return fmt.Errorf("writing key file: %w", err)
	}

	template.IsCA = false
	template.NotBefore = time.Now()
	if template.NotAfter.IsZero() {
		template.NotAfter = time.Now().Add(24 * time.Hour)
	}
	template.SerialNumber = big.NewInt(ca.serial.Add(1))

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.cert, key.Public(), ca.key)
	if err != nil {
		return fmt.Errorf("signing certificate: %w", err)
	}

	return writePEMFile(certPath, "CERTIFICATE", 0644, certDER)
}

func (ca *kafkaTLSCA) writeCACertificate(path string) error {
	return writePEMFile(path, "CERTIFICATE", 0644, ca.certDER)
}

func writePEMFile(path string, pemType string, mode os.FileMode, derBytes []byte) error {
	data := pem.EncodeToMemory(&pem.Block{Type: pemType, Bytes: derBytes})
	return os.WriteFile(path, data, mode)
}
