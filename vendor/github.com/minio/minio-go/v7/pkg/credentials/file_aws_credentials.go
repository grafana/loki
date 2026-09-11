/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2017 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package credentials

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/ini.v1"
)

// ssoPortalRequestTimeout bounds the AWS SSO portal role-credentials request.
const ssoPortalRequestTimeout = time.Minute

// An externalProcessCredentials stores the output of a credential_process
type externalProcessCredentials struct {
	Version         int
	SessionToken    string
	AccessKeyID     string `json:"AccessKeyId"`
	SecretAccessKey string
	Expiration      time.Time
}

// An ssoCredentials stores the response of fetching role credentials for
// an AWS SSO role from the SSO portal.
type ssoCredentials struct {
	RoleCredentials ssoRoleCredentials `json:"roleCredentials"`
}

// An ssoRoleCredentials stores the role-specific credentials portion of
// an SSO role credentials response. Expiration is milliseconds since
// the Unix epoch.
type ssoRoleCredentials struct {
	AccessKeyID     string `json:"accessKeyId"`
	Expiration      int64  `json:"expiration"`
	SecretAccessKey string `json:"secretAccessKey"`
	SessionToken    string `json:"sessionToken"`
}

func (s ssoRoleCredentials) expirationTime() time.Time {
	return time.Unix(0, s.Expiration*int64(time.Millisecond))
}

