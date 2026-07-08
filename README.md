# AccSpace: Premium Account Discovery & Deletion Dashboard

AccSpace is a secure, lightweight, and modern Go web application to help users scan their email inboxes via IMAP, identify where their credentials are registered, check service risk profiles, and request automated GDPR/CCPA removals or navigate to direct deletion links.

---

## 📁 Directory Structure

```
go-frontend/
 ├── main.go               # App entry point (starts server, loads .env)
 ├── envfile.go            # Minimal .env file loader (stdlib only)
 ├── config/
 │    └── config.go        # Google OAuth config
 ├── database/
 │    └── database.go      # PostgreSQL connection pool & migrations (memory fallback)
 ├── backend/
 │    ├── handlers.go      # Endpoint controllers, CSRF, SSL, & IMAP discover flow
 │    ├── imap.go          # /scan-inbox IMAP search
 │    ├── oauth.go         # Google OAuth login/callback
 │    ├── auth.go          # JWTAuth middleware
 │    └── session.go       # Signed session token issue/verify
 ├── templates/
 │    ├── landing.html
 │    └── index.html       # Dynamic HTML5 template (HTMX ready)
 ├── static/
 │    ├── css/             # dashboard.css, landing.css, main.css, variables.css
 │    └── js/               # dashboard.js, landing.js
 ├── .env                  # Environment configuration (now actually loaded on startup)
 └── .gitignore            # Git exclusion definitions
```

---

## ⚡ Setup & Installation

### 1. Prerequisites
- **Go** (v1.20+) installed and globally added to your system's `PATH`.
- (Optional) **PostgreSQL** instance running. If PostgreSQL is missing, the application automatically triggers memory-fallback mode (which lets you test all flows using fallback memory stores).

### 2. Download Dependencies
Run this in the project root to pull all required libraries (already listed in `go.mod`):
```cmd
go mod tidy
```

### 3. Configure `.env`
Edit the `.env` file in the project root with your own values (`JWT_SECRET`, Google OAuth credentials, IMAP credentials, etc). **`.env` is now loaded automatically on startup** (see `envfile.go`) - you no longer need to manually `export` these variables first. A real environment variable (if you do export one) always takes priority over the `.env` file.

### 4. Launch Development Server
Start the HTTPS listener:
```cmd
go run main.go
```
*Note: The server will automatically generate `cert.pem` and `key.pem` self-signed certificates on its first start for local testing.*

### 5. Log in, then visit the Dashboard
Open your browser and navigate to:
👉 **[https://localhost:8085](https://localhost:8085)** (port comes from `PORT` in `.env`, defaults to 8085)

*(Since it uses a self-signed local SSL, click **Advanced** -> **Accept Risk and Continue** to enter).*

`/dashboard` and all account/API routes now require a logged-in session. From the landing page, use **Developer Bypass** to get a valid local session without needing real Google OAuth credentials configured.
"# account-management-" 
"# account-management-" 
