package e2edb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/e2e"
	"github.com/grafana/e2e/images"
)

const (
	DexClientID     = "kafka-client"
	DexClientSecret = "kafka-client-secret" //nolint:gosec
	DexUserEmail    = "admin@example.com"
	DexUserPassword = "password" //nolint:gosec
	// bcrypt hash of DexUserPassword ("password"), cost 10.
	dexUserPasswordHash = "$2a$10$ALZkboQwQZ9dSjedVp2zWuMGxv3H3NtrMZ6DoMYo69rs9ePcckDZS" //nolint:gosec
	dexUserID           = "08a8684b-db88-4b73-90a9-3cd1661f5466"
)

// DexService represents a Dex OIDC identity provider for e2e tests.
type DexService struct {
	*e2e.HTTPService
}

// NewDex creates a new Dex service configured for password-grant OAuth2 flows.
// Dex is configured with a static client and a static user so that tests can
// obtain JWT tokens via the resource-owner password grant.
func NewDex() *DexService {
	return &DexService{
		HTTPService: e2e.NewHTTPService(
			"dex",
			images.Dex,
			// Bypass the default gomplate-based entrypoint to avoid template
			// interpretation of '$' characters in the bcrypt password hash.
			e2e.NewCommandWithoutEntrypoint(
				"/usr/local/bin/dex",
				"serve",
				filepath.Join(e2e.ContainerSharedDir, "dex.yaml"),
			),
			e2e.NewHTTPReadinessProbe(5556, "/dex/.well-known/openid-configuration", 200, 200),
			5556,
		),
	}
}

func (s *DexService) Start(networkName, sharedDir string) error {
	// The issuer URL uses the Docker hostname which is the service name.
	// Other containers on the same Docker network can reach Dex at this address.
	config := fmt.Sprintf(`issuer: http://dex:5556/dex

storage:
  type: memory

web:
  http: 0.0.0.0:5556

enablePasswordDB: true

staticClients:
  - id: %s
    secret: %s
    name: Kafka Client
    redirectURIs:
      - http://localhost/callback

staticPasswords:
  - email: %q
    hash: %q
    username: "admin"
    userID: %q

oauth2:
  passwordConnector: local
  # The password grant is required for non-interactive token retrieval in tests.
  responseTypes: ["code", "token", "id_token"]
`, DexClientID, DexClientSecret, DexUserEmail, dexUserPasswordHash, dexUserID)

	configPath := filepath.Join(sharedDir, "dex.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("writing dex config: %w", err)
	}

	return s.HTTPService.Start(networkName, sharedDir)
}

func (s *DexService) FetchToken() (string, error) {
	data := url.Values{
		"grant_type":    {"password"},
		"scope":         {"openid"},
		"username":      {DexUserEmail},
		"password":      {DexUserPassword},
		"client_id":     {DexClientID},
		"client_secret": {DexClientSecret},
	}

	resp, err := http.Post(
		fmt.Sprintf("http://%s/dex/token", s.HTTPEndpoint()),
		"application/x-www-form-urlencoded",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("POST to token endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("unmarshaling token response: %w", err)
	}

	if tokenResp.IDToken == "" {
		return "", fmt.Errorf("id_token not found in response: %s", string(body))
	}

	return tokenResp.IDToken, nil
}
