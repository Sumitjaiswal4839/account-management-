package backend

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"html"
	"html/template"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"go-frontend/database"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/gin-gonic/gin"
)

// Snippet templates for HTMX partial rendering
const rowTemplate = `
<tr>
    <td>
        <div class="user-cell">
            <div class="user-initials">{{.Initials}}</div>
            <span>{{.Name}}</span>
        </div>
    </td>
    <td>{{.Email}}</td>
    <td>{{if .Phone}}{{.Phone}}{{else}}—{{end}}</td>
    <td><span class="badge badge-{{.RoleClass}}">{{.Role}}</span></td>
    <td>
        <div class="status-indicator">
            <span class="status-dot active"></span>
            <span>Active</span>
        </div>
    </td>
    <td>Just now</td>
    <td class="actions-col">
        <button class="btn-icon-only btn-delete-account" hx-delete="/delete-account?id={{.ID}}" hx-target="closest tr" hx-swap="outerHTML">🗑️</button>
    </td>
</tr>
`

var tmplRow = template.Must(template.New("row").Parse(rowTemplate))

const discoverRowTemplate = `
<tr class="discovered-row">
    <td>
        <div class="user-cell">
            <div class="user-initials">{{.Initials}}</div>
            <strong>{{.ServiceName}}</strong>
            {{if .IsMock}}<span class="badge badge-medium" title="IMAP scan could not reach the mail server, so this is placeholder demo data - not a real result">DEMO</span>{{end}}
        </div>
    </td>
    <td>{{.LoginEmail}}</td>
    <td><span class="badge badge-{{.RiskLevel}}">{{.RiskLevel}} Risk</span></td>
    <td>
        <div class="status-indicator">
            <span class="status-dot {{if eq .Status "Active"}}active{{else}}inactive{{end}}"></span>
            <span>{{.Status}}</span>
        </div>
    </td>
    <td>{{.LoginDate}}</td>
    <td>{{.LastActive}}</td>
    <td class="actions-col">
        <button class="btn btn-secondary btn-sm" onclick="window.open('{{.DeleteUrl}}', '_blank')">🔗 Direct Link</button>
        <button class="btn btn-danger btn-sm" hx-post="/api/cleanup?id={{.ID}}" hx-target="closest tr" hx-swap="outerHTML">🗑️ CCPA Delete</button>
    </td>
</tr>
`

var tmplDiscoverRow = template.Must(template.New("discoverRow").Parse(discoverRowTemplate))

// Generate self-signed certificate if cert.pem/key.pem don't exist
func GenerateSelfSignedCert() error {
	_, errCert := os.Stat("cert.pem")
	_, errKey := os.Stat("key.pem")
	if errCert == nil && errKey == nil {
		return nil
	}

	log.Println("Generating self-signed SSL/TLS certificates (cert.pem and key.pem)...")
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"AccSpace Local Setup"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	template.IPAddresses = append(template.IPAddresses, net.ParseIP("127.0.0.1"))
	template.DNSNames = append(template.DNSNames, "localhost")

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.Create("cert.pem")
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return err
	}

	keyOut, err := os.OpenFile("key.pem", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return err
	}

	log.Println("Certificates generated successfully.")
	return nil
}

func generateRandomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func validateCSRF(c *gin.Context) bool {
	cookieVal, err := c.Cookie("csrf_token")
	if err != nil {
		return false
	}
	headerToken := c.GetHeader("X-CSRF-Token")
	return headerToken != "" && headerToken == cookieVal
}

func extractDomain(emailAddr string) string {
	re := regexp.MustCompile(`@([a-zA-Z0-9.-]+)`)
	matches := re.FindStringSubmatch(emailAddr)
	if len(matches) > 1 {
		return matches[1]
	}
	return "unknown.com"
}

// Route Controllers (Gin context handlers)

func RootHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "landing.html", nil)
}

