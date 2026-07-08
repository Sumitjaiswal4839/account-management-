package main

import (
	"bufio"
	"log"
	"os"
	"strings"
)

// loadEnvFile reads KEY=VALUE pairs from a .env file in the working directory
// and sets them as process environment variables, WITHOUT overwriting any
// variable that's already set in the real environment (so `export FOO=bar`
// before `go run main.go` still takes priority, same as most .env loaders).
//
// Previously nothing in this codebase ever read .env at all - os.Getenv()
// only sees real shell environment variables, so every value in .env
// (JWT_SECRET, GOOGLE_CLIENT_ID/SECRET, DATABASE_URL, EMAIL_*, PORT) was
// silently ignored unless someone manually exported it first. That's why
// the app "worked" (fell back to defaults / memory mode) without any error
// message, which made it look broken with no clue why.
func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		// No .env file present is fine - just rely on real env vars/defaults.
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	loaded := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Strip matching surrounding quotes, e.g. FOO="bar baz"
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		if key == "" {
			continue
		}
		if _, alreadySet := os.LookupEnv(key); alreadySet {
			continue // real environment variable wins over .env
		}
		if err := os.Setenv(key, value); err == nil {
			loaded++
		}
	}
	if loaded > 0 {
		log.Printf("Loaded %d variable(s) from %s", loaded, path)
	}
}
