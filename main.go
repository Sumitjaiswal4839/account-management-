package main

import (
	"log"
	"os"
	"time"

	"go-frontend/backend"
	"go-frontend/config"
	"go-frontend/database"

	"github.com/gin-gonic/gin"
)

func main() {
	// 0. Load .env into the process environment (see envfile.go). Must run
	// before config.InitConfig()/database.InitDB(), which both read env vars
	// via os.Getenv() - previously nothing loaded .env at all, so every value
	// in it was silently ignored.
	loadEnvFile(".env")

	// 1. SSL/TLS Certificate Setup
	if err := backend.GenerateSelfSignedCert(); err != nil {
		log.Fatalf("Failed to generate self-signed certificates: %v", err)
	}

	// 2. Database connection initialization
	database.InitDB()

	// 3. Initialize Config parameters
	config.InitConfig()

	// 4. Initialize Gin Engine Router
	r := gin.Default()

	// Static Assets config
	r.Static("/static", "./static")

	// Load HTML templates
	r.LoadHTMLGlob("templates/*")

	// Public routes: landing page + login endpoints only.
	// Login endpoints get rate-limited (per-IP) since they're the most
	// obvious brute-force/spam targets - previously nothing throttled these.
	r.GET("/", backend.RootHandler)
	r.GET("/login/google", backend.RateLimit(20, time.Minute), backend.GoogleLogin)
	r.GET("/oauth2/callback", backend.RateLimit(20, time.Minute), backend.GoogleCallback)
	r.GET("/login/mock", backend.RateLimit(10, time.Minute), backend.DevMockLogin) // Local dev mock login bypass helper (disabled when APP_ENV=production)

	// Everything else in the app is real user/account data, so it now
	// requires a valid signed session (JWTAuth). Previously only /scan-inbox
	// was protected - /dashboard, add/delete-account, discover, cleanup, and
	// settings were all reachable by anyone with no login at all.
	authed := r.Group("/")
	authed.Use(backend.JWTAuth())
	{
		authed.GET("/dashboard", backend.DashboardHandler)
		authed.POST("/add-account", backend.RateLimit(30, time.Minute), backend.AddAccountHandler)
		authed.DELETE("/delete-account", backend.DeleteAccountHandler)
		authed.POST("/api/discover", backend.DiscoverHandler)
		authed.POST("/api/cleanup", backend.CleanupHandler)
		authed.GET("/api/filter-timeline", backend.FilterTimelineHandler)
		authed.POST("/api/settings", backend.SaveSettingsHandler)
		// POST now (was GET) - IMAP credentials are sent as form fields, not
		// URL query params, so passwords don't land in access logs/history.
		authed.POST("/scan-inbox", backend.ScanInbox)
	}

	// Port is now driven by the PORT env var (see .env) so it can never drift
	// out of sync with the OAuth RedirectURL in config/config.go again.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8085"
	}

	log.Printf("Server starting on HTTPS: https://localhost:%s\n", port)
	err := r.RunTLS(":"+port, "cert.pem", "key.pem")
	if err != nil {
		log.Fatalf("HTTPS Server failed: %v", err)
	}
}