func DashboardHandler(c *gin.Context) {
	csrfToken, err := c.Cookie("csrf_token")
	if err != nil {
		csrfToken = generateRandomToken()
		// Set cookie
		c.SetCookie("csrf_token", csrfToken, 3600, "/", "", true, true)
	}

	accounts, err := database.FetchUsers()
	if err != nil {
		log.Printf("Fetch users failed: %v", err)
	}

	subject, _ := c.Get("session_subject")
	emailSubject, _ := subject.(string)
	userID, _ := database.GetUserIDByEmail(emailSubject)
	user, _ := database.GetUserByEmail(emailSubject)

	discovered, err := database.FetchDiscoveredAccounts(userID, "")
	if err != nil {
		log.Printf("Fetch discovered failed: %v", err)
	}

	c.HTML(http.StatusOK, "index.html", gin.H{
		"Title":           "Account Space - Premium Dashboard",
		"CSRFToken":       csrfToken,
		"Accounts":        accounts,
		"Count":           len(accounts),
		"Discovered":      discovered,
		"DefaultImapHost": database.GetSetting("imap_host", "imap.gmail.com:993"),
		"SessionTTL":      database.GetSetting("session_ttl", "1h"),
		"CurrentUser":     user,
	})
}

func AddAccountHandler(c *gin.Context) {
	if !validateCSRF(c) {
		c.String(http.StatusForbidden, "Forbidden: CSRF token mismatch")
		return
	}

	name := html.EscapeString(strings.TrimSpace(c.PostForm("username")))
	email := html.EscapeString(strings.TrimSpace(c.PostForm("email")))
	phone := html.EscapeString(strings.TrimSpace(c.PostForm("phone")))
	role := html.EscapeString(strings.TrimSpace(c.PostForm("role")))

	if name == "" || email == "" || !strings.Contains(email, "@") {
		c.String(http.StatusBadRequest, "Bad Request: Invalid params")
		return
	}

	newID, err := database.InsertUser(email, name, phone, role)
	if err != nil {
		log.Printf("Database insert error: %v", err)
		c.String(http.StatusInternalServerError, "DB error")
		return
	}

	data := database.RowData{
		ID:        newID,
		Initials:  database.GetInitials(name),
		Name:      name,
		Phone:     phone,
		Email:     email,
		Role:      role,
		RoleClass: strings.ToLower(role),
	}

	c.Header("Content-Type", "text/html")
	_ = tmplRow.Execute(c.Writer, data)
}

func DeleteAccountHandler(c *gin.Context) {
	if !validateCSRF(c) {
		c.String(http.StatusForbidden, "Forbidden: CSRF token mismatch")
		return
	}

	idParam := c.Query("id")
	if idParam == "" {
		c.String(http.StatusBadRequest, "Missing ID")
		return
	}

	err := database.DeleteUser(idParam)
	if err != nil {
		log.Printf("Delete database error: %v", err)
		c.String(http.StatusInternalServerError, "DB error")
		return
	}
	c.Status(http.StatusOK)
}

