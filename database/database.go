package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

var (
	DB          *sql.DB
	DbAvailable bool
)

type RowData struct {
	ID        int64
	Initials  string
	Name      string
	Phone     string
	Email     string
	Role      string
	RoleClass string
}

type DiscoveredAccount struct {
	ID          int64
	Initials    string
	ServiceName string
	LoginEmail  string
	Status      string
	LastActive  string
	RiskLevel   string
	DeleteUrl   string
	LoginDate   string
	AuthType    string
	RiskScore   int
	IsMock      bool // true when this row is demo/fallback data, not a real scan result
	UserID      int64
}

// Memory-backed fallbacks in case database connection fails
var (
	MemoryAccounts = []RowData{}
	MemoryDiscovered = []DiscoveredAccount{}
	// MemorySettings backs the Settings panel when Postgres isn't available.
	MemorySettings = map[string]string{
		"imap_host":   "imap.gmail.com:993",
		"session_ttl": "1h",
	}
)

// Initials helper
func GetInitials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "U"
	}
	initials := ""
	for i, part := range parts {
		if i >= 2 {
			break
		}
		if len(part) > 0 {
			initials += string(part[0])
		}
	}
	return strings.ToUpper(initials)
}

// Map service to deletion direct link
func GetDeleteUrl(domain string) string {
	domain = strings.ToLower(domain)
	mappings := map[string]string{
		"spotify.com":   "https://www.spotify.com/us/about-us/contact/close-account/",
		"netflix.com":   "https://www.netflix.com/CancelPlan",
		"linkedin.com":  "https://www.linkedin.com/help/linkedin/answer/a1337837",
		"facebook.com":  "https://www.facebook.com/help/delete_account",
		"github.com":    "https://github.com/settings/security",
		"instagram.com": "https://www.instagram.com/accounts/remove/request/permanent/",
		"google.com":    "https://myaccount.google.com/delete-services-or-account",
	}
	if url, exists := mappings[domain]; exists {
		return url
	}
	return "https://www.google.com/search?q=" + domain + "+delete+account+page"
}

