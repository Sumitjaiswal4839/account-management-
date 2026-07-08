package backend

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"go-frontend/config"
	"go-frontend/database"

	"github.com/gin-gonic/gin"
)

// sessionTTL reads the Session Expiration value from the Settings panel
// (persisted via SaveSettingsHandler), defaulting to 1 hour if unset or
// unparsable. This makes the Settings panel's session-duration control
// actually do something, instead of being a purely cosmetic dropdown.
func sessionTTL() time.Duration {
	raw := database.GetSetting("session_ttl", "1h")
	d, err := time.ParseDuration(raw)
	if err != nil {
		return time.Hour
	}
	return d
}

// GoogleLogin generates a fresh random state per login attempt, stashes it in
// a short-lived cookie, and sends it to Google. Previously this was a single
// hardcoded string for every request, which defeats the point of the OAuth
// state CSRF check entirely.
func GoogleLogin(c *gin.Context) {
	state := generateRandomToken()
	c.SetCookie("oauth_state", state, 300, "/", "", true, true)
	url := config.GoogleOAuthConfig.AuthCodeURL(state)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

func GoogleCallback(c *gin.Context) {
	expectedState, err := c.Cookie("oauth_state")
	if err != nil || expectedState == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing OAuth state cookie - login again"})
		return
	}
	c.SetCookie("oauth_state", "", -1, "/", "", true, true) // one-time use

	state := c.Query("state")
	if state != expectedState {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid OAuth state"})
		return
	}

	code := c.Query("code")
	token, err := config.GoogleOAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		// Real failure: do NOT log the user in. Previously this branch
		// silently issued a fake "MOCK" session cookie on any exchange
		// error, which meant a broken/misconfigured OAuth setup still
		// produced a working logged-in session.
		log.Printf("OAuth token exchange failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Google sign-in failed. If you're developing locally without real " +
				"Google OAuth credentials, use /login/mock instead.",
		})
		return
	}

	client := config.GoogleOAuthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch Google profile info"})
		return
	}
	defer resp.Body.Close()

	var profile struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil || profile.Email == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse Google profile info"})
		return
	}

	// Persist a real user record for this login (previously the fetched
	// profile was parsed but never stored anywhere - there was no lasting
	// link between "logged in via Google" and any row in the users table).
	if _, err := database.UpsertUserFromLogin(profile.Email, profile.Name); err != nil {
		log.Printf("Warning: failed to upsert user record for %s: %v", profile.Email, err)
	}

	// Issue our own signed session token keyed to the user's email, rather
	// than handing the raw Google access token to the browser as the session
	// cookie (it was previously being reused as if it were our own JWT).
	sessionToken := IssueSessionToken(profile.Email, sessionTTL())
	c.SetCookie("jwt_token", sessionToken, int(sessionTTL().Seconds()), "/", "", true, true)
	c.Redirect(http.StatusTemporaryRedirect, "/dashboard")
}

// DevMockLogin bypasses real OAuth for local dev. It still issues a properly
// signed session token (subject "dev-mock-user") so it passes the same
// JWTAuth() verification as a real login - it's just clearly labeled as a
// dev bypass rather than indistinguishable "mock" strings floating around.
//
// Disabled whenever APP_ENV=production, so a deployed instance can't be
// logged into via this route without ever going through real Google OAuth.
func DevMockLogin(c *gin.Context) {
	if strings.EqualFold(os.Getenv("APP_ENV"), "production") {
		c.String(http.StatusNotFound, "404 page not found")
		return
	}
	sessionToken := IssueSessionToken("dev-mock-user", sessionTTL())
	c.SetCookie("jwt_token", sessionToken, int(sessionTTL().Seconds()), "/", "", true, true)
	c.Redirect(http.StatusTemporaryRedirect, "/dashboard")
}