// ssoCachedToken is the subset of the cached token file written by
// `aws sso login` under ~/.aws/sso/cache/ that we need.
type ssoCachedToken struct {
	AccessToken string    `json:"accessToken"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Region      string    `json:"region"`
}

// A FileAWSCredentials retrieves credentials from the current user's home
// directory, and keeps track if those credentials are expired.
//
// Profile ini file example: $HOME/.aws/credentials
type FileAWSCredentials struct {
	Expiry

	// Path to the shared credentials file.
	//
	// If empty, the default AWS files are merged instead: the AWS config
	// file ("AWS_CONFIG_FILE" env variable or "$HOME/.aws/config") loaded
	// first, then the shared credentials file
	// ("AWS_SHARED_CREDENTIALS_FILE" env variable or
	// "$HOME/.aws/credentials") overriding matching keys, the same
	// resolution the AWS SDK uses. Profiles written by `aws configure sso`
	// live in the config file and are discovered this way.
	//
	// If set, only this one file is read.
	Filename string

	// AWS Profile to extract credentials from the shared credentials file. If empty
	// will default to environment variable "AWS_PROFILE" or "default" if
	// environment variable is also not set.
	Profile string

	// retrieved states if the credentials have been successfully retrieved.
	retrieved bool

	// overrideSSOCacheDir overrides, for tests, the directory holding the
	// cached SSO tokens (defaults to ~/.aws/sso/cache).
	overrideSSOCacheDir string

	// overrideSSOPortalURL overrides, for tests, the AWS SSO portal URL
	// serving role credentials.
	overrideSSOPortalURL string
}

// NewFileAWSCredentials returns a pointer to a new Credentials object
// wrapping the Profile file provider.
func NewFileAWSCredentials(filename, profile string) *Credentials {
	return New(&FileAWSCredentials{
		Filename: filename,
		Profile:  profile,
	})
}

func (p *FileAWSCredentials) retrieve(cc *CredContext) (Value, error) {
	profile := p.Profile
	if profile == "" {
		profile = os.Getenv("AWS_PROFILE")
		if profile == "" {
			profile = "default"
		}
	}

	p.retrieved = false

	var (
		iniConfig  *ini.File
		iniProfile *ini.Section
		err        error
	)
	if p.Filename != "" {
		iniConfig, iniProfile, err = loadProfile(p.Filename, profile)
	} else {
		iniConfig, iniProfile, err = loadDefaultProfiles(profile)
	}
	if err != nil {
		return Value{}, err
	}

	// Default to empty string if not found.
	id := iniProfile.Key("aws_access_key_id")
	// Default to empty string if not found.
	secret := iniProfile.Key("aws_secret_access_key")
	// Default to empty string if not found.
	token := iniProfile.Key("aws_session_token")

	// A complete static key pair takes precedence over SSO configuration
	// and credential_process in the same profile, matching aws-sdk-go-v2's
	// resolution order: static credentials, then SSO, then
	// credential_process.
	hasStaticKeys := id.String() != "" && secret.String() != ""

	// Exactly one half of a static key pair is never usable and poisons
	// downstream consumers — Chain accepts a Value when either field is
	// set — so it is rejected before any resolution, matching
	// aws-sdk-go-v2's partial-credentials validation.
	if !hasStaticKeys && (id.String() != "" || secret.String() != "") {
		return Value{}, errors.New("profile has partial static credentials: aws_access_key_id and aws_secret_access_key must both be set")
	}

	// If the profile is configured for AWS SSO, obtain credentials from the
	// token cached by `aws sso login`.
	if !hasStaticKeys && iniProfile.Key("sso_role_name").String() != "" {
		ssoCreds, err := p.getSSOCredentials(cc, iniConfig, iniProfile)
		if err != nil {
			return Value{}, err
		}
		expiration := ssoCreds.RoleCredentials.expirationTime()
		p.retrieved = true
		p.SetExpiration(expiration, DefaultExpiryWindow)
		return Value{
			AccessKeyID:     ssoCreds.RoleCredentials.AccessKeyID,
			SecretAccessKey: ssoCreds.RoleCredentials.SecretAccessKey,
			SessionToken:    ssoCreds.RoleCredentials.SessionToken,
			Expiration:      expiration,
			SignerType:      SignatureV4,
		}, nil
	}

	// A profile carrying SSO configuration without sso_role_name cannot
	// engage the SSO flow above; without a complete static key pair the
	// values below would be anonymous.
	if !hasStaticKeys &&
		(iniProfile.Key("sso_session").String() != "" || iniProfile.Key("sso_start_url").String() != "" ||
			iniProfile.Key("sso_account_id").String() != "" || iniProfile.Key("sso_region").String() != "") {
		return Value{}, errors.New("profile has SSO configuration but no sso_role_name, and no complete static credentials")
	}

	// If credential_process is defined, obtain credentials by executing the
	// external process. Static credentials and SSO in the same profile both
	// take precedence over it, matching aws-sdk-go-v2.
	credentialProcess := strings.TrimSpace(iniProfile.Key("credential_process").String())
	if !hasStaticKeys && credentialProcess != "" {
		args := strings.Fields(credentialProcess)
		if len(args) <= 1 {
			return Value{}, errors.New("invalid credential process args")
		}
		cmd := exec.Command(args[0], args[1:]...)
		out, err := cmd.Output()
		if err != nil {
			return Value{}, err
		}
		var externalProcessCredentials externalProcessCredentials
		err = json.Unmarshal([]byte(out), &externalProcessCredentials)
		if err != nil {
			return Value{}, err
		}
		p.retrieved = true
		p.SetExpiration(externalProcessCredentials.Expiration, DefaultExpiryWindow)
		return Value{
			AccessKeyID:     externalProcessCredentials.AccessKeyID,
			SecretAccessKey: externalProcessCredentials.SecretAccessKey,
			SessionToken:    externalProcessCredentials.SessionToken,
			Expiration:      externalProcessCredentials.Expiration,
			SignerType:      SignatureV4,
		}, nil
	}

	p.retrieved = true
	return Value{
		AccessKeyID:     id.String(),
		SecretAccessKey: secret.String(),
		SessionToken:    token.String(),
		SignerType:      SignatureV4,
	}, nil
}

// getSSOCredentials fetches role credentials for an SSO-configured profile
// from the AWS SSO portal, using the access token cached by `aws sso login`.
func (p *FileAWSCredentials) getSSOCredentials(cc *CredContext, iniConfig *ini.File, iniProfile *ini.Section) (ssoCredentials, error) {
	ssoAccountID := iniProfile.Key("sso_account_id").String()
	ssoRoleName := iniProfile.Key("sso_role_name").String()
	if ssoAccountID == "" {
		return ssoCredentials{}, errors.New("profile defines sso_role_name but no sso_account_id")
	}

	// Modern config: the profile references an [sso-session <name>] section
	// and the cached token file is named after the SHA1 of the session name.
	// Legacy config: sso_start_url/sso_region live directly on the profile
	// and the cached token file is named after the SHA1 of the start URL.
	ssoSessionName := iniProfile.Key("sso_session").String()
	cacheKey := ssoSessionName
	ssoRegion := iniProfile.Key("sso_region").String()
	if ssoSessionName != "" {
		if sessionSection, err := iniConfig.GetSection("sso-session " + ssoSessionName); err == nil {
			// aws-sdk-go-v2 rejects profile sso_region/sso_start_url
			// values that contradict the sso-session; guessing between
			// the two could silently query the wrong portal.
			if region := sessionSection.Key("sso_region").String(); region != "" {
				if ssoRegion != "" && ssoRegion != region {
					return ssoCredentials{}, fmt.Errorf("sso_region %q in profile must match sso_region %q in sso-session %q", ssoRegion, region, ssoSessionName)
				}
				ssoRegion = region
			}
			if sessionStartURL := sessionSection.Key("sso_start_url").String(); sessionStartURL != "" {
				if profileStartURL := iniProfile.Key("sso_start_url").String(); profileStartURL != "" && profileStartURL != sessionStartURL {
					return ssoCredentials{}, fmt.Errorf("sso_start_url %q in profile must match sso_start_url %q in sso-session %q", profileStartURL, sessionStartURL, ssoSessionName)
				}
			}
		}
	} else {
		startURL := iniProfile.Key("sso_start_url").String()
		if startURL == "" {
			return ssoCredentials{}, errors.New("profile defines sso_role_name but neither sso_session nor sso_start_url")
		}
		cacheKey = startURL
	}

	hash := sha1.Sum([]byte(cacheKey))
	cachedTokenFilename := hex.EncodeToString(hash[:]) + ".json"

	cacheDir := p.overrideSSOCacheDir
	if cacheDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return ssoCredentials{}, fmt.Errorf("getting home dir: %w", err)
		}
		cacheDir = filepath.Join(homeDir, ".aws", "sso", "cache")
	}
	cachedTokenPath := filepath.Join(cacheDir, cachedTokenFilename)
	cachedTokenRaw, err := os.ReadFile(cachedTokenPath)
	if err != nil {
		return ssoCredentials{}, fmt.Errorf("reading cached SSO token %q (try `aws sso login`): %w", cachedTokenPath, err)
	}

	var cachedToken ssoCachedToken
	if err := json.Unmarshal(cachedTokenRaw, &cachedToken); err != nil {
		return ssoCredentials{}, fmt.Errorf("parsing cached SSO token %q: %w", cachedTokenPath, err)
	}
	now := time.Now
	if p.CurrentTime != nil {
		now = p.CurrentTime
	}
	if cachedToken.ExpiresAt.Before(now()) {
		return ssoCredentials{}, errors.New("cached SSO token is expired, refresh it with `aws sso login`")
	}

	if ssoRegion == "" {
		ssoRegion = cachedToken.Region
	}
	if ssoRegion == "" {
		return ssoCredentials{}, errors.New("unable to determine AWS SSO region from profile, sso-session or cached token")
	}

	portalURL := p.overrideSSOPortalURL
	if portalURL == "" {
		portalURL = ssoPortalBaseURL(ssoRegion)
	}
	if cc == nil {
		cc = defaultCredContext
	}
	ctx, cancel := context.WithTimeout(cc.requestContext(), ssoPortalRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, portalURL+"/federation/credentials", nil)
	if err != nil {
		return ssoCredentials{}, fmt.Errorf("creating SSO role credentials request: %w", err)
	}
	req.Header.Set("x-amz-sso_bearer_token", cachedToken.AccessToken)
	query := req.URL.Query()
	query.Add("account_id", ssoAccountID)
	query.Add("role_name", ssoRoleName)
	req.URL.RawQuery = query.Encode()

	client := cc.Client
	if client == nil {
		client = defaultCredContext.Client
	}
	resp, err := client.Do(req)
	if err != nil {
		return ssoCredentials{}, fmt.Errorf("fetching SSO role credentials: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return ssoCredentials{}, fmt.Errorf("fetching SSO role credentials: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var ssoCreds ssoCredentials
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ssoCreds); err != nil {
		return ssoCredentials{}, fmt.Errorf("parsing SSO role credentials response: %w", err)
	}
	if ssoCreds.RoleCredentials.AccessKeyID == "" || ssoCreds.RoleCredentials.SecretAccessKey == "" {
		return ssoCredentials{}, errors.New("SSO portal returned empty role credentials")
	}
	return ssoCreds, nil
}

// ssoPortalBaseURL returns the AWS SSO portal endpoint for region. The DNS
// suffix follows the region's AWS partition per aws-sdk-go-v2 endpoint
// metadata; regions of unlisted partitions, including aws-us-gov, use the
// standard amazonaws.com suffix.
func ssoPortalBaseURL(region string) string {
	suffix := "amazonaws.com"
	switch {
	case strings.HasPrefix(region, "cn-"):
		suffix = "amazonaws.com.cn"
	case strings.HasPrefix(region, "us-iso-"):
		suffix = "c2s.ic.gov"
	case strings.HasPrefix(region, "us-isob-"):
		suffix = "sc2s.sgov.gov"
	case strings.HasPrefix(region, "eu-isoe-"):
		suffix = "cloud.adc-e.uk"
	case strings.HasPrefix(region, "us-isof-"):
		suffix = "csp.hci.ic.gov"
	case strings.HasPrefix(region, "eusc-"):
		suffix = "amazonaws.eu"
	}
	return fmt.Sprintf("https://portal.sso.%s.%s", region, suffix)
}

// Retrieve reads and extracts the shared credentials from the current
// users home directory.
//
// Deprecated: Retrieve() exists for historical compatibility and should not
// be used. To get new credentials use the RetrieveWithCredContext function.
func (p *FileAWSCredentials) Retrieve() (Value, error) {
	return p.retrieve(nil)
}

// RetrieveWithCredContext retrieves credentials from the file like Retrieve,
// using the context's HTTP client for any SSO portal call.
func (p *FileAWSCredentials) RetrieveWithCredContext(cc *CredContext) (Value, error) {
	return p.retrieve(cc)
}

// loadProfile loads from the file pointed to by shared credentials filename for profile.
// The credentials retrieved from the profile will be returned or error. Error will be
// returned if it fails to read from the file, or the data is invalid.
func loadProfile(filename, profile string) (*ini.File, *ini.Section, error) {
	config, err := ini.Load(filename)
	if err != nil {
		return nil, nil, err
	}
	iniProfile, err := config.GetSection(profile)
	if err != nil {
		// AWS config files (~/.aws/config) name non-default profile
		// sections "profile <name>".
		var sectionErr error
		iniProfile, sectionErr = config.GetSection("profile " + profile)
		if sectionErr != nil {
			return nil, nil, err
		}
	}
	return config, iniProfile, nil
}

// loadDefaultProfiles loads profile from the default AWS files, mirroring
// the AWS SDK's shared-config resolution: the config file (AWS_CONFIG_FILE
// or ~/.aws/config) is loaded first and the shared credentials file
// (AWS_SHARED_CREDENTIALS_FILE or ~/.aws/credentials) overrides matching
// keys. A missing file is tolerated as long as the other one loads; a file
// that exists but fails to parse aborts resolution, like aws-sdk-go-v2,
// because silently skipping it could resolve credentials from the wrong
// (lower-precedence) source.
func loadDefaultProfiles(profile string) (*ini.File, *ini.Section, error) {
	configFilename := os.Getenv("AWS_CONFIG_FILE")
	credsFilename := os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	var loadErrs []error
	if configFilename == "" || credsFilename == "" {
		// A home-dir failure only rules out the files defaulted under it;
		// a file named via env var must still load.
		homeDir, err := os.UserHomeDir()
		if err != nil {
			loadErrs = append(loadErrs, err)
		} else {
			if configFilename == "" {
				configFilename = filepath.Join(homeDir, ".aws", "config")
			}
			if credsFilename == "" {
				credsFilename = filepath.Join(homeDir, ".aws", "credentials")
			}
		}
	}

	merged := ini.Empty()
	loaded := false
	if configFilename != "" {
		config, err := ini.Load(configFilename)
		switch {
		case err == nil:
			loaded = true
			mergeAWSConfigSections(merged, config)
		case errors.Is(err, fs.ErrNotExist):
			loadErrs = append(loadErrs, err)
		default:
			return nil, nil, err
		}
	}
	if credsFilename != "" {
		config, err := ini.Load(credsFilename)
		switch {
		case err == nil:
			loaded = true
			mergeAWSCredentialsSections(merged, config)
		case errors.Is(err, fs.ErrNotExist):
			loadErrs = append(loadErrs, err)
		default:
			return nil, nil, err
		}
	}
	if !loaded {
		// Prefer the home-dir error when there is one (the informative
		// failure); otherwise return the last not-exist error bare so the
		// legacy os.IsNotExist probe on a missing-files result keeps working.
		err := loadErrs[len(loadErrs)-1]
		for _, e := range loadErrs {
			if !errors.Is(e, fs.ErrNotExist) {
				err = e
				break
			}
		}
		return nil, nil, err
	}
	iniProfile, err := merged.GetSection(profile)
	if err != nil {
		return nil, nil, err
	}
	return merged, iniProfile, nil
}

// mergeAWSConfigSections copies the AWS config file's sections into dst
// under the SDK's config-file rules: bare sections other than "default" and
// "sso-session <name>" are invalid profile names and ignored, and a
// "profile <name>" section is renamed to <name>, replacing a bare
// same-named section wholesale rather than merging with it.
func mergeAWSConfigSections(dst, src *ini.File) {
	for _, section := range src.Sections() {
		name := section.Name()
		if name == ini.DefaultSection || strings.HasPrefix(name, "profile ") ||
			(!strings.EqualFold(name, "default") && !strings.HasPrefix(name, "sso-session ")) {
			continue
		}
		copySectionKeys(dst.Section(name), section)
	}
	for _, section := range src.Sections() {
		name := section.Name()
		if !strings.HasPrefix(name, "profile ") {
			continue
		}
		dstSection := dst.Section(strings.TrimPrefix(name, "profile "))
		for _, key := range dstSection.Keys() {
			dstSection.DeleteKey(key.Name())
		}
		copySectionKeys(dstSection, section)
	}
}

// mergeAWSCredentialsSections copies the shared credentials file's sections
// into dst, overriding matching keys; "profile "-prefixed section names are
// invalid in the credentials file and ignored. When a section overrides an
// existing profile, the static credentials move atomically:
// aws_access_key_id, aws_secret_access_key and aws_session_token are only
// taken from an overriding section carrying the complete key pair, so a
// partial pair cannot clobber half of an existing one. A section new to
// the merge is copied as-is; a partial pair it carries is rejected at
// resolution time, matching aws-sdk-go-v2's partial-credentials error.
func mergeAWSCredentialsSections(dst, src *ini.File) {
	for _, section := range src.Sections() {
		name := section.Name()
		if name == ini.DefaultSection || strings.HasPrefix(name, "profile ") {
			continue
		}
		_, err := dst.GetSection(name)
		newSection := err != nil
		hasPair := section.HasKey("aws_access_key_id") && section.HasKey("aws_secret_access_key")
		dstSection := dst.Section(name)
		for _, key := range section.Keys() {
			switch key.Name() {
			case "aws_access_key_id", "aws_secret_access_key", "aws_session_token":
				if !newSection && !hasPair {
					continue
				}
			}
			dstSection.Key(key.Name()).SetValue(key.Value())
		}
	}
}

func copySectionKeys(dst, src *ini.Section) {
	for _, key := range src.Keys() {
		dst.Key(key.Name()).SetValue(key.Value())
	}
}