func DiscoverHandler(c *gin.Context) {
	if !validateCSRF(c) {
		c.String(http.StatusForbidden, "Forbidden: CSRF token mismatch")
		return
	}

	emailVal := html.EscapeString(c.PostForm("email"))
	passwordVal := c.PostForm("password")
	imapServer := html.EscapeString(c.PostForm("imap_server"))

	if emailVal == "" || passwordVal == "" {
		c.String(http.StatusBadRequest, "Missing fields")
		return
	}

	var discoveredAccounts []database.DiscoveredAccount

	subject, _ := c.Get("session_subject")
	emailSubject, _ := subject.(string)
	userID, _ := database.GetUserIDByEmail(emailSubject)

	log.Printf("IMAP target: %s for %s", imapServer, emailVal)
	imapClient, err := client.DialTLS(imapServer, nil)
	if err != nil {
		log.Printf("IMAP Connection failed: %v", err)
		c.String(http.StatusInternalServerError, "❌ Connection failed: " + err.Error() + ". Please verify your IMAP host details.")
		return
	}
	defer imapClient.Logout()

	if err := imapClient.Login(emailVal, passwordVal); err != nil {
			log.Printf("IMAP Login failed: %v", err)
			c.String(http.StatusUnauthorized, "IMAP Login failed. If this is a Gmail/Google account with 2-Step "+
				"Verification enabled, you need a 16-character App Password (myaccount.google.com/apppasswords), "+
				"not your regular account password.")
			return
		}

		// Select "All Mail" (Gmail default) to scan archive, falling back to INBOX
		mbox, err := imapClient.Select("[Gmail]/All Mail", true)
		if err != nil {
			mbox, err = imapClient.Select("INBOX", true)
		}
		if err != nil {
			c.String(http.StatusInternalServerError, "Select Mailbox failed")
			return
		}

		from := uint32(1)
		if mbox.Messages > 100 { // increase fetch limit to 100 to catch more emails
			from = mbox.Messages - 99
		}
		seqset := new(imap.SeqSet)
		seqset.AddRange(from, mbox.Messages)

		items := []imap.FetchItem{imap.FetchEnvelope}
		messages := make(chan *imap.Message, 100)
		done := make(chan error, 1)

		go func() {
			done <- imapClient.Fetch(seqset, items, messages)
		}()

		welcomeRegex := regexp.MustCompile(`(?i)(welcome|verify|registration|confirm|signup|your account)`)
		curatedDomains := map[string]bool{
			"spotify.com":   true,
			"netflix.com":   true,
			"linkedin.com":  true,
			"facebook.com":  true,
			"github.com":    true,
			"instagram.com": true,
			"google.com":    true,
			"amazon.com":    true,
			"twitter.com":   true,
			"zoom.us":       true,
			"slack.com":     true,
		}

		for msg := range messages {
			if msg.Envelope == nil || len(msg.Envelope.From) == 0 {
				continue
			}
			subject := msg.Envelope.Subject
			sender := msg.Envelope.From[0]
			domain := extractDomain(sender.Address())
			isCurated := curatedDomains[domain]

			if welcomeRegex.MatchString(subject) || isCurated {
				duplicate := false
				for _, d := range discoveredAccounts {
					if d.ServiceName == domain {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}

				newID, err := database.InsertDiscoveredAccount(userID, domain, emailVal, "Active", "Discovered headers", "low", time.Now().Format("2006-01-02"), "Password", 10, false)
				if err == nil {
					d := database.DiscoveredAccount{
						ID:          newID,
						Initials:    database.GetInitials(domain),
						ServiceName: domain,
						LoginEmail:  emailVal,
						Status:      "Active",
						LastActive:  "Discovered headers",
						RiskLevel:   "low",
						DeleteUrl:   database.GetDeleteUrl(domain),
						LoginDate:   time.Now().Format("2006-01-02"),
						AuthType:    "Password",
						RiskScore:   10,
						IsMock:      false,
					}
					discoveredAccounts = append(discoveredAccounts, d)
				}
			}
		}
		_ = <-done
	// Execute HaveIBeenPwned breach detection lookup
	hibpResults := checkHIBP(userID, emailVal)
	discoveredAccounts = append(discoveredAccounts, hibpResults...)

	c.Header("Content-Type", "text/html")
	var responseHTML strings.Builder
	for _, d := range discoveredAccounts {
		_ = tmplDiscoverRow.Execute(&responseHTML, d)
	}
	c.String(http.StatusOK, responseHTML.String())
}

func CleanupHandler(c *gin.Context) {
	if !validateCSRF(c) {
		c.String(http.StatusForbidden, "Forbidden: CSRF token mismatch")
		return
	}

	idParam := c.Query("id")
	if idParam == "" {
		c.String(http.StatusBadRequest, "Missing account ID")
		return
	}

	err := database.RemoveDiscoveredAccount(idParam)
	if err != nil {
		log.Printf("DB error on cleanup: %v", err)
		c.String(http.StatusInternalServerError, "DB error")
		return
	}
	c.Status(http.StatusOK)
}

func FilterTimelineHandler(c *gin.Context) {
	timeline := c.Query("timeline")
	subject, _ := c.Get("session_subject")
	emailSubject, _ := subject.(string)
	userID, _ := database.GetUserIDByEmail(emailSubject)
	discovered, err := database.FetchDiscoveredAccounts(userID, timeline)
	if err != nil {
		log.Printf("Fetch filtered discovered failed: %v", err)
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	c.Header("Content-Type", "text/html")
	var responseHTML strings.Builder
	for _, d := range discovered {
		_ = tmplDiscoverRow.Execute(&responseHTML, d)
	}

	if len(discovered) == 0 {
		responseHTML.WriteString(`
			<tr class="no-discovered-placeholder">
				<td colspan="7" style="text-align: center; color: var(--text-secondary); padding: 32px;">
					No scanning traces found matching this timeline filter.
				</td>
			</tr>
		`)
	}
	c.String(http.StatusOK, responseHTML.String())
}

// SaveSettingsHandler persists the Settings panel's IMAP host and session
// duration fields. Previously "Save Configuration" only ran a browser
// alert() - nothing was ever written anywhere, so the form was cosmetic.
// (Google Client ID/Secret stay env-only and read-only in the UI - those
// are credentials that shouldn't be editable/storable from the browser.)
func SaveSettingsHandler(c *gin.Context) {
	if !validateCSRF(c) {
		c.String(http.StatusForbidden, "Forbidden: CSRF token mismatch")
		return
	}

	imapHost := html.EscapeString(strings.TrimSpace(c.PostForm("imap_host")))
	sessionTTLVal := strings.TrimSpace(c.PostForm("session_ttl"))

	if imapHost == "" {
		c.String(http.StatusBadRequest, "Missing IMAP host")
		return
	}
	if _, err := time.ParseDuration(sessionTTLVal); err != nil {
		c.String(http.StatusBadRequest, "Invalid session duration (expected e.g. 1h, 8h, 24h)")
		return
	}

	if err := database.SetSetting("imap_host", imapHost); err != nil {
		log.Printf("Failed to save imap_host setting: %v", err)
		c.String(http.StatusInternalServerError, "Failed to save IMAP host")
		return
	}
	if err := database.SetSetting("session_ttl", sessionTTLVal); err != nil {
		log.Printf("Failed to save session_ttl setting: %v", err)
		c.String(http.StatusInternalServerError, "Failed to save session duration")
		return
	}

	c.String(http.StatusOK, "Settings saved.")
}

func checkHIBP(userID int64, email string) []database.DiscoveredAccount {
	var list []database.DiscoveredAccount
	apiKey := os.Getenv("HIBP_API_KEY")

	// If no API key is specified, skip real-time HIBP check (do not return simulated mock values to keep scan 100% real)
	if apiKey == "" {
		log.Println("No HIBP_API_KEY found in env. Skipping HIBP lookup to ensure real scan results only.")
		return list
	}

	// Real API request
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", "https://haveibeenpwned.com/api/v3/breachedaccount/"+email, nil)
	if err != nil {
		return list
	}
	req.Header.Set("hibp-api-key", apiKey)
	req.Header.Set("user-agent", "AccSpace-Security-App")

	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return list
	}
	defer resp.Body.Close()

	var breaches []struct {
		Name        string   `json:"Name"`
		Domain      string   `json:"Domain"`
		BreachDate  string   `json:"BreachDate"`
		DataClasses []string `json:"DataClasses"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&breaches); err != nil {
		return list
	}

	for _, b := range breaches {
		domain := b.Domain
		if domain == "" {
			domain = strings.ToLower(b.Name) + ".com"
		}
		score := 50
		risk := "medium"
		for _, dc := range b.DataClasses {
			if strings.ToLower(dc) == "passwords" {
				score = 90
				risk = "high"
			}
		}

		newID, _ := database.InsertDiscoveredAccount(userID, domain, email, "Breached", "Data Breach Check", risk, b.BreachDate, "Password (Exposed)", score, false)
		list = append(list, database.DiscoveredAccount{
			ID:          newID,
			Initials:    database.GetInitials(domain),
			ServiceName: domain,
			LoginEmail:  email,
			Status:      "Breached",
			LastActive:  "Data Breach Check",
			RiskLevel:   risk,
			DeleteUrl:   database.GetDeleteUrl(domain),
			LoginDate:   b.BreachDate,
			AuthType:    "Password (Exposed)",
			RiskScore:   score,
			IsMock:      false,
		})
	}
	return list
}
