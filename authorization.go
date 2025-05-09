package authztraefikgateway

import (
	"context"
	"crypto/tls" // TLS config for development
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Config holds the plugin configuration (camelCase names to match YAML keys)
type Config struct {
	KeycloakURL      string `json:"keycloakURL,omitempty"`
	KeycloakClientId string `json:"keycloakClientId,omitempty"`
}

// CreateConfig creates an empty config; actual values come from YAML
func CreateConfig() *Config {
	return &Config{}
}

// AuthMiddleware holds the plugin state
type AuthMiddleware struct {
	next             http.Handler
	keycloakClientId string
	keycloakUrl      string
	name             string
}

// ServeHTTP handles the incoming request and checks permission via Keycloak
func (am *AuthMiddleware) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	fmt.Println("🔎 [AUTH] ServeHTTP Called")

	authorizationHeader := req.Header.Get("Authorization")
	if authorizationHeader == "" {
		fmt.Println("❌ [AUTH] Authorization header is missing")
		http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
		return
	}
	fmt.Println("🔎 [AUTH] Authorization Header:", authorizationHeader)

	// 🧠 Extract the path and derive `resource` and `scope`
	// Assumes path like: /prefix1/prefix2/prefix3/<resource>/<scope>/...
	pathParts := strings.Split(req.URL.Path, "/")
	if len(pathParts) < 6 {
		// Needs at least 6 parts: "", prefix1, prefix2, prefix3, resource, scope
		fmt.Println("❌ [AUTH] Path too short. Must be at least: /.../<resource>/<scope>/...")
		http.Error(w, "Invalid path format. Expected format: /prefix/.../<resource>/<scope>", http.StatusBadRequest)
		return
	}

	// Extract `resource` and `scope` as the 4th and 5th segments (index 4 and 5)
	resource := pathParts[4]
	scope := pathParts[5]
	permission := "/" + resource + "#" + scope
	fmt.Println("🔎 [AUTH] Derived permission:", permission)

	// Prepare request payload for Keycloak
	formData := url.Values{}
	formData.Set("permission", permission)
	formData.Set("grant_type", "urn:ietf:params:oauth:grant-type:uma-ticket")
	formData.Set("audience", am.keycloakClientId)

	if am.keycloakUrl == "" {
		fmt.Println("❌ [CONFIG] Keycloak URL is empty in middleware. Cannot proceed.")
		http.Error(w, "Misconfigured Keycloak URL", http.StatusInternalServerError)
		return
	}

	// 🔐 Build the request to Keycloak
	kcReq, err := http.NewRequest("POST", am.keycloakUrl, strings.NewReader(formData.Encode()))
	if err != nil {
		fmt.Println("❌ [HTTP] Error creating Keycloak request:", err)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	kcReq.Header.Set("Authorization", authorizationHeader)
	kcReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	fmt.Println("🔄 [REQUEST] Sending request to Keycloak:", am.keycloakUrl)

	// ⚠️ TLS config: skip verify only for development/testing!
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	// 🔍 Send request to Keycloak
	kcResp, err := client.Do(kcReq)
	if err != nil {
		fmt.Println("❌ [HTTP] Error performing Keycloak request:", err)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	defer kcResp.Body.Close()

	// 📦 Read and log Keycloak's response
	bodyBytes, _ := io.ReadAll(kcResp.Body)
	bodyString := string(bodyBytes)

	fmt.Println("🔎 [HTTP] Keycloak response status:", kcResp.Status)
	fmt.Println("📦 [HTTP] Keycloak response body:", bodyString)

	if kcResp.StatusCode == http.StatusOK {
		fmt.Println("✅ [AUTHZ] Access granted by Keycloak")
		am.next.ServeHTTP(w, req)
	} else {
		fmt.Printf("❌ [AUTHZ] Access denied by Keycloak. Status code: %d\n", kcResp.StatusCode)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}
}

// New is called by Traefik to create the middleware instance
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	fmt.Println("🔧 [INIT] New Middleware Initialization")
	fmt.Printf("🔧 [INIT] Config pointer: %p\n", config)
	fmt.Printf("🔧 [CONFIG] Raw config: %+v\n", config)

	if config == nil {
		fmt.Println("❌ [CONFIG] Received nil config! Middleware cannot proceed.")
		return nil, fmt.Errorf("nil config provided")
	}

	if strings.TrimSpace(config.KeycloakURL) == "" {
		fmt.Println("⚠️  [CONFIG] KeycloakURL is empty! Make sure you define it in the dynamic middleware config.")
	}
	if strings.TrimSpace(config.KeycloakClientId) == "" {
		fmt.Println("⚠️  [CONFIG] KeycloakClientId is empty! Make sure you define it in the dynamic middleware config.")
	}

	mw := &AuthMiddleware{
		next:             next,
		name:             name,
		keycloakUrl:      config.KeycloakURL,
		keycloakClientId: config.KeycloakClientId,
	}

	fmt.Printf("🔧 [INIT] Middleware initialized with keycloakUrl: [%s], keycloakClientId: [%s]\n", mw.keycloakUrl, mw.keycloakClientId)

	return mw, nil
}