// Connect PostgreSQL
func InitDB() {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://postgres:postgres@localhost:5432/accspace?sslmode=disable"
	}

	var err error
	DB, err = sql.Open("postgres", connStr)
	DbAvailable = true

	if err != nil {
		log.Printf("Warning: Failed to connect to database: %v. Running in memory mode.", err)
		DbAvailable = false
		return
	}

	err = DB.Ping()
	if err != nil {
		log.Printf("Warning: Database ping failed: %v. Running in memory mode.", err)
		DbAvailable = false
		return
	}

	log.Println("Connected to PostgreSQL successfully.")
	// Run schema migrations
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id BIGSERIAL PRIMARY KEY,
			email VARCHAR(100) UNIQUE NOT NULL,
			name VARCHAR(100),
			phone VARCHAR(50),
			role VARCHAR(50) DEFAULT 'Viewer',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		-- Upgrade path for DBs created before the "name" column existed
		-- (previously the display name was incorrectly stored in "phone").
		ALTER TABLE users ADD COLUMN IF NOT EXISTS name VARCHAR(100);

		CREATE TABLE IF NOT EXISTS accounts (
			id BIGSERIAL PRIMARY KEY,
			service_name VARCHAR(100) NOT NULL,
			login_email VARCHAR(100) NOT NULL,
			status VARCHAR(50) DEFAULT 'Active',
			last_active VARCHAR(100) DEFAULT 'Just now',
			risk_level VARCHAR(50) DEFAULT 'low',
			login_date DATE DEFAULT CURRENT_DATE,
			auth_type VARCHAR(50) DEFAULT 'Password',
			risk_score INT DEFAULT 10,
			is_mock BOOLEAN DEFAULT false,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		ALTER TABLE accounts ADD COLUMN IF NOT EXISTS is_mock BOOLEAN DEFAULT false;
		ALTER TABLE accounts ADD COLUMN IF NOT EXISTS user_id BIGINT REFERENCES users(id) ON DELETE CASCADE;

		CREATE TABLE IF NOT EXISTS cleanup_logs (
			id BIGSERIAL PRIMARY KEY,
			account_id BIGINT,
			action VARCHAR(100) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS app_settings (
			key VARCHAR(100) PRIMARY KEY,
			value VARCHAR(500) NOT NULL
		);
	`)
	if err != nil {
		log.Printf("Migration failed: %v", err)
	} else {
		log.Println("PostgreSQL auto-migration completed.")
		// Remove any default seeded users from the database if they exist
		_, _ = DB.Exec("DELETE FROM users WHERE email IN ('alice.v@accspace.io', 'bob.s@accspace.io', 'charlie.d@accspace.io')")
	}
}

// CRUD Operations

func FetchUsers() ([]RowData, error) {
	if !DbAvailable {
		return MemoryAccounts, nil
	}

	var users []RowData
	rows, err := DB.Query("SELECT id, email, name, phone, role FROM users ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var u RowData
		var name, phone, email, role sql.NullString
		var id int64
		if err := rows.Scan(&id, &email, &name, &phone, &role); err == nil {
			u.ID = id
			u.Name = name.String
			u.Phone = phone.String
			u.Email = email.String
			u.Role = role.String
			u.Initials = GetInitials(name.String)
			u.RoleClass = strings.ToLower(role.String)
			users = append(users, u)
		}
	}
	return users, nil
}

// InsertUser stores a managed account. Previously this took (email, phone,
// role) but was always called with the person's display NAME in the "phone"
// slot, and got saved into the phone column - there was no real name column
// and no way to actually capture a phone number. Now both are separate.
func InsertUser(email, name, phone, role string) (int64, error) {
	if !DbAvailable {
		newID := time.Now().UnixNano()
		MemoryAccounts = append([]RowData{{ID: newID, Initials: GetInitials(name), Name: name, Phone: phone, Email: email, Role: role, RoleClass: strings.ToLower(role)}}, MemoryAccounts...)
		return newID, nil
	}

	var newID int64
	err := DB.QueryRow(
		"INSERT INTO users (email, name, phone, role) VALUES ($1, $2, $3, $4) RETURNING id",
		email, name, phone, role,
	).Scan(&newID)
	return newID, err
}

// UpsertUserFromLogin creates (or updates the name on) a real, persistent
// user record every time someone logs in - via Google OAuth or the dev
// mock login. Previously a successful Google login only issued a session
// token; it never created any corresponding row in "users", so there was
// no durable link between "who logged in" and the Accounts table at all.
func UpsertUserFromLogin(email, name string) (int64, error) {
	if email == "" {
		return 0, fmt.Errorf("email required")
	}
	if name == "" {
		name = email
	}

	if !DbAvailable {
		for i, u := range MemoryAccounts {
			if strings.EqualFold(u.Email, email) {
				MemoryAccounts[i].Name = name
				MemoryAccounts[i].Initials = GetInitials(name)
				return MemoryAccounts[i].ID, nil
			}
		}
		newID := time.Now().UnixNano()
		MemoryAccounts = append([]RowData{{
			ID: newID, Initials: GetInitials(name), Name: name, Email: email,
			Role: "Viewer", RoleClass: "viewer",
		}}, MemoryAccounts...)
		return newID, nil
	}

	var id int64
	err := DB.QueryRow(`
		INSERT INTO users (email, name, role) VALUES ($1, $2, 'Viewer')
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, email, name).Scan(&id)
	return id, err
}

func DeleteUser(id string) error {
	if !DbAvailable {
		var newMemory []RowData
		for _, acc := range MemoryAccounts {
			if fmt.Sprintf("%d", acc.ID) != id {
				newMemory = append(newMemory, acc)
			}
		}
		MemoryAccounts = newMemory
		return nil
	}

	_, err := DB.Exec("DELETE FROM users WHERE id = $1", id)
	return err
}

