package backend

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// JWTAuth middleware verifies the signed session token (see session.go).
// A request is only allowed through if the token's HMAC signature checks out
// AND it hasn't expired - previously this only checked for a "Bearer " prefix,
// which meant any string in that shape was accepted as valid auth.
func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token != "" {
			token = strings.TrimPrefix(token, "Bearer ")
		} else if cookieToken, err := c.Cookie("jwt_token"); err == nil {
			token = cookieToken
		}

		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: Missing token"})
			c.Abort()
			return
		}

		subject, err := VerifySessionToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: " + err.Error()})
			c.Abort()
			return
		}

		// Downstream handlers can read this to know who's logged in.
		c.Set("session_subject", subject)
		c.Next()
	}
}
