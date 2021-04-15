package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	cv "github.com/nirasan/go-oauth-pkce-code-verifier"
	"github.com/skratchdot/open-golang/open"
	"gopkg.in/yaml.v3"
)

type ClientConfig struct {
	AuthURL     string `yaml:"auth_url"`
	TokenURL    string `yaml:"token_url"`
	ClientID    string `yaml:"client_id"`
	LokiAddr    string `yaml:"loki_addr"`
	Token       string `yaml:"token"`
	HeaderName  string `yaml:"header_name"`
	RedirectUri string `yaml:"redirect_uri"`
}

// AuthorizeUser implements the PKCE OAuth2 flow.
func AuthorizeUser(c ClientConfig, configFile string) {
	// initialize the code verifier
	var CodeVerifier, _ = cv.CreateCodeVerifier()

	// Create code_challenge with S256 method
	codeChallenge := CodeVerifier.CodeChallengeS256()

	// construct the authorization URL (with Auth0 as the authorization provider)
	authorizationURL := fmt.Sprintf("https://%s?scope=%s"+
		"&response_type=code&client_id=%s"+
		"&code_challenge=%s"+
		"&code_challenge_method=S256&redirect_uri=%s",
		c.AuthURL,
		url.QueryEscape("openid profile email offline_access"),
		c.ClientID,
		codeChallenge,
		c.RedirectUri)
	fmt.Println(authorizationURL)

	// start a web server to listen on a callback URL
	server := &http.Server{Addr: c.RedirectUri}

	// define a handler that will get the authorization code, call the token endpoint, and close the HTTP server
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// get the authorization code
		code := r.URL.Query().Get("code")
		if code == "" {
			fmt.Println("logcli: Url Param 'code' is missing")
			io.WriteString(w, "Error: could not find 'code' URL parameter\n")

			// close the HTTP server and return
			cleanup(server)
			return
		}

		// trade the authorization code and the code verifier for an access token
		codeVerifier := CodeVerifier.String()
		token, err := getIDToken(c.TokenURL, c.ClientID, codeVerifier, code, c.RedirectUri)
		if err != nil {
			fmt.Println("logcli: could not get access token")
			io.WriteString(w, "Error: could not retrieve access token\n")

			// close the HTTP server and return
			cleanup(server)
			return
		}

		c.Token = token
		data, err := yaml.Marshal(c)
		if err != nil {
			fmt.Println("logcli: could marshal config", err)
			io.WriteString(w, "Error: could marshal config\n")

			// close the HTTP server and return
			cleanup(server)
			return
		}

		err = ioutil.WriteFile(configFile, data, 644)
		if err != nil {
			fmt.Println("logcli: could not write config file", err)
			io.WriteString(w, "Error: could not store access token\n")

			// close the HTTP server and return
			cleanup(server)
			return
		}

		// return an indication of success to the caller
		io.WriteString(w, `
		<html>
			<body>
				<h1>Login successful!</h1>
				<h2>You can close this window and return to the grafana loki CLI.</h2>
			</body>
		</html>`)

		fmt.Println("Successfully logged into grafana loki API.")

		// close the HTTP server
		cleanup(server)
	})

	// parse the redirect URL for the port number
	u, err := url.Parse(c.RedirectUri)
	if err != nil {
		fmt.Printf("logcli: bad redirect URL: %s\n", err)
		os.Exit(1)
	}

	// set up a listener on the redirect port
	port := fmt.Sprintf(":%s", u.Port())
	l, err := net.Listen("tcp", port)
	if err != nil {
		fmt.Printf("logcli: can't listen to port %s: %s\n", port, err)
		os.Exit(1)
	}

	// open a browser window to the authorizationURL
	err = open.Start(authorizationURL)
	if err != nil {
		fmt.Printf("logcli: can't open browser to URL %s: %s\n", authorizationURL, err)
		os.Exit(1)
	}

	// start the blocking web server loop
	// this will exit when the handler gets fired and calls server.Close()
	server.Serve(l)
}

// getIDToken trades the authorization code retrieved from oidc id token
func getIDToken(tokenURL, clientID string, codeVerifier string, authorizationCode string, callbackURL string) (string, error) {
	// set the url and form-encoded data for the POST to the access token endpoint
	url := fmt.Sprintf("https://%s/oauth/token", tokenURL)
	data := fmt.Sprintf(
		"grant_type=authorization_code&client_id=%s"+
			"&code_verifier=%s"+
			"&code=%s"+
			"&redirect_uri=%s",
		clientID, codeVerifier, authorizationCode, callbackURL)
	payload := strings.NewReader(data)

	// create the request and execute it
	req, _ := http.NewRequest("POST", url, payload)
	req.Header.Add("content-type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("logcli: HTTP error: %s", err)
		return "", err
	}

	// process the response
	defer res.Body.Close()
	var responseData map[string]interface{}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("logcli: read response body error: %s", err)
		return "", err
	}

	// unmarshal the json into a string map
	err = json.Unmarshal(body, &responseData)
	if err != nil {
		fmt.Printf("logcli: JSON error: %s", err)
		return "", err
	}

	// retrieve the id_token(JWT) out of the map, and return to caller
	idToken := responseData["id_token"].(string)
	return idToken, nil
}

// cleanup closes the HTTP server
func cleanup(server *http.Server) {
	// we run this as a goroutine so that this function falls through and
	// the socket to the browser gets flushed/closed before the server goes away
	go server.Close()
}