func FetchDiscoveredAccounts(userID int64, timelineFilter string) ([]DiscoveredAccount, error) {
	if !DbAvailable {
		// Filter memory accounts by date and user
		var filtered []DiscoveredAccount
		for _, acc := range MemoryDiscovered {
			if acc.UserID != userID {
				continue
			}
			t, err := time.Parse("2006-01-02", acc.LoginDate)
			if err != nil {
				filtered = append(filtered, acc)
				continue
			}

			if timelineFilter == "last2years" {
				if t.Before(time.Now().AddDate(-2, 0, 0)) {
					continue
				}
			} else if timelineFilter != "" && timelineFilter != "all" {
				// specific year (e.g. 2024)
				if fmt.Sprintf("%d", t.Year()) != timelineFilter {
					continue
				}
			}
			filtered = append(filtered, acc)
		}
		return filtered, nil
	}

	var list []DiscoveredAccount
	var rows *sql.Rows
	var err error

	baseQuery := "SELECT id, service_name, login_email, status, last_active, risk_level, login_date, auth_type, risk_score, is_mock, user_id FROM accounts"

	if timelineFilter == "last2years" {
		rows, err = DB.Query(baseQuery+" WHERE user_id = $1 AND login_date >= CURRENT_DATE - INTERVAL '2 years' ORDER BY id DESC", userID)
	} else if timelineFilter != "" && timelineFilter != "all" {
		rows, err = DB.Query(baseQuery+" WHERE user_id = $1 AND EXTRACT(YEAR FROM login_date) = $2 ORDER BY id DESC", userID, timelineFilter)
	} else {
		rows, err = DB.Query(baseQuery+" WHERE user_id = $1 ORDER BY id DESC", userID)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var d DiscoveredAccount
		var loginTime time.Time
		if err := rows.Scan(&d.ID, &d.ServiceName, &d.LoginEmail, &d.Status, &d.LastActive, &d.RiskLevel, &loginTime, &d.AuthType, &d.RiskScore, &d.IsMock, &d.UserID); err == nil {
			d.Initials = GetInitials(d.ServiceName)
			d.DeleteUrl = GetDeleteUrl(d.ServiceName)
			d.LoginDate = loginTime.Format("2006-01-02")
			list = append(list, d)
		}
	}
	return list, nil
}

// InsertDiscoveredAccount stores a discovered account trace.
func InsertDiscoveredAccount(userID int64, service, email, status, lastActive, risk, loginDateStr, authType string, riskScore int, isMock bool) (int64, error) {
	// Parse date
	loginDate, err := time.Parse("2006-01-02", loginDateStr)
	if err != nil {
		loginDate = time.Now()
	}

	if cutoffYear := os.Getenv("ACCSPACE_LEGACY_CUTOFF_YEAR"); cutoffYear != "" {
		if cutoff, convErr := time.Parse("2006", cutoffYear); convErr == nil && loginDate.Before(cutoff) {
			lastActive = lastActive + " (Legacy: pre-" + cutoffYear + ")"
		}
	}

	newID := time.Now().UnixNano()
	if !DbAvailable {
		d := DiscoveredAccount{
			ID:          newID,
			Initials:    GetInitials(service),
			ServiceName: service,
			LoginEmail:  email,
			Status:      status,
			LastActive:  lastActive,
			RiskLevel:   risk,
			DeleteUrl:   GetDeleteUrl(service),
			LoginDate:   loginDate.Format("2006-01-02"),
			AuthType:    authType,
			RiskScore:   riskScore,
			IsMock:      isMock,
			UserID:      userID,
		}
		MemoryDiscovered = append(MemoryDiscovered, d)
		return newID, nil
	}

	err = DB.QueryRow(
		"INSERT INTO accounts (service_name, login_email, status, last_active, risk_level, login_date, auth_type, risk_score, is_mock, user_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id",
		service, email, status, lastActive, risk, loginDate, authType, riskScore, isMock, userID,
	).Scan(&newID)
	return newID, err
}

