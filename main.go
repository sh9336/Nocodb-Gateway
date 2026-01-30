package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/sessions"
	"github.com/grove/generic-proxy/internal/auth"
	"github.com/grove/generic-proxy/internal/config"
	"github.com/grove/generic-proxy/internal/db"
	"github.com/grove/generic-proxy/internal/introspect"
	"github.com/grove/generic-proxy/internal/middleware"
	"github.com/grove/generic-proxy/internal/proxy"
	"github.com/grove/generic-proxy/internal/utils"
	"github.com/markbates/goth/gothic"
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token  string `json:"token"`
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// Demo users for testing
var demoUsers = map[string]struct {
	Password string
	UserID   string
	Role     string
}{
	"admin@example.com": {
		Password: "admin123",
		UserID:   "admin-001",
		Role:     "admin",
	},
	"user@example.com": {
		Password: "user123",
		UserID:   "user-001",
		Role:     "user",
	},
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("[STARTUP] Initializing Generic Proxy Server with OAuth...")

	// Load environment configuration
	cfg := config.Load()

	// Load proxy configuration (optional - for config-driven mode)
	var proxyConfig *config.ProxyConfig
	var resolvedConfig *config.ResolvedConfig
	proxyConfigPath := os.Getenv("PROXY_CONFIG_PATH")
	if proxyConfigPath == "" {
		proxyConfigPath = "./config/proxy.yaml"
	}
	if _, err := os.Stat(proxyConfigPath); err == nil {
		log.Printf("[STARTUP] Loading proxy configuration from: %s", proxyConfigPath)
		proxyConfig, err = config.LoadProxyConfig(proxyConfigPath)
		if err != nil {
			log.Printf("[STARTUP WARN] Failed to load proxy config: %v", err)
			log.Printf("[STARTUP] Continuing in legacy mode without config-driven schema")
		}
	} else {
		log.Printf("[STARTUP] No proxy config found at %s, using legacy mode", proxyConfigPath)
	}

	log.Printf("[STARTUP] Configuration loaded:")
	log.Printf("  - Port: %s", cfg.Port)
	log.Printf("  - NocoDB URL: %s", cfg.NocoDBURL)
	log.Printf("  - NocoDB Base ID: %s", cfg.NocoDBBaseID)
	log.Printf("  - JWT Secret: %s", cfg.MaskSecret(cfg.JWTSecret))
	log.Printf("  - Database Path: %s", cfg.DatabasePath)

	// Initialize SQLite database for user storage
	database, err := db.NewDatabase(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("[STARTUP ERROR] Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Initialize Goth OAuth providers
	initializeGothProviders(cfg)

	// Setup gothic session store
	store := sessions.NewCookieStore([]byte(cfg.SessionSecret))
	store.MaxAge(86400 * 30) // 30 days
	store.Options.Path = "/"
	store.Options.HttpOnly = true
	store.Options.Secure = false // Set to true in production with HTTPS
	gothic.Store = store

	// Ensure NocoDB URL ends with /
	nocoDBURL := cfg.NocoDBURL
	if !strings.HasSuffix(nocoDBURL, "/") {
		nocoDBURL += "/"
	}

	// Initialize MetaCache for table name resolution
	var metaCache *proxy.MetaCache
	if cfg.NocoDBBaseID != "" {
		metaBaseURL := deriveMetaBaseURL(nocoDBURL)
		log.Printf("[STARTUP] Meta Base URL: %s", metaBaseURL)

		metaCache = proxy.NewMetaCache(metaBaseURL, cfg.NocoDBBaseID, cfg.NocoDBToken)

		// Perform initial synchronous metadata load
		if err := metaCache.LoadInitial(); err != nil {
			log.Fatalf("[STARTUP FATAL] MetaCache initial load failed: %v", err)
		}

		// Start background auto-refresh
		metaCache.StartAutoRefresh()

		// If we have a proxy config, resolve it using MetaCache (only after MetaCache is ready)
		if proxyConfig != nil {
			log.Printf("[STARTUP] Resolving proxy configuration using loaded MetaCache...")
			resolver := config.NewResolver(metaCache)
			resolvedConfig, err = resolver.Resolve(proxyConfig)
			if err != nil {
				log.Printf("[STARTUP ERROR] ❌ Failed to resolve proxy configuration: %v", err)
				log.Printf("[STARTUP ERROR] This means the proxy.yaml references tables/fields not found in NocoDB")
				log.Printf("[STARTUP] Falling back to legacy mode (no schema validation)")
				resolvedConfig = nil
			} else {
				log.Printf("[STARTUP] ✅ Successfully resolved proxy configuration")
				log.Printf("[STARTUP] Schema-driven mode ACTIVE with %d tables", len(resolvedConfig.Tables))
			}
		}
	} else {
		log.Println("[STARTUP WARN] NOCODB_BASE_ID not set - MetaCache disabled")
	}

	// Create proxy handler
	proxyHandler := proxy.NewProxyHandler(nocoDBURL, cfg.NocoDBToken, cfg.NocoDBBaseID, metaCache)

	// Set resolved configuration if available (config-driven mode)
	if resolvedConfig != nil {
		proxyHandler.SetResolvedConfig(resolvedConfig)
		log.Printf("[STARTUP] Proxy handler configured in schema-driven mode")
	} else {
		log.Printf("[STARTUP] Proxy handler configured in legacy mode")
	}

	// Create auth handler
	authHandler := auth.NewHandler(database, cfg.JWTSecret, "http://localhost:4321")

	// Create introspection handler
	introspectHandler := introspect.NewHandler(metaCache, resolvedConfig, proxyConfigPath)

	// Create router
	mux := http.NewServeMux()

	// Public endpoints
	mux.HandleFunc("/login", loginHandler(database, cfg.JWTSecret))
	mux.HandleFunc("/signup", signupHandler(database, cfg.JWTSecret))
	mux.HandleFunc("/health", healthHandler)

	// Introspection endpoints (read-only, no auth required for ops visibility)
	mux.HandleFunc("/__proxy/status", introspectHandler.ServeStatus)
	mux.HandleFunc("/__proxy/schema", introspectHandler.ServeSchema)

	// OAuth endpoints
	mux.HandleFunc("/auth/google", authHandler.BeginAuth)
	mux.HandleFunc("/auth/google/callback", authHandler.CallbackAuth)
	mux.HandleFunc("/auth/github", authHandler.BeginAuth)
	mux.HandleFunc("/auth/github/callback", authHandler.CallbackAuth)
	mux.HandleFunc("/auth/logout", authHandler.Logout)

	// Protected auth endpoints
	protectedUserHandler := auth.AuthMiddleware(cfg.JWTSecret)(
		http.HandlerFunc(authHandler.GetCurrentUser),
	)
	mux.Handle("/auth/me", protectedUserHandler)

	// Protected secure ping endpoint (example)
	protectedPingHandler := auth.AuthMiddleware(cfg.JWTSecret)(
		http.HandlerFunc(securePingHandler(database)),
	)
	mux.Handle("/api/secure/ping", protectedPingHandler)

	// Protected proxy endpoints (ONLY data access path)
	protectedHandler := middleware.AuthMiddleware(cfg.JWTSecret)(
		middleware.AuthorizeMiddleware(proxyHandler),
	)
	mux.Handle("/proxy/", protectedHandler)

	// Apply CORS middleware (outermost layer to prevent duplicates)
	handler := middleware.CORSMiddleware(mux)

	// Start server
	addr := ":" + cfg.Port
	log.Printf("\n[STARTUP] ========================================")
	log.Printf("[STARTUP] Generic NocoDB Proxy Server")
	log.Printf("[STARTUP] ========================================")
	log.Printf("[STARTUP] Server Address: %s", addr)
	log.Printf("[STARTUP] NocoDB URL: %s", nocoDBURL)

	// Log proxy mode
	if resolvedConfig != nil {
		log.Printf("\n[STARTUP] 🎯 PROXY MODE: Schema-Driven")
		log.Printf("[STARTUP]    Config: %s", proxyConfigPath)
		log.Printf("[STARTUP]    Tables: %d configured", len(resolvedConfig.Tables))
		log.Printf("[STARTUP]    Validation: ENABLED")
	} else {
		log.Printf("\n[STARTUP] 🔓 PROXY MODE: Legacy (No Validation)")
		log.Printf("[STARTUP]    All operations allowed")
	}

	log.Printf("\n[STARTUP] Endpoints:")
	log.Printf("  - Data Access:    /proxy/*")
	log.Printf("  - Status:         /__proxy/status")
	log.Printf("  - Schema Info:    /__proxy/schema")
	log.Printf("  - Health Check:   /health")

	log.Printf("\n[STARTUP] OAuth Providers:")
	if cfg.GoogleClientID != "" {
		log.Printf("  ✓ Google OAuth enabled")
		log.Printf("    Callback: %s", cfg.GoogleCallbackURL)
	} else {
		log.Printf("  ✗ Google OAuth disabled (set GOOGLE_CLIENT_ID)")
	}
	if cfg.GitHubClientID != "" {
		log.Printf("  ✓ GitHub OAuth enabled")
		log.Printf("    Callback: %s", cfg.GitHubCallbackURL)
	} else {
		log.Printf("  ✗ GitHub OAuth disabled (set GITHUB_CLIENT_ID)")
	}

	log.Printf("\n[STARTUP] Demo users (legacy login):")
	log.Printf("  - admin@example.com / admin123 (role: admin)")
	log.Printf("  - user@example.com / user123 (role: user)")
	log.Printf("\n[STARTUP] ========================================")
	log.Println("[STARTUP] ✅ Server ready!")
	log.Printf("[STARTUP] ========================================\n")

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func loginHandler(database *db.Database, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[LOGIN] Login attempt from %s", r.RemoteAddr)

		if r.Method != http.MethodPost {
			log.Printf("[LOGIN ERROR] Invalid method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[LOGIN ERROR] Failed to decode request body: %v", err)
			respondWithError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		log.Printf("[LOGIN] Login request for email: %s", req.Email)

		// Try database authentication first
		dbUser, err := database.ValidatePassword(req.Email, req.Password)
		if err == nil && dbUser != nil {
			log.Printf("[LOGIN] Database user authenticated: %s (role: %s)", dbUser.Email, dbUser.Role)

			// Generate JWT
			token, err := utils.GenerateJWT(fmt.Sprintf("%d", dbUser.ID), dbUser.Role, jwtSecret)
			if err != nil {
				log.Printf("[LOGIN ERROR] Failed to generate JWT: %v", err)
				respondWithError(w, http.StatusInternalServerError, "failed to generate token")
				return
			}

			// Return token
			w.Header().Set("Content-Type", "application/json")
			response := LoginResponse{
				Token:  token,
				UserID: fmt.Sprintf("%d", dbUser.ID),
				Role:   dbUser.Role,
			}
			json.NewEncoder(w).Encode(response)
			log.Printf("[LOGIN] Login successful for database user: %s", dbUser.Email)
			return
		}

		// Fallback to demo users
		user, exists := demoUsers[req.Email]
		if !exists || user.Password != req.Password {
			log.Printf("[LOGIN ERROR] Invalid credentials for email: %s", req.Email)
			respondWithError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		log.Printf("[LOGIN] Credentials validated for demo user: %s (role: %s)", user.UserID, user.Role)

		// Generate JWT
		log.Printf("[LOGIN] Generating JWT token...")
		token, err := utils.GenerateJWT(user.UserID, user.Role, jwtSecret)
		if err != nil {
			log.Printf("[LOGIN ERROR] Failed to generate JWT: %v", err)
			respondWithError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}
		log.Printf("[LOGIN] JWT generated successfully")

		// Return token
		w.Header().Set("Content-Type", "application/json")
		response := LoginResponse{
			Token:  token,
			UserID: user.UserID,
			Role:   user.Role,
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("[LOGIN ERROR] Failed to encode response: %v", err)
			return
		}
		log.Printf("[LOGIN] Login successful for demo user: %s", user.UserID)
	}
}

type SignupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

func signupHandler(database *db.Database, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[SIGNUP] Signup attempt from %s", r.RemoteAddr)

		if r.Method != http.MethodPost {
			log.Printf("[SIGNUP ERROR] Invalid method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req SignupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("[SIGNUP ERROR] Failed to decode request body: %v", err)
			respondWithError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		// Validate input
		if req.Email == "" || req.Password == "" || req.Name == "" {
			log.Printf("[SIGNUP ERROR] Missing required fields")
			respondWithError(w, http.StatusBadRequest, "email, password, and name are required")
			return
		}

		if len(req.Password) < 6 {
			log.Printf("[SIGNUP ERROR] Password too short")
			respondWithError(w, http.StatusBadRequest, "password must be at least 6 characters")
			return
		}

		log.Printf("[SIGNUP] Creating user: email=%s, name=%s", req.Email, req.Name)

		// Check if user already exists (from OAuth or previous signup)
		existingUser, err := database.GetUserByEmail(req.Email)
		if err == nil && existingUser != nil {
			log.Printf("[SIGNUP ERROR] User already exists with email: %s", req.Email)
			respondWithError(w, http.StatusConflict, "an account with this email already exists. Please login instead.")
			return
		}

		// Create user in database
		user, err := database.CreateLocalUser(req.Email, req.Password, req.Name)
		if err != nil {
			log.Printf("[SIGNUP ERROR] Failed to create user: %v", err)
			respondWithError(w, http.StatusInternalServerError, "failed to create user account")
			return
		}

		log.Printf("[SIGNUP] User created successfully: ID=%d, Email=%s", user.ID, user.Email)

		// Generate JWT token
		token, err := utils.GenerateJWT(fmt.Sprintf("%d", user.ID), user.Role, jwtSecret)
		if err != nil {
			log.Printf("[SIGNUP ERROR] Failed to generate JWT: %v", err)
			respondWithError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}

		// Return token
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		response := LoginResponse{
			Token:  token,
			UserID: fmt.Sprintf("%d", user.ID),
			Role:   user.Role,
		}
		json.NewEncoder(w).Encode(response)
		log.Printf("[SIGNUP] Signup successful for user: %s", user.Email)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// CORS middleware moved to middleware/cors.go to prevent duplicate headers
// Helper functions moved to main_helpers.go
