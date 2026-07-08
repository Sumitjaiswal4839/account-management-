package backend

import (
	"log"
	"net/http"
	"os"

	"go-frontend/database"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/gin-gonic/gin"
)

// ScanInbox endpoint scans message subjects and returns JSON results.
//
// Previously this only ever read a single global EMAIL_USER/EMAIL_PASS/
// EMAIL_HOST from the environment, so every logged-in user (regardless of
// whose session it was) scanned the exact same fixed inbox - it had no
// connection to whoever the JWTAuth session actually belonged to. Now the
// request's own email/password/host (POST form fields) are used if
// provided, with the env vars only as a fallback default for local testing.
//
// These are read from the POST body rather than query params: query params
// end up in server access logs, browser history, and proxy/referrer logs,
// which is a bad place for a mailbox password to live.
func ScanInbox(c *gin.Context) {
	email := c.PostForm("email")
	if email == "" {
		email = os.Getenv("EMAIL_USER")
	}
	pass := c.PostForm("password")
	if pass == "" {
		pass = os.Getenv("EMAIL_PASS")
	}
	host := c.PostForm("host")
	if host == "" {
		host = os.Getenv("EMAIL_HOST")
	}

	if email == "" || pass == "" || host == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Missing IMAP credentials: POST email, password, host as form fields, " +
				"or set EMAIL_USER/EMAIL_PASS/EMAIL_HOST in the environment as a fallback",
		})
		return
	}

	subjectVal, _ := c.Get("session_subject")
	emailSubject, _ := subjectVal.(string)
	userID, _ := database.GetUserIDByEmail(emailSubject)
	log.Printf("ScanInbox requested by session %v (userID: %d) for mailbox %s", emailSubject, userID, email)

	cli, err := client.DialTLS(host, nil)
	if err != nil {
		log.Printf("IMAP Connection failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "IMAP server connection failed: " + err.Error()})
		return
	}
	defer cli.Logout()

	if err := cli.Login(email, pass); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "IMAP credentials unauthorized. If this is a Gmail/Google account with 2-Step " +
				"Verification enabled, you need a 16-character App Password (myaccount.google.com/apppasswords), " +
				"not your regular account password.",
		})
		return
	}

	// Select "All Mail" (Gmail default) to scan archive, falling back to INBOX
	_, err = cli.Select("[Gmail]/All Mail", false)
	if err != nil {
		_, err = cli.Select("INBOX", false)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read mailbox"})
		return
	}

	var messageIDs []uint32
	idSet := make(map[uint32]bool)

	subjectKeywords := []string{"Welcome", "Sign up", "account", "verify", "receipt", "order", "confirm", "password", "update", "invoice", "subscription"}
	for _, keyword := range subjectKeywords {
		crit := imap.NewSearchCriteria()
		crit.Header.Add("Subject", keyword)
		found, err := cli.Search(crit)
		if err != nil {
			continue
		}
		for _, id := range found {
			if !idSet[id] {
				idSet[id] = true
				messageIDs = append(messageIDs, id)
			}
		}
	}

	curatedDomains := []string{
		"spotify.com", "netflix.com", "linkedin.com", "facebook.com", "github.com",
		"instagram.com", "google.com", "amazon.com", "twitter.com", "zoom.us",
		"slack.com", "microsoft.com", "apple.com", "paypal.com", "yahoo.com",
		"bankofamerica.com", "chase.com", "walmart.com",
	}
	for _, d := range curatedDomains {
		crit := imap.NewSearchCriteria()
		crit.Header.Add("From", d)
		found, err := cli.Search(crit)
		if err != nil {
			continue
		}
		for _, id := range found {
			if !idSet[id] {
				idSet[id] = true
				messageIDs = append(messageIDs, id)
			}
		}
	}

	if len(messageIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"email": email, "results": []string{}})
		return
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(messageIDs...)

	messages := make(chan *imap.Message, 10)
	section := &imap.BodySectionName{}
	fetchDone := make(chan error, 1)
	// Fetch must run in its own goroutine: it blocks until the channel is
	// fully drained AND closed, so calling it synchronously (as before) would
	// deadlock the moment more than 10 messages matched (the buffer size),
	// since nothing would be reading from the channel while Fetch waits to
	// push into it.
	go func() {
		fetchDone <- cli.Fetch(seqset, []imap.FetchItem{imap.FetchEnvelope, section.FetchItem()}, messages)
	}()

	var results []map[string]string
	for msg := range messages {
		senderAddr := ""
		if len(msg.Envelope.From) > 0 {
			senderAddr = msg.Envelope.From[0].Address()
		}

		results = append(results, map[string]string{
			"From":    senderAddr,
			"Subject": msg.Envelope.Subject,
			"Date":    msg.Envelope.Date.String(),
		})

		// Log into accounts database dynamically
		if senderAddr != "" {
			domain := extractDomain(senderAddr)
			_, _ = database.InsertDiscoveredAccount(userID, domain, email, "Active", "IMAP scan", "low", msg.Envelope.Date.Format("2006-01-02"), "Password", 10, false)
		}
	}

	if err := <-fetchDone; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Message fetch failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"email":   email,
		"results": results,
	})
}