func RemoveDiscoveredAccount(id string) error {
	if !DbAvailable {
		var newMemory []DiscoveredAccount
		for _, acc := range MemoryDiscovered {
			if fmt.Sprintf("%d", acc.ID) != id {
				newMemory = append(newMemory, acc)
			}
		}
		MemoryDiscovered = newMemory
		return nil
	}

	_, err := DB.Exec("INSERT INTO cleanup_logs (account_id, action) VALUES ($1, 'Request GDPR/CCPA removal')", id)
	if err != nil {
		log.Printf("Failed to log cleanup: %v", err)
	}
	_, err = DB.Exec("DELETE FROM accounts WHERE id = $1", id)
	return err
}

// GetSetting reads a persisted Settings-panel value, falling back to
// defaultVal if it was never set. Previously the Settings panel's "Save
// Configuration" button only called alert() in the browser - nothing was
// ever actually saved anywhere.
func GetSetting(key, defaultVal string) string {
	if !DbAvailable {
		if v, ok := MemorySettings[key]; ok && v != "" {
			return v
		}
		return defaultVal
	}

	var value string
	err := DB.QueryRow("SELECT value FROM app_settings WHERE key = $1", key).Scan(&value)
	if err != nil || value == "" {
		return defaultVal
	}
	return value
}

// SetSetting persists a Settings-panel value.
func SetSetting(key, value string) error {
	if !DbAvailable {
		MemorySettings[key] = value
		return nil
	}

	_, err := DB.Exec(
		"INSERT INTO app_settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value",
		key, value,
	)
	return err
}

func GetUserIDByEmail(email string) (int64, error) {
	if !DbAvailable {
		// Look up in MemoryAccounts
		for _, u := range MemoryAccounts {
			if strings.EqualFold(u.Email, email) {
				return u.ID, nil
			}
		}
		// If not found, create a mock one
		newID := time.Now().UnixNano()
		MemoryAccounts = append(MemoryAccounts, RowData{ID: newID, Name: email, Email: email, Role: "Viewer", RoleClass: "viewer"})
		return newID, nil
	}

	var id int64
	err := DB.QueryRow("SELECT id FROM users WHERE email = $1", email).Scan(&id)
	if err != nil {
		// Auto-insert user if they don't exist yet (e.g. mock user or dev user)
		err = DB.QueryRow("INSERT INTO users (email, name, role) VALUES ($1, $2, 'Viewer') RETURNING id", email, email).Scan(&id)
	}
	return id, err
}

func GetUserByEmail(email string) (RowData, error) {
	if email == "" {
		return RowData{}, fmt.Errorf("email required")
	}

	if !DbAvailable {
		for _, u := range MemoryAccounts {
			if strings.EqualFold(u.Email, email) {
				return u, nil
			}
		}
		user := RowData{ID: time.Now().UnixNano(), Name: email, Email: email, Role: "Viewer", RoleClass: "viewer"}
		MemoryAccounts = append(MemoryAccounts, user)
		return user, nil
	}

	var row RowData
	var id int64
	var name, phone, role sql.NullString
	err := DB.QueryRow("SELECT id, email, name, phone, role FROM users WHERE email = $1", email).Scan(&id, &row.Email, &name, &phone, &role)
	if err != nil {
		if err == sql.ErrNoRows {
			if err = DB.QueryRow("INSERT INTO users (email, name, role) VALUES ($1, $2, 'Viewer') RETURNING id", email, email).Scan(&id); err != nil {
				return RowData{}, err
			}
			row.Email = email
			row.Name = email
			row.Role = "Viewer"
			row.RoleClass = "viewer"
			row.ID = id
			return row, nil
		}
		return RowData{}, err
	}

	row.ID = id
	row.Name = name.String
	row.Phone = phone.String
	row.Role = role.String
	row.RoleClass = strings.ToLower(role.String)
	if row.Name == "" {
		row.Name = row.Email
	}
	row.Initials = GetInitials(row.Name)
	return row, nil
}
