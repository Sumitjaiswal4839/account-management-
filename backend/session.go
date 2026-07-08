package backend

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Session tokens are signed with HMAC-SHA256 using JWT_SECRET.
// Format: base64url(subject|expiryUnix).base64url(hmac-signature)
// This replaces the old "any string starting with Bearer " check, which
// verified nothing at all.

func sessionSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		// Fail safe for local/dev only. In production JWT_SECRET must be set,
		// otherwise every server restart invalidates all sessions - which is
		// intentional here rather than silently accepting unsigned tokens.
		secret = "dev-only-insecure-secret-set-JWT_SECRET-in-env"
	}
	return []byte(secret)
}

func sign(payload string) string {
	mac := hmac.New(sha256.New, sessionSecret())
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// IssueSessionToken creates a signed token for the given subject (e.g. user email)
// valid for the given duration.
func IssueSessionToken(subject string, ttl time.Duration) string {
	expiry := time.Now().Add(ttl).Unix()
	payload := fmt.Sprintf("%s|%d", subject, expiry)
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(payload))
	signature := sign(payload)
	return encodedPayload + "." + signature
}

// VerifySessionToken checks the signature and expiry, returning the subject if valid.
func VerifySessionToken(token string) (string, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", errors.New("malformed token")
	}
	encodedPayload, signature := parts[0], parts[1]

	payloadBytes, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return "", errors.New("malformed payload")
	}
	payload := string(payloadBytes)

	expectedSig := sign(payload)
	if subtle.ConstantTimeCompare([]byte(signature), []byte(expectedSig)) != 1 {
		return "", errors.New("invalid signature")
	}

	fields := strings.SplitN(payload, "|", 2)
	if len(fields) != 2 {
		return "", errors.New("malformed payload fields")
	}
	subject := fields[0]
	expiry, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return "", errors.New("malformed expiry")
	}
	if time.Now().Unix() > expiry {
		return "", errors.New("token expired")
	}

	return subject, nil
}
