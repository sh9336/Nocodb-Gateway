package proxy

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/grove/generic-proxy/internal/config"
)

type ProxyHandler struct {
	NocoDBURL      string
	NocoDBToken    string
	NocoDBBaseID   string
	Meta           *MetaCache
	ResolvedConfig *config.ResolvedConfig
	Validator      *Validator
}

// NewProxyHandler creates a new proxy handler
func NewProxyHandler(nocoDBURL, nocoDBToken, nocoDBBaseID string, meta *MetaCache) *ProxyHandler {
	return &ProxyHandler{
		NocoDBURL:    nocoDBURL,
		NocoDBToken:  nocoDBToken,
		NocoDBBaseID: nocoDBBaseID,
		Meta:         meta,
	}
}

// SetResolvedConfig sets the resolved configuration and initializes the validator
func (p *ProxyHandler) SetResolvedConfig(config *config.ResolvedConfig) {
	p.ResolvedConfig = config
	p.Validator = NewValidator(config, p.Meta)
	if config.BaseID != "" {
		p.NocoDBBaseID = config.BaseID
	}
	log.Printf("[PROXY] Resolved configuration set with %d tables", len(config.Tables))
}

// ServeHTTP handles proxying requests to NocoDB
func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("[PROXY] Incoming request: %s %s", r.Method, r.URL.Path)

	// Extract the path after /proxy/
	path := strings.TrimPrefix(r.URL.Path, "/proxy/")
	log.Printf("[PROXY] Extracted path: %s", path)

	var resolvedPath string

	// If we have a validator (config-driven mode), use it
	if p.Validator != nil && p.ResolvedConfig != nil {
		log.Printf("[PROXY] Using config-driven validation")

		validation, err := p.Validator.ValidateRequest(r.Method, path)
		if err != nil {
			log.Printf("[PROXY ERROR] Validation failed: %v", err)
			http.Error(w, "forbidden: "+err.Error(), http.StatusForbidden)
			return
		}

		resolvedPath = validation.ResolvedPath
		log.Printf("[PROXY] Validated and resolved: %s -> %s", path, resolvedPath)
	} else {
		// Fallback to MetaCache-only resolution (legacy mode)
		log.Printf("[PROXY] Using legacy MetaCache-only mode")

		if p.Meta != nil {
			parts := strings.SplitN(path, "/", 2)
			if len(parts) > 0 && parts[0] != "" {
				tableName := parts[0]
				if tableID, ok := p.Meta.Resolve(tableName); ok {
					log.Printf("[META] Resolved table '%s' -> '%s'", tableName, tableID)

					// Check if this is a link request and resolve link field alias
					if len(parts) > 1 {
						remainingPath := strings.Join(parts[1:], "/")
						resolvedRemainingPath, err := p.resolveLinkFieldInPath(tableID, tableName, remainingPath)
						if err != nil {
							log.Printf("[PROXY ERROR] Link field resolution failed: %v", err)
							http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
							return
						}
						resolvedPath = tableID + "/" + resolvedRemainingPath
					} else {
						resolvedPath = tableID
					}
				} else {
					log.Printf("[META] No mapping found for table '%s', using raw name", tableName)
					resolvedPath = path
				}
			} else {
				resolvedPath = path
			}
		} else {
			resolvedPath = path
		}
	}

	// Construct the target URL
	targetURL := p.NocoDBURL
	if !strings.HasSuffix(targetURL, "/") {
		targetURL += "/"
	}

	// Detect NocoDB API structure and build path accordingly
	// Legacy NocoDB (v1) requires the BaseID in the data path: /api/v1/db/data/v1/{baseId}/{tableName}
	// Modern NocoDB (v2+) uses table IDs directly if pointing to the tables endpoint: /api/v2/tables/{tableId}/records
	if strings.Contains(targetURL, "/data/v1/") {
		log.Printf("[PROXY] Legacy NocoDB structure detected (/data/v1/), injecting BaseID")
		baseID := p.NocoDBBaseID
		if baseID == "" && p.Meta != nil {
			baseID = p.Meta.BaseID
		}
		targetURL += baseID + "/" + resolvedPath
	} else {
		log.Printf("[PROXY] Modern NocoDB structure detected (Direct Access), appending resolved path")
		targetURL += resolvedPath
	}

	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}
	log.Printf("[PROXY] Target URL: %s", targetURL)

	// Create a new request to NocoDB
	proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		log.Printf("[PROXY ERROR] Failed to create proxy request: %v", err)
		http.Error(w, "failed to create proxy request", http.StatusInternalServerError)
		return
	}
	log.Printf("[PROXY] Created proxy request successfully")

	// Copy headers from original request (except Authorization)
	for key, values := range r.Header {
		if key != "Authorization" {
			for _, value := range values {
				proxyReq.Header.Add(key, value)
			}
		}
	}

	// Add NocoDB authentication token
	proxyReq.Header.Set("xc-token", p.NocoDBToken)
	log.Printf("[PROXY] Added xc-token header")

	// Execute the request
	log.Printf("[PROXY] Executing request to NocoDB...")
	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("[PROXY ERROR] Failed to execute proxy request: %v", err)
		http.Error(w, "failed to proxy request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	log.Printf("[PROXY] NocoDB responded with status: %d %s", resp.StatusCode, resp.Status)

	// Copy response headers (excluding CORS headers to prevent duplicates)
	for key, values := range resp.Header {
		// Skip CORS headers - these are handled by CORSMiddleware
		if strings.HasPrefix(key, "Access-Control-") {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Read response body for logging
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[PROXY ERROR] Failed to read response body: %v", err)
		http.Error(w, "failed to read response", http.StatusInternalServerError)
		return
	}

	// Log response details
	if resp.StatusCode >= 400 {
		log.Printf("[PROXY ERROR] NocoDB error response (status %d): %s", resp.StatusCode, string(body))
	} else {
		log.Printf("[PROXY] Response body length: %d bytes", len(body))
		if len(body) < 500 {
			log.Printf("[PROXY] Response body: %s", string(body))
		}
	}

	// Set status code
	w.WriteHeader(resp.StatusCode)

	// Write response body
	_, err = w.Write(body)
	if err != nil {
		log.Printf("[PROXY ERROR] Failed to write response: %v", err)
	}
	log.Printf("[PROXY] Request completed successfully")
}

// resolveLinkFieldInPath detects link requests and resolves link field aliases to field IDs
// Handles paths like: links/{linkAlias}/{recordId} -> links/{linkFieldID}/{recordId}
func (p *ProxyHandler) resolveLinkFieldInPath(tableID, tableName, remainingPath string) (string, error) {
	// Split the remaining path to check if it's a link request
	parts := strings.Split(remainingPath, "/")

	// Check if this is a link request: links/{linkAlias}/{recordId}
	// Pattern: parts[0] = "links", parts[1] = linkAlias, parts[2] = recordId
	if len(parts) >= 3 && parts[0] == "links" {
		linkAlias := parts[1]
		log.Printf("[LINK RESOLVER] Detected link request for table '%s', alias '%s'", tableName, linkAlias)

		// Try to resolve the link field alias to field ID using MetaCache
		if p.Meta != nil {
			// Try direct match first
			linkFieldID, ok := p.Meta.ResolveLinkField(tableID, linkAlias)
			if !ok {
				// Try normalized version (replace underscores with spaces)
				normalizedAlias := strings.ReplaceAll(linkAlias, "_", " ")
				linkFieldID, ok = p.Meta.ResolveLinkField(tableID, normalizedAlias)
			}

			if ok {
				log.Printf("[LINK RESOLVER] %s.%s → %s", tableName, linkAlias, linkFieldID)
				// Replace the alias with the resolved field ID
				parts[1] = linkFieldID
				return strings.Join(parts, "/"), nil
			}

			// Link field not found in cache
			return "", fmt.Errorf("unknown link field '%s' for table '%s'", linkAlias, tableName)
		}

		log.Printf("[LINK RESOLVER WARNING] MetaCache not available, using alias as-is")
	}

	// Not a link request or MetaCache unavailable, return path as-is
	return remainingPath, nil
}
