package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

const (
	defaultPort             = 8080
	defaultUA               = "clash-verge/v2.4.5"
	sessionTTL              = 7 * 24 * time.Hour
	usageCollectionInterval = time.Hour
)

const defaultDNSFakeIPFilter = `*.ntp.org
*.pool.ntp.org
pool.ntp.org
*.debian.pool.ntp.org
time.apple.com
time.google.com
time.cloudflare.com
ntp.aliyun.com
ntp.tencent.com
time1.cloud.tencent.com
time.edu.cn
time.neu.edu.cn`

// webAssets packages the frontend with the server so the binary can run from
// any working directory without separate static files.
//
//go:embed web/*
var webAssets embed.FS

type App struct {
	db                   *sql.DB
	static               http.Handler
	subscriptionClient   *http.Client
	publicSubscriptionMu sync.Mutex
}

type contextKey string

const userKey contextKey = "user"

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type settingsPayload struct {
	Port                  int    `json:"port"`
	BaseURL               string `json:"baseUrl"`
	DefaultUserAgent      string `json:"defaultUserAgent"`
	SchedulerEnabled      bool   `json:"schedulerEnabled"`
	DefaultInterval       int    `json:"defaultInterval"`
	MixedPort             int    `json:"mixedPort"`
	AllowLAN              bool   `json:"allowLan"`
	BindAddress           string `json:"bindAddress"`
	Mode                  string `json:"mode"`
	LogLevel              string `json:"logLevel"`
	DNSEnabled            bool   `json:"dnsEnabled"`
	DNSIPv6               bool   `json:"dnsIPv6"`
	DNSEnhancedMode       string `json:"dnsEnhancedMode"`
	DNSFakeIPRange        string `json:"dnsFakeIPRange"`
	DNSFakeIPFilter       string `json:"dnsFakeIPFilter"`
	DNSUseHosts           bool   `json:"dnsUseHosts"`
	DNSDefaultNameservers string `json:"dnsDefaultNameservers"`
	DNSNameservers        string `json:"dnsNameservers"`
	DNSFallback           string `json:"dnsFallback"`
	DNSPolicyGroupID      int64  `json:"dnsPolicyGroupId"`
	DNSFallbackGeoIP      bool   `json:"dnsFallbackGeoIP"`
	DNSFallbackIPCIDR     string `json:"dnsFallbackIPCIDR"`
}

type subscription struct {
	ID              int64   `json:"id"`
	Name            string  `json:"name"`
	URL             string  `json:"url"`
	Path            string  `json:"path"`
	UserAgent       string  `json:"userAgent"`
	MatchList       string  `json:"matchList"`
	MismatchList    string  `json:"mismatchList"`
	Enabled         bool    `json:"enabled"`
	UpdateMode      string  `json:"updateMode"`
	IntervalMinutes int     `json:"intervalMinutes"`
	LastUpdateAt    string  `json:"lastUpdateAt"`
	LastSuccessAt   string  `json:"lastSuccessAt"`
	LastError       string  `json:"lastError"`
	ProxyCount      int     `json:"proxyCount"`
	UsedGB          float64 `json:"usedGB"`
	TotalGB         float64 `json:"totalGB"`
	ExpireAt        string  `json:"expireAt"`
}

type subscriptionUsageSample struct {
	CollectedAt string  `json:"collectedAt"`
	UsedGB      float64 `json:"usedGB"`
	TotalGB     float64 `json:"totalGB"`
	DeltaGB     float64 `json:"deltaGB"`
	ExpireAt    string  `json:"expireAt"`
	Event       string  `json:"event"`
}

type subscriptionUsageSummary struct {
	TrackedGB       float64 `json:"trackedGB"`
	CurrentUsedGB   float64 `json:"currentUsedGB"`
	TotalGB         float64 `json:"totalGB"`
	SampleCount     int     `json:"sampleCount"`
	ResetCount      int     `json:"resetCount"`
	TrackingSince   string  `json:"trackingSince"`
	LastCollectedAt string  `json:"lastCollectedAt"`
}

type subscriptionUsageResponse struct {
	SubscriptionID   int64                     `json:"subscriptionId"`
	SubscriptionName string                    `json:"subscriptionName"`
	Summary          subscriptionUsageSummary  `json:"summary"`
	Samples          []subscriptionUsageSample `json:"samples"`
}

type proxyServer struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	Address    string `json:"address"`
	Port       int    `json:"port"`
	Credential string `json:"credential"`
	Transport  string `json:"transport"`
	SNI        string `json:"sni"`
	SkipVerify bool   `json:"skipVerify"`
	Enabled    bool   `json:"enabled"`
	Latency    string `json:"latency"`
}

type routingRule struct {
	ID            int64  `json:"id"`
	RuleType      string `json:"ruleType"`
	Match         string `json:"match"`
	Target        string `json:"target"`
	TargetGroupID int64  `json:"targetGroupId,omitempty"`
	Priority      int    `json:"priority"`
	Enabled       bool   `json:"enabled"`
}

type serviceRuleGroup struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	Target        string   `json:"target"`
	TargetGroupID int64    `json:"targetGroupId"`
	RuleCount     int      `json:"ruleCount"`
	Enabled       bool     `json:"enabled"`
	Priority      int      `json:"priority"`
	Rules         []string `json:"-"`
}

type ruleProvider struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	Behavior        string `json:"behavior"`
	Format          string `json:"format"`
	PrimaryURL      string `json:"primaryUrl"`
	BackupURL       string `json:"backupUrl"`
	Path            string `json:"path"`
	Enabled         bool   `json:"enabled"`
	UpdateMode      string `json:"updateMode"`
	IntervalMinutes int    `json:"interval"`
	LastUpdateAt    string `json:"lastUpdateAt"`
	LastError       string `json:"lastError"`
}

type accessKey struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Key           string  `json:"key,omitempty"`
	KeyPreview    string  `json:"keyPreview"`
	Enabled       bool    `json:"enabled"`
	MonthlyDataGB float64 `json:"monthlyDataGB"`
	LastUsedAt    string  `json:"lastUsedAt"`
	CreatedAt     string  `json:"createdAt"`
}

type proxyGroup struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Proxies []string `json:"proxies"`
	Enabled bool     `json:"enabled"`
}

type rawClashConfig struct {
	Proxies []map[string]any `yaml:"proxies"`
}

func main() {
	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "substore.db")+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	app := &App{db: db}
	if err := app.migrate(); err != nil {
		log.Fatal(err)
	}
	go app.scheduler()
	webRoot, err := fs.Sub(webAssets, "web")
	if err != nil {
		log.Fatal(err)
	}
	app.static = http.FileServer(http.FS(webRoot))
	port := app.settings().Port
	server := &http.Server{Addr: ":" + strconv.Itoa(port), Handler: app.routes(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("SubStore is running at http://localhost:%d", port)
	log.Fatal(server.ListenAndServe())
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/bootstrap", a.handleBootstrap)
	mux.HandleFunc("/api/auth/setup", a.handleSetup)
	mux.HandleFunc("/api/auth/login", a.handleLogin)
	mux.HandleFunc("/api/auth/logout", a.handleLogout)
	mux.HandleFunc("/api/auth/me", a.auth(a.handleMe))
	mux.HandleFunc("/api/auth/account", a.auth(a.handleAccount))
	mux.HandleFunc("/api/settings", a.auth(a.handleSettings))
	mux.HandleFunc("/api/subscriptions", a.auth(a.handleSubscriptions))
	mux.HandleFunc("/api/subscriptions/", a.auth(a.handleSubscriptionAction))
	mux.HandleFunc("/api/proxies", a.auth(a.handleProxies))
	mux.HandleFunc("/api/proxies/", a.auth(a.handleProxyAction))
	mux.HandleFunc("/api/rules", a.auth(a.handleRules))
	mux.HandleFunc("/api/rules/reorder", a.auth(a.handleRuleReorder))
	mux.HandleFunc("/api/rules/", a.auth(a.handleRuleAction))
	mux.HandleFunc("/api/service-rule-groups", a.auth(a.handleServiceRuleGroups))
	mux.HandleFunc("/api/service-rule-groups/", a.auth(a.handleServiceRuleGroupAction))
	mux.HandleFunc("/api/rule-providers", a.auth(a.handleRuleProviders))
	mux.HandleFunc("/api/rule-providers/", a.auth(a.handleRuleProviderAction))
	mux.HandleFunc("/api/groups", a.auth(a.handleGroups))
	mux.HandleFunc("/api/groups/", a.auth(a.handleGroupAction))
	mux.HandleFunc("/api/access-keys", a.auth(a.handleAccessKeys))
	mux.HandleFunc("/api/access-keys/", a.auth(a.handleAccessKeyAction))
	mux.HandleFunc("/api/config/preview", a.auth(a.handleConfigPreview))
	mux.HandleFunc("/sub/", a.handlePublicSubscription)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// The frontend is shipped with the Go service. Always revalidate static
		// assets so a deployment cannot leave browsers running an older app.js.
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		a.static.ServeHTTP(w, r)
	})
	return logging(mux)
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, safeLogPath(r.URL.Path), time.Since(start).Round(time.Millisecond))
	})
}

func safeLogPath(path string) string {
	if strings.HasPrefix(path, "/sub/") {
		return "/sub/[redacted]"
	}
	return path
}

func (a *App) migrate() error {
	_, err := a.db.Exec(`
CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'admin', enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, last_login_at TEXT);
CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, user_id INTEGER NOT NULL, expires_at TEXT NOT NULL, created_at TEXT NOT NULL, FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS subscriptions (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, url TEXT NOT NULL, path TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL, match_list TEXT NOT NULL DEFAULT '', mismatch_list TEXT NOT NULL DEFAULT '', used_gb REAL NOT NULL DEFAULT 0, total_gb REAL NOT NULL DEFAULT 0, expire_at TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1, update_mode TEXT NOT NULL DEFAULT 'manual', interval_minutes INTEGER NOT NULL DEFAULT 1440, last_update_at TEXT, last_success_at TEXT, last_error TEXT, raw_content TEXT, proxy_count INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS proxies (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, protocol TEXT NOT NULL, address TEXT NOT NULL, port INTEGER NOT NULL, credential TEXT NOT NULL, transport TEXT NOT NULL DEFAULT 'TCP', sni TEXT, skip_verify INTEGER NOT NULL DEFAULT 0, enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS rules (id INTEGER PRIMARY KEY AUTOINCREMENT, rule_type TEXT NOT NULL, match_value TEXT NOT NULL, target TEXT NOT NULL, target_group_id INTEGER, priority INTEGER NOT NULL DEFAULT 100, enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS service_rule_groups (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, rules_json TEXT NOT NULL DEFAULT '[]', target_group_id INTEGER NOT NULL, priority INTEGER NOT NULL DEFAULT 55, enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS rule_providers (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, provider_type TEXT NOT NULL DEFAULT 'http', behavior TEXT NOT NULL DEFAULT 'classical', provider_format TEXT NOT NULL DEFAULT 'yaml', primary_url TEXT NOT NULL, backup_url TEXT, path TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1, update_mode TEXT NOT NULL DEFAULT 'manual', interval_minutes INTEGER NOT NULL DEFAULT 86400, last_update_at TEXT, last_error TEXT, content TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS access_keys (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, key_hash TEXT NOT NULL UNIQUE, key_value TEXT NOT NULL DEFAULT '', key_preview TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, monthly_data_gb REAL NOT NULL DEFAULT 0, last_used_at TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS proxy_groups (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, group_type TEXT NOT NULL, proxies_json TEXT NOT NULL DEFAULT '[]', enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS subscription_usage_sources (id INTEGER PRIMARY KEY AUTOINCREMENT, url_hash TEXT NOT NULL UNIQUE, last_attempt_at TEXT NOT NULL DEFAULT '', last_collected_at TEXT NOT NULL DEFAULT '', last_used_gb REAL NOT NULL DEFAULT 0, last_total_gb REAL NOT NULL DEFAULT 0, last_expire_at TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS subscription_usage_samples (id INTEGER PRIMARY KEY AUTOINCREMENT, source_id INTEGER NOT NULL, collected_at TEXT NOT NULL, used_gb REAL NOT NULL, total_gb REAL NOT NULL DEFAULT 0, delta_gb REAL NOT NULL DEFAULT 0, expire_at TEXT NOT NULL DEFAULT '', event TEXT NOT NULL DEFAULT 'normal', FOREIGN KEY(source_id) REFERENCES subscription_usage_sources(id) ON DELETE CASCADE);
CREATE INDEX IF NOT EXISTS idx_subscription_usage_samples_source_time ON subscription_usage_samples(source_id, collected_at DESC);
`)
	if err != nil {
		return err
	}
	for _, column := range []string{"match_list TEXT NOT NULL DEFAULT ''", "mismatch_list TEXT NOT NULL DEFAULT ''"} {
		if _, alterErr := a.db.Exec(`ALTER TABLE subscriptions ADD COLUMN ` + column); alterErr != nil && !strings.Contains(alterErr.Error(), "duplicate column name") {
			return alterErr
		}
	}
	for _, column := range []string{"used_gb REAL NOT NULL DEFAULT 0", "total_gb REAL NOT NULL DEFAULT 0"} {
		if _, alterErr := a.db.Exec(`ALTER TABLE subscriptions ADD COLUMN ` + column); alterErr != nil && !strings.Contains(alterErr.Error(), "duplicate column name") {
			return alterErr
		}
	}
	for _, column := range []string{"expire_at TEXT NOT NULL DEFAULT ''"} {
		if _, alterErr := a.db.Exec(`ALTER TABLE subscriptions ADD COLUMN ` + column); alterErr != nil && !strings.Contains(alterErr.Error(), "duplicate column name") {
			return alterErr
		}
	}
	for _, column := range []string{"path TEXT NOT NULL DEFAULT ''"} {
		if _, alterErr := a.db.Exec(`ALTER TABLE subscriptions ADD COLUMN ` + column); alterErr != nil && !strings.Contains(alterErr.Error(), "duplicate column name") {
			return alterErr
		}
	}
	for _, column := range []string{"provider_type TEXT NOT NULL DEFAULT 'http'", "behavior TEXT NOT NULL DEFAULT 'classical'", "provider_format TEXT NOT NULL DEFAULT 'yaml'", "path TEXT NOT NULL DEFAULT ''"} {
		if _, alterErr := a.db.Exec(`ALTER TABLE rule_providers ADD COLUMN ` + column); alterErr != nil && !strings.Contains(alterErr.Error(), "duplicate column name") {
			return alterErr
		}
	}
	for _, column := range []string{"key_value TEXT NOT NULL DEFAULT ''"} {
		if _, alterErr := a.db.Exec(`ALTER TABLE access_keys ADD COLUMN ` + column); alterErr != nil && !strings.Contains(alterErr.Error(), "duplicate column name") {
			return alterErr
		}
	}
	for _, column := range []string{"monthly_data_gb REAL NOT NULL DEFAULT 0"} {
		if _, alterErr := a.db.Exec(`ALTER TABLE access_keys ADD COLUMN ` + column); alterErr != nil && !strings.Contains(alterErr.Error(), "duplicate column name") {
			return alterErr
		}
	}
	for _, column := range []string{"target_group_id INTEGER"} {
		if _, alterErr := a.db.Exec(`ALTER TABLE rules ADD COLUMN ` + column); alterErr != nil && !strings.Contains(alterErr.Error(), "duplicate column name") {
			return alterErr
		}
	}
	if err := a.backfillSubscriptionPaths(); err != nil {
		return err
	}
	if err := a.migrateLegacyGroupReferences(); err != nil {
		return err
	}
	if err := a.backfillRuleGroupReferences(); err != nil {
		return err
	}
	defaults := map[string]string{"port": "8080", "base_url": "http://localhost:8080", "default_user_agent": defaultUA, "scheduler_enabled": "true", "default_interval": "1440", "dns_fake_ip_filter": defaultDNSFakeIPFilter}
	for key, value := range defaults {
		_, err = a.db.Exec(`INSERT OR IGNORE INTO settings(key,value) VALUES(?,?)`, key, value)
		if err != nil {
			return err
		}
	}
	if err := a.seedHomeIPRedditRules(); err != nil {
		return err
	}
	if err := a.seedAppleIntelligenceRouting(); err != nil {
		return err
	}
	if err := a.seedRedirHostDNSRouting(); err != nil {
		return err
	}
	if err := a.migrateAppleIntelligenceTextRules(); err != nil {
		return err
	}
	if err := a.migrateAppleIntelligenceServiceGroup(); err != nil {
		return err
	}
	if err := a.migrateAppleIntelligenceProxyGroup(); err != nil {
		return err
	}
	if err := a.seedServiceRuleGroups(); err != nil {
		return err
	}
	if err := a.migratePreferredRoutingGroupOrder(); err != nil {
		return err
	}
	return nil
}

// migratePreferredRoutingGroupOrder makes new clients start with the intended
// routes while preserving any additional members the user already configured.
// Clash clients can still choose another member at any time.
func (a *App) migratePreferredRoutingGroupOrder() error {
	const migrationKey = "migration_preferred_routing_group_order_v1"
	var completed string
	if err := a.db.QueryRow(`SELECT value FROM settings WHERE key=?`, migrationKey).Scan(&completed); err == nil && completed == "true" {
		return nil
	}

	var cheapSubscriptionID, defaultGroupID, homeGroupID, redditGroupID int64
	if err := a.db.QueryRow(`SELECT id FROM subscriptions WHERE name='光喵' AND enabled=1 ORDER BY id LIMIT 1`).Scan(&cheapSubscriptionID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if err := a.db.QueryRow(`SELECT id FROM proxy_groups WHERE LOWER(name) IN ('default','cheap') AND enabled=1 ORDER BY CASE WHEN LOWER(name)='cheap' THEN 0 ELSE 1 END,id LIMIT 1`).Scan(&defaultGroupID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if err := a.db.QueryRow(`SELECT id FROM proxy_groups WHERE LOWER(name)='homeip' AND enabled=1 ORDER BY id LIMIT 1`).Scan(&homeGroupID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if err := a.db.QueryRow(`SELECT id FROM proxy_groups WHERE LOWER(name)='reddit' AND enabled=1 ORDER BY id LIMIT 1`).Scan(&redditGroupID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	reorder := func(raw string, preferred []string) string {
		var existing []string
		_ = json.Unmarshal([]byte(raw), &existing)
		seen := map[string]bool{}
		ordered := make([]string, 0, len(existing)+len(preferred)+1)
		appendMember := func(member string) {
			if member != "" && member != "DIRECT" && !seen[member] {
				seen[member] = true
				ordered = append(ordered, member)
			}
		}
		for _, member := range preferred {
			appendMember(member)
		}
		for _, member := range existing {
			appendMember(member)
		}
		ordered = append(ordered, "DIRECT")
		encoded, _ := json.Marshal(ordered)
		return string(encoded)
	}

	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range []struct {
		id        int64
		preferred []string
	}{
		{defaultGroupID, []string{fmt.Sprintf("subscription-id:%d", cheapSubscriptionID)}},
		{redditGroupID, []string{fmt.Sprintf("group-id:%d", homeGroupID), fmt.Sprintf("group-id:%d", defaultGroupID)}},
	} {
		var raw string
		if err = tx.QueryRow(`SELECT proxies_json FROM proxy_groups WHERE id=?`, item.id).Scan(&raw); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE proxy_groups SET proxies_json=? WHERE id=?`, reorder(raw, item.preferred), item.id); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES(?, 'true') ON CONFLICT(key) DO UPDATE SET value='true'`, migrationKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) backfillRuleGroupReferences() error {
	_, err := a.db.Exec(`UPDATE rules
		SET target_group_id=(SELECT id FROM proxy_groups WHERE proxy_groups.name=rules.target ORDER BY id LIMIT 1)
		WHERE target_group_id IS NULL
		AND EXISTS (SELECT 1 FROM proxy_groups WHERE proxy_groups.name=rules.target)`)
	return err
}

func (a *App) seedHomeIPRedditRules() error {
	const migrationKey = "migration_homeip_reddit_v1"
	var completed string
	if err := a.db.QueryRow(`SELECT value FROM settings WHERE key=?`, migrationKey).Scan(&completed); err == nil && completed == "true" {
		return nil
	}
	var groupID int64
	var groupName string
	if err := a.db.QueryRow(`SELECT id,name FROM proxy_groups WHERE UPPER(name)='HOMEIP' ORDER BY id LIMIT 1`).Scan(&groupID, &groupName); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for index, domain := range []string{"reddit.com", "redd.it", "redditstatic.com", "redditmedia.com", "reddithelp.com"} {
		var existingID int64
		err = tx.QueryRow(`SELECT id FROM rules WHERE rule_type='DOMAIN-SUFFIX' AND LOWER(match_value)=LOWER(?) ORDER BY id LIMIT 1`, domain).Scan(&existingID)
		if err == nil {
			if _, err = tx.Exec(`UPDATE rules SET target=?,target_group_id=? WHERE id=?`, groupName, groupID, existingID); err != nil {
				return err
			}
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO rules(rule_type,match_value,target,target_group_id,priority,enabled,created_at) VALUES('DOMAIN-SUFFIX',?,?,?,?,1,?)`, domain, groupName, groupID, 55+index, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES(?, 'true') ON CONFLICT(key) DO UPDATE SET value='true'`, migrationKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) seedAppleIntelligenceRouting() error {
	const migrationKey = "migration_apple_intelligence_routing_v1"
	var completed string
	if err := a.db.QueryRow(`SELECT value FROM settings WHERE key=?`, migrationKey).Scan(&completed); err == nil && completed == "true" {
		return nil
	}
	var homeID int64
	var homeName string
	if err := a.db.QueryRow(`SELECT id,name FROM proxy_groups WHERE LOWER(name)='homeip' AND enabled=1 ORDER BY id LIMIT 1`).Scan(&homeID, &homeName); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	type providerDefinition struct {
		name       string
		behavior   string
		format     string
		primaryURL string
		backupURL  string
	}
	providers := []providerDefinition{
		{
			name:       "apple-intelligence",
			behavior:   "domain",
			format:     "yaml",
			primaryURL: "https://cdn.jsdelivr.net/gh/Accademia/Additional_Rule_For_Clash@main/AppleAI/AppleAI_Domain.yaml",
			backupURL:  "https://raw.githubusercontent.com/Accademia/Additional_Rule_For_Clash/main/AppleAI/AppleAI_Domain.yaml",
		},
		{name: "icloud", behavior: "domain", format: "text", primaryURL: "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/icloud.txt"},
		{name: "apple", behavior: "domain", format: "text", primaryURL: "https://cdn.jsdelivr.net/gh/Loyalsoldier/clash-rules@release/apple.txt"},
	}
	for _, provider := range providers {
		path := defaultRuleProviderPath(provider.name)
		var id int64
		err = tx.QueryRow(`SELECT id FROM rule_providers WHERE LOWER(name)=LOWER(?) ORDER BY id LIMIT 1`, provider.name).Scan(&id)
		if err == sql.ErrNoRows {
			if _, err = tx.Exec(`INSERT INTO rule_providers(name,provider_type,behavior,provider_format,primary_url,backup_url,path,enabled,update_mode,interval_minutes,created_at) VALUES(?,'http',?,?,?,?,?,1,'manual',86400,?)`, provider.name, provider.behavior, provider.format, provider.primaryURL, provider.backupURL, path, now); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if provider.name == "apple-intelligence" {
			if _, err = tx.Exec(`UPDATE rule_providers SET name=?,provider_type='http',behavior=?,provider_format=?,primary_url=?,backup_url=?,path=?,enabled=1,interval_minutes=86400 WHERE id=?`, provider.name, provider.behavior, provider.format, provider.primaryURL, provider.backupURL, path, id); err != nil {
				return err
			}
		}
	}

	type ruleDefinition struct {
		match         string
		target        string
		targetGroupID any
		priority      int
	}
	rules := []ruleDefinition{
		{match: "apple-intelligence", target: homeName, targetGroupID: homeID, priority: 51},
		{match: "icloud", target: "DIRECT", priority: 56},
		{match: "apple", target: "DIRECT", priority: 57},
	}
	for _, rule := range rules {
		var id int64
		err = tx.QueryRow(`SELECT id FROM rules WHERE UPPER(rule_type)='RULE-SET' AND LOWER(match_value)=LOWER(?) ORDER BY id LIMIT 1`, rule.match).Scan(&id)
		if err == sql.ErrNoRows {
			if _, err = tx.Exec(`INSERT INTO rules(rule_type,match_value,target,target_group_id,priority,enabled,created_at) VALUES('RULE-SET',?,?,?,?,1,?)`, rule.match, rule.target, rule.targetGroupID, rule.priority, now); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if _, err = tx.Exec(`UPDATE rules SET target=?,target_group_id=?,priority=?,enabled=1 WHERE id=?`, rule.target, rule.targetGroupID, rule.priority, id); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES(?, 'true') ON CONFLICT(key) DO UPDATE SET value='true'`, migrationKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) seedRedirHostDNSRouting() error {
	const migrationKey = "migration_redir_host_dns_v1"
	var completed string
	if err := a.db.QueryRow(`SELECT value FROM settings WHERE key=?`, migrationKey).Scan(&completed); err == nil && completed == "true" {
		return nil
	}
	var policyGroupID int64
	if err := a.db.QueryRow(`SELECT id FROM proxy_groups WHERE LOWER(name) IN ('default','cheap') AND enabled=1 ORDER BY CASE WHEN LOWER(name)='default' THEN 0 ELSE 1 END,id LIMIT 1`).Scan(&policyGroupID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	const providerName = "geosite-cn"
	const primaryURL = "https://cdn.jsdelivr.net/gh/Accademia/Additional_Rule_For_Clash@main/GeositeCN/GeositeCN_Domain.yaml"
	const backupURL = "https://raw.githubusercontent.com/Accademia/Additional_Rule_For_Clash/main/GeositeCN/GeositeCN_Domain.yaml"
	var providerID int64
	err = tx.QueryRow(`SELECT id FROM rule_providers WHERE LOWER(name)=LOWER(?) ORDER BY id LIMIT 1`, providerName).Scan(&providerID)
	if err == sql.ErrNoRows {
		if _, err = tx.Exec(`INSERT INTO rule_providers(name,provider_type,behavior,provider_format,primary_url,backup_url,path,enabled,update_mode,interval_minutes,created_at) VALUES(?,'http','domain','yaml',?,?,?,1,'manual',86400,?)`, providerName, primaryURL, backupURL, defaultRuleProviderPath(providerName), now); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if _, err = tx.Exec(`UPDATE rule_providers SET name=?,provider_type='http',behavior='domain',provider_format='yaml',primary_url=?,backup_url=?,path=?,enabled=1,interval_minutes=86400 WHERE id=?`, providerName, primaryURL, backupURL, defaultRuleProviderPath(providerName), providerID); err != nil {
		return err
	}

	var ruleID int64
	err = tx.QueryRow(`SELECT id FROM rules WHERE UPPER(rule_type)='GEOIP' AND UPPER(match_value)='CN' ORDER BY id LIMIT 1`).Scan(&ruleID)
	if err == sql.ErrNoRows {
		if _, err = tx.Exec(`INSERT INTO rules(rule_type,match_value,target,target_group_id,priority,enabled,created_at) VALUES('GEOIP','CN','DIRECT',NULL,58,1,?)`, now); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if _, err = tx.Exec(`UPDATE rules SET target='DIRECT',target_group_id=NULL,priority=58,enabled=1 WHERE id=?`, ruleID); err != nil {
		return err
	}

	settings := map[string]string{
		"dns_enhanced_mode":   "redir-host",
		"dns_nameservers":     "https://dns.alidns.com/dns-query, https://doh.pub/dns-query",
		"dns_fallback":        "https://cloudflare-dns.com/dns-query, https://dns.google/dns-query",
		"dns_policy_group_id": strconv.FormatInt(policyGroupID, 10),
	}
	for key, value := range settings {
		if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES(?, 'true') ON CONFLICT(key) DO UPDATE SET value='true'`, migrationKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) migrateAppleIntelligenceTextRules() error {
	const migrationKey = "migration_apple_intelligence_text_rules_v1"
	var completed string
	if err := a.db.QueryRow(`SELECT value FROM settings WHERE key=?`, migrationKey).Scan(&completed); err == nil && completed == "true" {
		return nil
	}
	result, err := a.db.Exec(`UPDATE rule_providers SET behavior='classical',provider_format='text',primary_url=?,backup_url='',path=? WHERE LOWER(name)='apple-intelligence'`,
		"https://raw.githubusercontent.com/ddgksf2013/Filter/refs/heads/master/AppleIntelligence.list", defaultRuleProviderPath("apple-intelligence"))
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil
	}
	_, err = a.db.Exec(`INSERT INTO settings(key,value) VALUES(?, 'true') ON CONFLICT(key) DO UPDATE SET value='true'`, migrationKey)
	return err
}

func (a *App) migrateAppleIntelligenceServiceGroup() error {
	const migrationKey = "migration_apple_intelligence_service_group_v1"
	var completed string
	if err := a.db.QueryRow(`SELECT value FROM settings WHERE key=?`, migrationKey).Scan(&completed); err == nil && completed == "true" {
		return nil
	}
	var homeID int64
	if err := a.db.QueryRow(`SELECT id FROM proxy_groups WHERE LOWER(name)='homeip' AND enabled=1 ORDER BY id LIMIT 1`).Scan(&homeID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	var providerCount int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM rule_providers WHERE LOWER(name)='apple-intelligence' AND enabled=1`).Scan(&providerCount); err != nil {
		return err
	}
	if providerCount == 0 {
		return nil
	}

	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rulesJSON, _ := json.Marshal([]string{"RULE-SET,apple-intelligence"})
	var serviceGroupID int64
	err = tx.QueryRow(`SELECT id FROM service_rule_groups WHERE LOWER(name)='apple intelligence' ORDER BY id LIMIT 1`).Scan(&serviceGroupID)
	if err == sql.ErrNoRows {
		if _, err = tx.Exec(`INSERT INTO service_rule_groups(name,rules_json,target_group_id,priority,enabled,created_at) VALUES('Apple Intelligence',?,?,51,1,?)`, string(rulesJSON), homeID, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if _, err = tx.Exec(`UPDATE service_rule_groups SET rules_json=?,target_group_id=?,priority=51,enabled=1 WHERE id=?`, string(rulesJSON), homeID, serviceGroupID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM rules WHERE UPPER(rule_type)='RULE-SET' AND LOWER(match_value)='apple-intelligence'`); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES(?, 'true') ON CONFLICT(key) DO UPDATE SET value='true'`, migrationKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) migrateAppleIntelligenceProxyGroup() error {
	const migrationKey = "migration_apple_intelligence_proxy_group_v1"
	var completed string
	if err := a.db.QueryRow(`SELECT value FROM settings WHERE key=?`, migrationKey).Scan(&completed); err == nil && completed == "true" {
		return nil
	}
	var homeID, defaultID int64
	if err := a.db.QueryRow(`SELECT id FROM proxy_groups WHERE LOWER(name)='homeip' AND enabled=1 ORDER BY id LIMIT 1`).Scan(&homeID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if err := a.db.QueryRow(`SELECT id FROM proxy_groups WHERE LOWER(name) IN ('default','cheap') AND enabled=1 ORDER BY CASE WHEN LOWER(name)='default' THEN 0 ELSE 1 END,id LIMIT 1`).Scan(&defaultID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var groupID int64
	err = tx.QueryRow(`SELECT id FROM proxy_groups WHERE LOWER(name)='apple intelligence' ORDER BY id LIMIT 1`).Scan(&groupID)
	if err == sql.ErrNoRows {
		membersJSON, _ := json.Marshal([]string{fmt.Sprintf("group-id:%d", homeID), fmt.Sprintf("group-id:%d", defaultID), "DIRECT"})
		result, insertErr := tx.Exec(`INSERT INTO proxy_groups(name,group_type,proxies_json,enabled,created_at) VALUES('Apple Intelligence','select',?,1,?)`, string(membersJSON), time.Now().UTC().Format(time.RFC3339))
		if insertErr != nil {
			return insertErr
		}
		groupID, _ = result.LastInsertId()
	} else if err != nil {
		return err
	} else {
		membersJSON, _ := json.Marshal([]string{fmt.Sprintf("group-id:%d", homeID), fmt.Sprintf("group-id:%d", defaultID), "DIRECT"})
		if _, err = tx.Exec(`UPDATE proxy_groups SET group_type='select',proxies_json=?,enabled=1 WHERE id=?`, string(membersJSON), groupID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`UPDATE service_rule_groups SET target_group_id=? WHERE LOWER(name)='apple intelligence'`, groupID); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES(?, 'true') ON CONFLICT(key) DO UPDATE SET value='true'`, migrationKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) seedServiceRuleGroups() error {
	const migrationKey = "migration_service_rule_groups_v1"
	var completed string
	if err := a.db.QueryRow(`SELECT value FROM settings WHERE key=?`, migrationKey).Scan(&completed); err == nil && completed == "true" {
		return nil
	}
	var defaultID, homeID int64
	if err := a.db.QueryRow(`SELECT id FROM proxy_groups WHERE LOWER(name) IN ('default','cheap') AND enabled=1 ORDER BY CASE WHEN LOWER(name)='cheap' THEN 0 ELSE 1 END,id LIMIT 1`).Scan(&defaultID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if err := a.db.QueryRow(`SELECT id FROM proxy_groups WHERE LOWER(name)='homeip' AND enabled=1 ORDER BY id LIMIT 1`).Scan(&homeID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	type definition struct {
		name     string
		priority int
		members  []string
	}
	definitions := []definition{
		{name: "Reddit", priority: 52, members: []string{fmt.Sprintf("group-id:%d", homeID), fmt.Sprintf("group-id:%d", defaultID), "DIRECT"}},
		{name: "AI", priority: 53, members: []string{fmt.Sprintf("group-id:%d", homeID), fmt.Sprintf("group-id:%d", defaultID), "DIRECT"}},
		{name: "Netflix", priority: 54, members: []string{fmt.Sprintf("group-id:%d", homeID), fmt.Sprintf("group-id:%d", defaultID), "DIRECT"}},
		{name: "YouTube", priority: 54, members: []string{fmt.Sprintf("group-id:%d", defaultID), fmt.Sprintf("group-id:%d", homeID), "DIRECT"}},
		{name: "DisneyPlus", priority: 54, members: []string{fmt.Sprintf("group-id:%d", homeID), fmt.Sprintf("group-id:%d", defaultID), "DIRECT"}},
		{name: "Game", priority: 55, members: []string{"DIRECT", fmt.Sprintf("group-id:%d", defaultID), fmt.Sprintf("group-id:%d", homeID)}},
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range definitions {
		var proxyGroupID int64
		err = tx.QueryRow(`SELECT id FROM proxy_groups WHERE LOWER(name)=LOWER(?) ORDER BY id LIMIT 1`, item.name).Scan(&proxyGroupID)
		if err == sql.ErrNoRows {
			membersJSON, _ := json.Marshal(item.members)
			result, insertErr := tx.Exec(`INSERT INTO proxy_groups(name,group_type,proxies_json,enabled,created_at) VALUES(?,'select',?,1,?)`, item.name, string(membersJSON), time.Now().UTC().Format(time.RFC3339))
			if insertErr != nil {
				return insertErr
			}
			proxyGroupID, _ = result.LastInsertId()
		} else if err != nil {
			return err
		}
		rulesJSON, _ := json.Marshal(defaultServiceRules[item.name])
		var existingID int64
		err = tx.QueryRow(`SELECT id FROM service_rule_groups WHERE LOWER(name)=LOWER(?)`, item.name).Scan(&existingID)
		if err == sql.ErrNoRows {
			if _, err = tx.Exec(`INSERT INTO service_rule_groups(name,rules_json,target_group_id,priority,enabled,created_at) VALUES(?,?,?,?,1,?)`, item.name, string(rulesJSON), proxyGroupID, item.priority, time.Now().UTC().Format(time.RFC3339)); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	for _, rule := range defaultServiceRules["Reddit"] {
		typeAndMatch := strings.SplitN(rule, ",", 2)
		if len(typeAndMatch) == 2 {
			if _, err = tx.Exec(`DELETE FROM rules WHERE rule_type=? AND LOWER(match_value)=LOWER(?)`, typeAndMatch[0], typeAndMatch[1]); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES(?, 'true') ON CONFLICT(key) DO UPDATE SET value='true'`, migrationKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) backfillSubscriptionPaths() error {
	rows, err := a.db.Query(`SELECT id,name FROM subscriptions`)
	if err != nil {
		return err
	}
	type subRow struct {
		id   int64
		name string
	}
	var items []subRow
	for rows.Next() {
		var r subRow
		if err := rows.Scan(&r.id, &r.name); err != nil {
			rows.Close()
			return err
		}
		items = append(items, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := a.db.Exec(`UPDATE subscriptions SET path=? WHERE id=?`, defaultSubscriptionPath(item.name), item.id); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) migrateLegacyGroupReferences() error {
	subscriptionIDs := map[string]int64{}
	for _, item := range a.listSubscriptions() {
		subscriptionIDs[item.Name] = item.ID
	}
	groups := a.listGroups()
	groupIDs := map[string]int64{}
	for _, item := range groups {
		groupIDs[item.Name] = item.ID
	}
	proxyIDs := map[string]int64{}
	rows, err := a.db.Query(`SELECT id,name FROM proxies`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return err
		}
		proxyIDs[name] = id
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, group := range groups {
		changed := false
		members := make([]string, 0, len(group.Proxies))
		for _, member := range group.Proxies {
			if strings.HasPrefix(member, "subscription:") {
				if id, ok := subscriptionIDs[strings.TrimSpace(strings.TrimPrefix(member, "subscription:"))]; ok {
					member = fmt.Sprintf("subscription-id:%d", id)
					changed = true
				}
			} else if strings.HasPrefix(member, "group:") {
				if id, ok := groupIDs[strings.TrimSpace(strings.TrimPrefix(member, "group:"))]; ok {
					member = fmt.Sprintf("group-id:%d", id)
					changed = true
				}
			} else if id, ok := proxyIDs[member]; ok {
				member = fmt.Sprintf("proxy-id:%d", id)
				changed = true
			}
			members = append(members, member)
		}
		if changed {
			raw, _ := json.Marshal(members)
			if _, err := a.db.Exec(`UPDATE proxy_groups SET proxies_json=? WHERE id=?`, string(raw), group.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) settings() settingsPayload {
	result := settingsPayload{Port: defaultPort, BaseURL: "http://localhost:8080", DefaultUserAgent: defaultUA, SchedulerEnabled: true, DefaultInterval: 1440, MixedPort: 7890, AllowLAN: false, BindAddress: "*", Mode: "rule", LogLevel: "info", DNSEnabled: true, DNSIPv6: false, DNSEnhancedMode: "fake-ip", DNSFakeIPRange: "198.18.0.1/16", DNSFakeIPFilter: defaultDNSFakeIPFilter, DNSUseHosts: true, DNSDefaultNameservers: "223.5.5.5, 119.29.29.29", DNSNameservers: "https://doh.pub/dns-query, https://dns.alidns.com/dns-query", DNSFallback: "https://doh-pure.onedns.net/dns-query, https://ada.openbld.net/dns-query", DNSFallbackGeoIP: true, DNSFallbackIPCIDR: "240.0.0.0/4, 0.0.0.0/32"}
	rows, err := a.db.Query(`SELECT key,value FROM settings`)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if rows.Scan(&key, &value) != nil {
			continue
		}
		switch key {
		case "port":
			result.Port, _ = strconv.Atoi(value)
		case "base_url":
			result.BaseURL = value
		case "default_user_agent":
			result.DefaultUserAgent = value
		case "scheduler_enabled":
			result.SchedulerEnabled = value == "true"
		case "default_interval":
			result.DefaultInterval, _ = strconv.Atoi(value)
		case "mixed_port":
			result.MixedPort, _ = strconv.Atoi(value)
		case "allow_lan":
			result.AllowLAN = value == "true"
		case "bind_address":
			result.BindAddress = value
		case "mode":
			result.Mode = value
		case "log_level":
			result.LogLevel = value
		case "dns_enabled":
			result.DNSEnabled = value == "true"
		case "dns_ipv6":
			result.DNSIPv6 = value == "true"
		case "dns_enhanced_mode":
			result.DNSEnhancedMode = value
		case "dns_fake_ip_range":
			result.DNSFakeIPRange = value
		case "dns_fake_ip_filter":
			result.DNSFakeIPFilter = value
		case "dns_use_hosts":
			result.DNSUseHosts = value == "true"
		case "dns_default_nameservers":
			result.DNSDefaultNameservers = value
		case "dns_nameservers":
			result.DNSNameservers = value
		case "dns_fallback":
			result.DNSFallback = value
		case "dns_policy_group_id":
			result.DNSPolicyGroupID, _ = strconv.ParseInt(value, 10, 64)
		case "dns_fallback_geoip":
			result.DNSFallbackGeoIP = value == "true"
		case "dns_fallback_ipcidr":
			result.DNSFallbackIPCIDR = value
		}
	}
	if configuredPort := envInt("SUBSTORE_PORT", 0); configuredPort > 0 {
		result.Port = configuredPort
	}
	if configuredBaseURL := strings.TrimRight(os.Getenv("SUBSTORE_BASE_URL"), "/"); configuredBaseURL != "" {
		result.BaseURL = configuredBaseURL
	} else {
		result.BaseURL = fmt.Sprintf("http://localhost:%d", result.Port)
	}
	return result
}

func (a *App) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	var count int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	writeJSON(w, http.StatusOK, map[string]any{"setupRequired": count == 0, "settings": a.settings()})
}

func (a *App) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var count int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	if count != 0 {
		writeError(w, http.StatusConflict, "Admin account already exists")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) || len(strings.TrimSpace(input.Username)) < 2 || len(input.Password) < 8 {
		writeError(w, http.StatusBadRequest, "Username must be at least 2 characters and password at least 8 characters")
		return
	}
	hash, err := hashPassword(input.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := a.db.Exec(`INSERT INTO users(username,password_hash,created_at) VALUES(?,?,?)`, strings.TrimSpace(input.Username), hash, now)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Could not create admin account")
		return
	}
	id, _ := result.LastInsertId()
	user := User{ID: id, Username: strings.TrimSpace(input.Username), Role: "admin"}
	a.startSession(w, r, user.ID)
	writeJSON(w, http.StatusCreated, user)
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	var user User
	var passwordHash string
	var enabled bool
	err := a.db.QueryRow(`SELECT id,username,role,password_hash,enabled FROM users WHERE username=?`, input.Username).Scan(&user.ID, &user.Username, &user.Role, &passwordHash, &enabled)
	if err != nil || !enabled || !verifyPassword(input.Password, passwordHash) {
		writeError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}
	_, _ = a.db.Exec(`UPDATE users SET last_login_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339), user.ID)
	a.startSession(w, r, user.ID)
	writeJSON(w, http.StatusOK, user)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("substore_session"); err == nil {
		_, _ = a.db.Exec(`DELETE FROM sessions WHERE id=?`, cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "substore_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, r.Context().Value(userKey))
}

func (a *App) handleAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Username           string `json:"username"`
		CurrentPassword    string `json:"currentPassword"`
		NewPassword        string `json:"newPassword"`
		ConfirmNewPassword string `json:"confirmNewPassword"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if len(input.Username) < 2 {
		writeError(w, http.StatusBadRequest, "Username must be at least 2 characters")
		return
	}
	if input.NewPassword != "" && len(input.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "New password must be at least 8 characters")
		return
	}
	if input.NewPassword != input.ConfirmNewPassword {
		writeError(w, http.StatusBadRequest, "New passwords do not match")
		return
	}
	user, ok := r.Context().Value(userKey).(User)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	var passwordHash string
	if err := a.db.QueryRow(`SELECT password_hash FROM users WHERE id=?`, user.ID).Scan(&passwordHash); err != nil || !verifyPassword(input.CurrentPassword, passwordHash) {
		writeError(w, http.StatusUnauthorized, "Current password is incorrect")
		return
	}
	if input.NewPassword != "" {
		var err error
		passwordHash, err = hashPassword(input.NewPassword)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec(`UPDATE users SET username=?,password_hash=? WHERE id=?`, input.Username, passwordHash, user.ID); err != nil {
		writeError(w, http.StatusConflict, "Username is already in use")
		return
	}
	if input.NewPassword != "" {
		if cookie, cookieErr := r.Cookie("substore_session"); cookieErr == nil {
			if _, err = tx.Exec(`DELETE FROM sessions WHERE user_id=? AND id<>?`, user.ID, cookie.Value); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	user.Username = input.Username
	writeJSON(w, http.StatusOK, user)
}

func (a *App) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("substore_session")
		if err != nil {
			writeError(w, http.StatusUnauthorized, "Authentication required")
			return
		}
		var user User
		var expires string
		err = a.db.QueryRow(`SELECT u.id,u.username,u.role,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id=? AND u.enabled=1`, cookie.Value).Scan(&user.ID, &user.Username, &user.Role, &expires)
		if err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusUnauthorized, "Authentication required")
			} else {
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		until, _ := time.Parse(time.RFC3339, expires)
		if until.Before(time.Now()) {
			writeError(w, http.StatusUnauthorized, "Session expired")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	}
}

func (a *App) startSession(w http.ResponseWriter, r *http.Request, userID int64) {
	token := randomToken(32)
	now := time.Now().UTC()
	_, _ = a.db.Exec(`INSERT INTO sessions(id,user_id,expires_at,created_at) VALUES(?,?,?,?)`, token, userID, now.Add(sessionTTL).Format(time.RFC3339), now.Format(time.RFC3339))
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{Name: "substore_session", Value: token, Path: "/", Expires: now.Add(sessionTTL), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, a.settings())
		return
	}
	if r.Method != http.MethodPatch {
		methodNotAllowed(w)
		return
	}
	var input settingsPayload
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.MixedPort < 1 || input.MixedPort > 65535 || !validClashMode(input.Mode) || !validLogLevel(input.LogLevel) || !validDNSEnhancedMode(input.DNSEnhancedMode) {
		writeError(w, http.StatusBadRequest, "Invalid client configuration")
		return
	}
	if input.DNSPolicyGroupID > 0 {
		var count int
		if err := a.db.QueryRow(`SELECT COUNT(*) FROM proxy_groups WHERE id=? AND enabled=1`, input.DNSPolicyGroupID).Scan(&count); err != nil || count != 1 {
			writeError(w, http.StatusBadRequest, "DNS proxy group not found")
			return
		}
	}
	values := map[string]string{"default_user_agent": strings.TrimSpace(input.DefaultUserAgent), "scheduler_enabled": strconv.FormatBool(input.SchedulerEnabled), "default_interval": strconv.Itoa(input.DefaultInterval), "mixed_port": strconv.Itoa(input.MixedPort), "allow_lan": strconv.FormatBool(input.AllowLAN), "bind_address": strings.TrimSpace(input.BindAddress), "mode": input.Mode, "log_level": input.LogLevel, "dns_enabled": strconv.FormatBool(input.DNSEnabled), "dns_ipv6": strconv.FormatBool(input.DNSIPv6), "dns_enhanced_mode": input.DNSEnhancedMode, "dns_fake_ip_range": strings.TrimSpace(input.DNSFakeIPRange), "dns_fake_ip_filter": strings.TrimSpace(input.DNSFakeIPFilter), "dns_use_hosts": strconv.FormatBool(input.DNSUseHosts), "dns_default_nameservers": strings.TrimSpace(input.DNSDefaultNameservers), "dns_nameservers": strings.TrimSpace(input.DNSNameservers), "dns_fallback": strings.TrimSpace(input.DNSFallback), "dns_policy_group_id": strconv.FormatInt(input.DNSPolicyGroupID, 10), "dns_fallback_geoip": strconv.FormatBool(input.DNSFallbackGeoIP), "dns_fallback_ipcidr": strings.TrimSpace(input.DNSFallbackIPCIDR)}
	for key, value := range values {
		_, _ = a.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	}
	writeJSON(w, http.StatusOK, a.settings())
}

func (a *App) handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, a.listSubscriptions())
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input subscription
	if !decodeJSON(w, r, &input) || strings.TrimSpace(input.Name) == "" || !validHTTPURL(input.URL) {
		writeError(w, http.StatusBadRequest, "Name and a valid http(s) URL are required")
		return
	}
	if input.UserAgent == "" {
		input.UserAgent = a.settings().DefaultUserAgent
	}
	normalizeSubscription(&input)
	if a.subscriptionNameExists(input.Name, 0) {
		writeError(w, http.StatusConflict, "Subscription name already exists")
		return
	}
	if input.UpdateMode == "" {
		input.UpdateMode = "manual"
	}
	if input.IntervalMinutes < 1 {
		input.IntervalMinutes = a.settings().DefaultInterval
	}
	result, err := a.db.Exec(`INSERT INTO subscriptions(name,url,path,user_agent,match_list,mismatch_list,used_gb,total_gb,expire_at,enabled,update_mode,interval_minutes,created_at) VALUES(?,?,?,?,?,?,0,0,'',?,?,?,?)`, input.Name, input.URL, input.Path, input.UserAgent, input.MatchList, input.MismatchList, boolInt(input.Enabled || input.ID == 0), input.UpdateMode, input.IntervalMinutes, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.ID, _ = result.LastInsertId()
	writeJSON(w, http.StatusCreated, input)
}

func (a *App) listSubscriptions() []subscription {
	rows, err := a.db.Query(`SELECT id,name,url,COALESCE(path,''),user_agent,COALESCE(match_list,''),COALESCE(mismatch_list,''),used_gb,total_gb,COALESCE(expire_at,''),enabled,update_mode,interval_minutes,COALESCE(last_update_at,''),COALESCE(last_success_at,''),COALESCE(last_error,''),proxy_count FROM subscriptions ORDER BY id DESC`)
	if err != nil {
		return []subscription{}
	}
	defer rows.Close()
	result := []subscription{}
	for rows.Next() {
		var item subscription
		var enabled int
		_ = rows.Scan(&item.ID, &item.Name, &item.URL, &item.Path, &item.UserAgent, &item.MatchList, &item.MismatchList, &item.UsedGB, &item.TotalGB, &item.ExpireAt, &enabled, &item.UpdateMode, &item.IntervalMinutes, &item.LastUpdateAt, &item.LastSuccessAt, &item.LastError, &item.ProxyCount)
		normalizeSubscription(&item)
		item.Enabled = enabled == 1
		result = append(result, item)
	}
	return result
}

func (a *App) subscriptionNameExists(name string, excludeID int64) bool {
	var count int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM subscriptions WHERE LOWER(TRIM(name))=LOWER(TRIM(?)) AND id<>?`, name, excludeID).Scan(&count)
	return count > 0
}

func memberReferencesRecord(member, kind string, id int64, name string) bool {
	member = strings.TrimSpace(member)
	switch kind {
	case "subscription":
		return member == fmt.Sprintf("subscription-id:%d", id) || member == "subscription:"+name
	case "proxy group":
		return member == fmt.Sprintf("group-id:%d", id) || member == "group:"+name
	case "manual proxy":
		// Manual proxies currently use their names in group membership. Keep the
		// ID form here as well so the guard remains correct after ID migration.
		return member == name || member == fmt.Sprintf("proxy-id:%d", id)
	default:
		return false
	}
}

func (a *App) dependentProxyGroups(kind string, id int64, name string) []string {
	dependencies := []string{}
	for _, group := range a.listGroups() {
		if kind == "proxy group" && group.ID == id {
			continue
		}
		for _, member := range group.Proxies {
			if memberReferencesRecord(member, kind, id, name) {
				dependencies = append(dependencies, fmt.Sprintf("proxy group %q", group.Name))
				break
			}
		}
	}
	return dependencies
}

func dependencyConflictMessage(kind, name string, dependencies []string) string {
	return fmt.Sprintf("Cannot delete %s %q. Used by: %s. Remove it from those items first.", kind, name, strings.Join(dependencies, ", "))
}

func writeDependencyConflict(w http.ResponseWriter, kind, name string, dependencies []string) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":        dependencyConflictMessage(kind, name, dependencies),
		"kind":         kind,
		"name":         name,
		"dependencies": dependencies,
	})
}

func (a *App) handleSubscriptionAction(w http.ResponseWriter, r *http.Request) {
	isUpdate := strings.HasSuffix(r.URL.Path, "/update")
	isUsage := strings.HasSuffix(r.URL.Path, "/usage")
	path := strings.TrimSuffix(strings.TrimSuffix(r.URL.Path, "/update"), "/usage")
	id, ok := pathID(path, "/api/subscriptions/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet && isUsage {
		a.handleSubscriptionUsage(w, id)
		return
	}
	if r.Method == http.MethodPost && isUpdate {
		a.updateSubscription(w, id)
		return
	}
	if r.Method == http.MethodDelete {
		var name string
		if err := a.db.QueryRow(`SELECT name FROM subscriptions WHERE id=?`, id).Scan(&name); err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "Subscription not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if dependencies := a.dependentProxyGroups("subscription", id, name); len(dependencies) > 0 {
			writeDependencyConflict(w, "subscription", name, dependencies)
			return
		}
		_, err := a.db.Exec(`DELETE FROM subscriptions WHERE id=?`, id)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodPatch {
		var input subscription
		if !decodeJSON(w, r, &input) || strings.TrimSpace(input.Name) == "" || !validHTTPURL(input.URL) {
			writeError(w, http.StatusBadRequest, "Name and a valid http(s) URL are required")
			return
		}
		if input.UserAgent == "" {
			input.UserAgent = a.settings().DefaultUserAgent
		}
		normalizeSubscription(&input)
		if a.subscriptionNameExists(input.Name, id) {
			writeError(w, http.StatusConflict, "Subscription name already exists")
			return
		}
		if input.UpdateMode == "" {
			input.UpdateMode = "manual"
		}
		if input.IntervalMinutes < 1 {
			input.IntervalMinutes = a.settings().DefaultInterval
		}
		_, err := a.db.Exec(`UPDATE subscriptions SET name=?,url=?,path=?,user_agent=?,match_list=?,mismatch_list=?,enabled=?,update_mode=?,interval_minutes=? WHERE id=?`, input.Name, input.URL, input.Path, input.UserAgent, input.MatchList, input.MismatchList, boolInt(input.Enabled), input.UpdateMode, input.IntervalMinutes, id)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	methodNotAllowed(w)
}

func (a *App) updateSubscription(w http.ResponseWriter, id int64) {
	var item subscription
	var enabled int
	err := a.db.QueryRow(`SELECT id,name,url,COALESCE(path,''),user_agent,match_list,mismatch_list,enabled,update_mode,interval_minutes FROM subscriptions WHERE id=?`, id).Scan(&item.ID, &item.Name, &item.URL, &item.Path, &item.UserAgent, &item.MatchList, &item.MismatchList, &enabled, &item.UpdateMode, &item.IntervalMinutes)
	if err != nil {
		writeError(w, 404, "Subscription not found")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	client := a.subscriptionClient
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	req, _ := http.NewRequest(http.MethodGet, item.URL, nil)
	req.Header.Set("User-Agent", item.UserAgent)
	response, err := client.Do(req)
	if err != nil {
		_, _ = a.db.Exec(`UPDATE subscriptions SET last_update_at=?,last_error=? WHERE id=?`, now, err.Error(), id)
		writeError(w, 502, "Subscription update failed: "+err.Error())
		return
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if readErr != nil || response.StatusCode >= 400 {
		message := fmt.Sprintf("upstream status %d", response.StatusCode)
		if readErr != nil {
			message = readErr.Error()
		}
		_, _ = a.db.Exec(`UPDATE subscriptions SET last_update_at=?,last_error=? WHERE id=?`, now, message, id)
		writeError(w, 502, "Subscription update failed: "+message)
		return
	}
	filtered := filterSubscriptionContent(body, item.MatchList, item.MismatchList)
	_ = a.storeSubscriptionSnapshot(id, now, body, filtered, response.Header.Get("Subscription-Userinfo"))
	writeJSON(w, 200, map[string]any{"ok": true, "updatedAt": now, "proxyCount": len(parseSubscriptionProxies(filtered))})
}

type subscriptionUserInfo struct {
	UsedGB, TotalGB               float64
	ExpireAt                      string
	HasUsage, HasTotal, HasExpire bool
}

func parseSubscriptionUserInfo(header string) subscriptionUserInfo {
	values := map[string]uint64{}
	for _, field := range strings.Split(header, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(field), "=")
		if !found {
			continue
		}
		parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		if err == nil {
			values[strings.ToLower(strings.TrimSpace(key))] = parsed
		}
	}
	upload, hasUpload := values["upload"]
	download, hasDownload := values["download"]
	total, hasTotal := values["total"]
	expire, hasExpire := values["expire"]
	const gib = float64(1024 * 1024 * 1024)
	result := subscriptionUserInfo{UsedGB: (float64(upload) + float64(download)) / gib, TotalGB: float64(total) / gib, HasUsage: hasUpload || hasDownload, HasTotal: hasTotal, HasExpire: hasExpire}
	if expire > 0 {
		result.ExpireAt = time.Unix(int64(expire), 0).UTC().Format(time.RFC3339)
	}
	return result
}

func subscriptionUsageURLHash(rawURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(rawURL)))
	return hex.EncodeToString(sum[:])
}

func (a *App) claimUsageCollection(rawURL, now string) (bool, error) {
	hash := subscriptionUsageURLHash(rawURL)
	if _, err := a.db.Exec(`INSERT OR IGNORE INTO subscription_usage_sources(url_hash,created_at) VALUES(?,?)`, hash, now); err != nil {
		return false, err
	}
	nowTime, err := time.Parse(time.RFC3339, now)
	if err != nil {
		return false, err
	}
	cutoff := nowTime.Add(-usageCollectionInterval).Format(time.RFC3339)
	result, err := a.db.Exec(`UPDATE subscription_usage_sources SET last_attempt_at=? WHERE url_hash=? AND (last_attempt_at='' OR last_attempt_at<=?) AND (last_collected_at='' OR last_collected_at<=?)`, now, hash, cutoff, cutoff)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

func (a *App) recordSubscriptionUsage(rawURL, collectedAt, header string, minimumInterval time.Duration) error {
	usage := parseSubscriptionUserInfo(header)
	if !usage.HasUsage {
		return nil
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	hash := subscriptionUsageURLHash(rawURL)
	if _, err = tx.Exec(`INSERT OR IGNORE INTO subscription_usage_sources(url_hash,created_at) VALUES(?,?)`, hash, collectedAt); err != nil {
		return err
	}
	var sourceID int64
	var lastCollected, lastExpire string
	var lastUsed, lastTotal float64
	if err = tx.QueryRow(`SELECT id,last_collected_at,last_used_gb,last_total_gb,last_expire_at FROM subscription_usage_sources WHERE url_hash=?`, hash).Scan(&sourceID, &lastCollected, &lastUsed, &lastTotal, &lastExpire); err != nil {
		return err
	}
	collectedTime, parseErr := time.Parse(time.RFC3339, collectedAt)
	if parseErr != nil {
		return parseErr
	}
	if previousTime, previousErr := time.Parse(time.RFC3339, lastCollected); previousErr == nil {
		if !collectedTime.After(previousTime) {
			return nil
		}
		if collectedTime.Sub(previousTime) < minimumInterval {
			if _, err = tx.Exec(`UPDATE subscriptions SET used_gb=?,total_gb=CASE WHEN ? THEN ? ELSE total_gb END,expire_at=CASE WHEN ? THEN ? ELSE expire_at END WHERE url=?`, usage.UsedGB, usage.HasTotal, usage.TotalGB, usage.HasExpire, usage.ExpireAt, rawURL); err != nil {
				return err
			}
			return tx.Commit()
		}
	}
	total := lastTotal
	if usage.HasTotal {
		total = usage.TotalGB
	}
	expire := lastExpire
	if usage.HasExpire {
		expire = usage.ExpireAt
	}
	delta := float64(0)
	event := "baseline"
	if lastCollected != "" {
		event = "normal"
		if usage.UsedGB >= lastUsed {
			delta = usage.UsedGB - lastUsed
		} else {
			// Provider counters commonly drop after a billing-cycle renewal. Treat
			// the new counter as usage in a new segment, never as a negative delta.
			delta = usage.UsedGB
			event = "counter-reset"
			if (usage.HasExpire && expire != lastExpire) || (usage.HasTotal && total != lastTotal) {
				event = "plan-renewed"
			}
		}
	}
	if _, err = tx.Exec(`INSERT INTO subscription_usage_samples(source_id,collected_at,used_gb,total_gb,delta_gb,expire_at,event) VALUES(?,?,?,?,?,?,?)`, sourceID, collectedAt, usage.UsedGB, total, delta, expire, event); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE subscription_usage_sources SET last_collected_at=?,last_used_gb=?,last_total_gb=?,last_expire_at=? WHERE id=?`, collectedAt, usage.UsedGB, total, expire, sourceID); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE subscriptions SET used_gb=?,total_gb=CASE WHEN ? THEN ? ELSE total_gb END,expire_at=CASE WHEN ? THEN ? ELSE expire_at END WHERE url=?`, usage.UsedGB, usage.HasTotal, usage.TotalGB, usage.HasExpire, usage.ExpireAt, rawURL); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) handleSubscriptionUsage(w http.ResponseWriter, id int64) {
	result := subscriptionUsageResponse{SubscriptionID: id, Samples: []subscriptionUsageSample{}}
	var rawURL string
	if err := a.db.QueryRow(`SELECT name,url FROM subscriptions WHERE id=?`, id).Scan(&result.SubscriptionName, &rawURL); err != nil {
		writeError(w, http.StatusNotFound, "Subscription not found")
		return
	}
	var sourceID int64
	err := a.db.QueryRow(`SELECT id,last_used_gb,last_total_gb,last_collected_at FROM subscription_usage_sources WHERE url_hash=?`, subscriptionUsageURLHash(rawURL)).Scan(&sourceID, &result.Summary.CurrentUsedGB, &result.Summary.TotalGB, &result.Summary.LastCollectedAt)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, result)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.db.QueryRow(`SELECT COALESCE(SUM(delta_gb),0),COUNT(*),COALESCE(SUM(CASE WHEN event IN ('counter-reset','plan-renewed') THEN 1 ELSE 0 END),0),COALESCE(MIN(collected_at),'') FROM subscription_usage_samples WHERE source_id=?`, sourceID).Scan(&result.Summary.TrackedGB, &result.Summary.SampleCount, &result.Summary.ResetCount, &result.Summary.TrackingSince)
	rows, err := a.db.Query(`SELECT collected_at,used_gb,total_gb,delta_gb,expire_at,event FROM subscription_usage_samples WHERE source_id=? ORDER BY collected_at DESC LIMIT 720`, sourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var sample subscriptionUsageSample
		if rows.Scan(&sample.CollectedAt, &sample.UsedGB, &sample.TotalGB, &sample.DeltaGB, &sample.ExpireAt, &sample.Event) == nil {
			result.Samples = append(result.Samples, sample)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) storeSubscriptionSnapshot(id int64, now string, body []byte, filtered, userInfo string) error {
	proxyCount := len(parseSubscriptionProxies(filtered))
	usage := parseSubscriptionUserInfo(userInfo)
	_, err := a.db.Exec(`UPDATE subscriptions SET last_update_at=?,last_success_at=?,last_error='',raw_content=?,proxy_count=?,
		used_gb=CASE WHEN ? THEN ? ELSE used_gb END,
		total_gb=CASE WHEN ? THEN ? ELSE total_gb END,
		expire_at=CASE WHEN ? THEN ? ELSE expire_at END WHERE id=?`,
		now, now, string(body), proxyCount, usage.HasUsage, usage.UsedGB, usage.HasTotal, usage.TotalGB, usage.HasExpire, usage.ExpireAt, id)
	if err != nil {
		return err
	}
	var rawURL string
	if err := a.db.QueryRow(`SELECT url FROM subscriptions WHERE id=?`, id).Scan(&rawURL); err != nil {
		return err
	}
	return a.recordSubscriptionUsage(rawURL, now, userInfo, usageCollectionInterval)
}

func (a *App) scheduler() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		a.collectSubscriptionUsage()
		settings := a.settings()
		if !settings.SchedulerEnabled {
			continue
		}
		rows, err := a.db.Query(`SELECT id,update_mode,interval_minutes,COALESCE(last_update_at,'') FROM subscriptions WHERE enabled=1 AND update_mode NOT IN ('manual','disabled')`)
		if err != nil {
			continue
		}
		type schedRow struct {
			id       int64
			mode     string
			interval int64
			last     string
		}
		var items []schedRow
		for rows.Next() {
			var r schedRow
			if rows.Scan(&r.id, &r.mode, &r.interval, &r.last) != nil {
				continue
			}
			items = append(items, r)
		}
		rows.Close()
		for _, r := range items {
			interval := r.interval
			if interval < 1 {
				interval = int64(settings.DefaultInterval)
			}
			lastAt, parseErr := time.Parse(time.RFC3339, r.last)
			if parseErr == nil && time.Since(lastAt) < time.Duration(interval)*time.Minute {
				continue
			}
			if err := a.refreshSubscription(r.id); err != nil {
				log.Printf("scheduled subscription update %d failed: %v", r.id, err)
			}
		}
	}
}

func (a *App) collectSubscriptionUsage() {
	rows, err := a.db.Query(`SELECT s.url,s.user_agent FROM subscriptions s WHERE s.enabled=1 AND s.id=(SELECT MIN(first.id) FROM subscriptions first WHERE first.enabled=1 AND first.url=s.url) ORDER BY s.id`)
	if err != nil {
		return
	}
	type source struct{ url, userAgent string }
	sources := []source{}
	for rows.Next() {
		var item source
		if rows.Scan(&item.url, &item.userAgent) == nil {
			sources = append(sources, item)
		}
	}
	rows.Close()
	client := a.subscriptionClient
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	for _, item := range sources {
		now := time.Now().UTC().Format(time.RFC3339)
		due, claimErr := a.claimUsageCollection(item.url, now)
		if claimErr != nil || !due {
			continue
		}
		req, requestErr := http.NewRequest(http.MethodGet, item.url, nil)
		if requestErr != nil {
			continue
		}
		if item.userAgent == "" {
			item.userAgent = a.settings().DefaultUserAgent
		}
		req.Header.Set("User-Agent", item.userAgent)
		response, requestErr := client.Do(req)
		if requestErr != nil {
			log.Printf("subscription usage collection failed: %v", requestErr)
			continue
		}
		_ = response.Body.Close()
		if response.StatusCode >= http.StatusBadRequest {
			log.Printf("subscription usage collection returned status %d", response.StatusCode)
			continue
		}
		if err := a.recordSubscriptionUsage(item.url, now, response.Header.Get("Subscription-Userinfo"), 0); err != nil {
			log.Printf("subscription usage recording failed: %v", err)
		}
	}
}

func (a *App) refreshSubscription(id int64) error {
	var item subscription
	var enabled int
	if err := a.db.QueryRow(`SELECT id,name,url,COALESCE(path,''),user_agent,match_list,mismatch_list,enabled,update_mode,interval_minutes FROM subscriptions WHERE id=?`, id).Scan(&item.ID, &item.Name, &item.URL, &item.Path, &item.UserAgent, &item.MatchList, &item.MismatchList, &enabled, &item.UpdateMode, &item.IntervalMinutes); err != nil {
		return err
	}
	normalizeSubscription(&item)
	now := time.Now().UTC().Format(time.RFC3339)
	client := a.subscriptionClient
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, item.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", item.UserAgent)
	response, err := client.Do(req)
	if err != nil {
		_, _ = a.db.Exec(`UPDATE subscriptions SET last_update_at=?,last_error=? WHERE id=?`, now, err.Error(), id)
		return err
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if readErr != nil || response.StatusCode >= 400 {
		message := fmt.Sprintf("upstream status %d", response.StatusCode)
		if readErr != nil {
			message = readErr.Error()
		}
		_, _ = a.db.Exec(`UPDATE subscriptions SET last_update_at=?,last_error=? WHERE id=?`, now, message, id)
		return fmt.Errorf("%s", message)
	}
	filtered := filterSubscriptionContent(body, item.MatchList, item.MismatchList)
	return a.storeSubscriptionSnapshot(id, now, body, filtered, response.Header.Get("Subscription-Userinfo"))
}

func (a *App) refreshAllSubscriptions() {
	items := a.listSubscriptions()
	var updates sync.WaitGroup
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		updates.Add(1)
		go func(id int64) {
			defer updates.Done()
			if err := a.refreshSubscription(id); err != nil {
				log.Printf("on-demand subscription update %d failed: %v", id, err)
			}
		}(item.ID)
	}
	updates.Wait()
}

func (a *App) handleProxies(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, 200, a.allProxies())
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input proxyServer
	if !decodeJSON(w, r, &input) || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Address) == "" || input.Port < 1 || input.Port > 65535 || input.Credential == "" {
		writeError(w, 400, "Name, address, port, and credential are required")
		return
	}
	if input.Transport == "" {
		input.Transport = "TCP"
	}
	result, err := a.db.Exec(`INSERT INTO proxies(name,protocol,address,port,credential,transport,sni,skip_verify,enabled,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, input.Name, input.Protocol, input.Address, input.Port, input.Credential, input.Transport, input.SNI, boolInt(input.SkipVerify), boolInt(input.Enabled || input.ID == 0), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	input.ID, _ = result.LastInsertId()
	input.Enabled = true
	writeJSON(w, 201, input)
}

func (a *App) listProxies() []proxyServer {
	rows, err := a.db.Query(`SELECT id,name,protocol,address,port,credential,transport,COALESCE(sni,''),skip_verify,enabled FROM proxies ORDER BY id DESC`)
	if err != nil {
		return []proxyServer{}
	}
	defer rows.Close()
	result := []proxyServer{}
	for rows.Next() {
		var x proxyServer
		var skip, enabled int
		_ = rows.Scan(&x.ID, &x.Name, &x.Protocol, &x.Address, &x.Port, &x.Credential, &x.Transport, &x.SNI, &skip, &enabled)
		x.SkipVerify = skip == 1
		x.Enabled = enabled == 1
		x.Latency = "—"
		result = append(result, x)
	}
	return result
}

func (a *App) allProxies() []proxyServer {
	result := a.listProxies()
	rows, err := a.db.Query(`SELECT id,name,COALESCE(match_list,''),COALESCE(mismatch_list,''),COALESCE(raw_content,'') FROM subscriptions WHERE enabled=1 AND raw_content IS NOT NULL AND raw_content <> ''`)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var subscriptionID int64
		var subscriptionName, matchList, mismatchList, raw string
		if rows.Scan(&subscriptionID, &subscriptionName, &matchList, &mismatchList, &raw) != nil {
			continue
		}
		for index, imported := range parseSubscriptionProxies(filterSubscriptionContent([]byte(raw), matchList, mismatchList)) {
			imported.ID = -((subscriptionID * 10000) + int64(index) + 1)
			imported.Name = subscriptionName + " / " + imported.Name
			imported.Enabled = true
			imported.Latency = "—"
			result = append(result, imported)
		}
	}
	return result
}

func parseSubscriptionProxies(raw string) []proxyServer {
	config := parseRawSubscriptionConfig(raw)
	result := []proxyServer{}
	for _, item := range config.Proxies {
		name, _ := item["name"].(string)
		address, _ := item["server"].(string)
		if name == "" || address == "" {
			continue
		}
		protocol, _ := item["type"].(string)
		credential := stringValue(item["uuid"])
		if credential == "" {
			credential = stringValue(item["password"])
		}
		port := intValue(item["port"])
		if port < 1 {
			port = 443
		}
		result = append(result, proxyServer{Name: name, Protocol: protocol, Address: address, Port: port, Credential: credential, Transport: stringValue(item["network"]), SNI: stringValue(item["sni"]), SkipVerify: boolValue(item["skip-cert-verify"]), Enabled: true})
	}
	return result
}

// parseRawSubscriptionConfig retains the original Clash proxy maps. Generated
// configurations must preserve protocol-specific fields such as SS cipher,
// Reality options, and transport settings rather than rebuilding a partial node.
func parseRawSubscriptionConfig(raw string) rawClashConfig {
	data := []byte(raw)
	config := rawClashConfig{}
	if yaml.Unmarshal(data, &config) != nil || len(config.Proxies) == 0 {
		if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw)); err == nil {
			_ = yaml.Unmarshal(decoded, &config)
		}
	}
	return config
}

func (a *App) subscriptionConfigProxies() []map[string]any {
	result := []map[string]any{}
	rows, err := a.db.Query(`SELECT name,COALESCE(match_list,''),COALESCE(mismatch_list,''),COALESCE(raw_content,'') FROM subscriptions WHERE enabled=1 AND raw_content IS NOT NULL AND raw_content <> '' ORDER BY id`)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var name, matchList, mismatchList, raw string
		if rows.Scan(&name, &matchList, &mismatchList, &raw) != nil {
			continue
		}
		for _, item := range parseRawSubscriptionConfig(filterSubscriptionContent([]byte(raw), matchList, mismatchList)).Proxies {
			proxy := make(map[string]any, len(item))
			for key, value := range item {
				proxy[key] = value
			}
			if nodeName := stringValue(proxy["name"]); nodeName != "" {
				proxy["name"] = name + " / " + nodeName
			}
			result = append(result, proxy)
		}
	}
	return result
}

func filterSubscriptionContent(body []byte, matchList, mismatchList string) string {
	matchTerms := filterTerms(matchList)
	mismatchTerms := filterTerms(mismatchList)
	if len(matchTerms) == 0 && len(mismatchTerms) == 0 {
		return string(body)
	}
	data := []byte(string(body))
	config := rawClashConfig{}
	if yaml.Unmarshal(data, &config) != nil || len(config.Proxies) == 0 {
		if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(body))); err == nil {
			data = decoded
			_ = yaml.Unmarshal(decoded, &config)
		}
	}
	filtered := make([]map[string]any, 0, len(config.Proxies))
	for _, proxy := range config.Proxies {
		searchable := strings.ToLower(strings.Join([]string{stringValue(proxy["name"]), stringValue(proxy["server"]), stringValue(proxy["type"]), stringValue(proxy["country"])}, " "))
		if len(matchTerms) > 0 && !containsFilterTerm(searchable, matchTerms) {
			continue
		}
		if containsFilterTerm(searchable, mismatchTerms) {
			continue
		}
		filtered = append(filtered, proxy)
	}
	config.Proxies = filtered
	result, err := yaml.Marshal(config)
	if err != nil {
		return string(body)
	}
	return string(result)
}

func filterTerms(value string) []string {
	terms := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })
	result := make([]string, 0, len(terms))
	for _, term := range terms {
		if trimmed := strings.TrimSpace(term); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func containsFilterTerm(value string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case uint64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		result, _ := strconv.Atoi(fmt.Sprint(value))
		return result
	}
}

func boolValue(value any) bool {
	result, _ := strconv.ParseBool(fmt.Sprint(value))
	return result
}
func (a *App) handleProxyAction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r.URL.Path, "/api/proxies/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		var name string
		if err := a.db.QueryRow(`SELECT name FROM proxies WHERE id=?`, id).Scan(&name); err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "Manual proxy not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if dependencies := a.dependentProxyGroups("manual proxy", id, name); len(dependencies) > 0 {
			writeDependencyConflict(w, "manual proxy", name, dependencies)
			return
		}
		_, err := a.db.Exec(`DELETE FROM proxies WHERE id=?`, id)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		w.WriteHeader(204)
		return
	}
	if r.Method == http.MethodPatch {
		var x proxyServer
		if !decodeJSON(w, r, &x) {
			return
		}
		_, err := a.db.Exec(`UPDATE proxies SET name=?,protocol=?,address=?,port=?,credential=?,transport=?,sni=?,skip_verify=?,enabled=? WHERE id=?`, x.Name, x.Protocol, x.Address, x.Port, x.Credential, x.Transport, x.SNI, boolInt(x.SkipVerify), boolInt(x.Enabled), id)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	methodNotAllowed(w)
}

func (a *App) handleRules(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, 200, a.listRules())
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var x routingRule
	if !decodeJSON(w, r, &x) || x.RuleType == "" || x.Match == "" || x.Target == "" {
		writeError(w, 400, "Rule type, match, and target are required")
		return
	}
	if err := a.resolveRuleTarget(&x); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.db.Exec(`INSERT INTO rules(rule_type,match_value,target,target_group_id,priority,enabled,created_at) VALUES(?,?,?,?,?,?,?)`, x.RuleType, x.Match, x.Target, nullableID(x.TargetGroupID), x.Priority, boolInt(x.Enabled || x.ID == 0), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	x.ID, _ = result.LastInsertId()
	x.Enabled = true
	writeJSON(w, 201, x)
}
func (a *App) listRules() []routingRule {
	rows, err := a.db.Query(`SELECT rules.id,rule_type,match_value,COALESCE(proxy_groups.name,rules.target),COALESCE(target_group_id,0),priority,rules.enabled
		FROM rules LEFT JOIN proxy_groups ON proxy_groups.id=rules.target_group_id ORDER BY priority,rules.id`)
	if err != nil {
		return []routingRule{}
	}
	defer rows.Close()
	result := []routingRule{}
	for rows.Next() {
		var x routingRule
		var enabled int
		_ = rows.Scan(&x.ID, &x.RuleType, &x.Match, &x.Target, &x.TargetGroupID, &x.Priority, &enabled)
		x.Enabled = enabled == 1
		result = append(result, x)
	}
	return result
}

func nullableID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

func (a *App) resolveRuleTarget(rule *routingRule) error {
	rule.Target = strings.TrimSpace(rule.Target)
	if strings.HasPrefix(rule.Target, "group-id:") {
		id, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(rule.Target, "group-id:")), 10, 64)
		if err != nil || id <= 0 {
			return fmt.Errorf("invalid proxy group target")
		}
		rule.TargetGroupID = id
	}
	if rule.TargetGroupID > 0 {
		if err := a.db.QueryRow(`SELECT name FROM proxy_groups WHERE id=? AND enabled=1`, rule.TargetGroupID).Scan(&rule.Target); err != nil {
			return fmt.Errorf("proxy group target not found")
		}
		return nil
	}
	var groupID int64
	if err := a.db.QueryRow(`SELECT id FROM proxy_groups WHERE name=? AND enabled=1 ORDER BY id LIMIT 1`, rule.Target).Scan(&groupID); err == nil {
		rule.TargetGroupID = groupID
	}
	return nil
}

func (a *App) handleRuleReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		methodNotAllowed(w)
		return
	}
	var input struct {
		IDs []int64 `json:"ids"`
	}
	if !decodeJSON(w, r, &input) || len(input.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "At least one rule is required")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for index, id := range input.IDs {
		if _, err = tx.Exec(`UPDATE rules SET priority=? WHERE id=?`, (index+1)*10, id); err != nil {
			_ = tx.Rollback()
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) listServiceRuleGroups() []serviceRuleGroup {
	rows, err := a.db.Query(`SELECT service_rule_groups.id,service_rule_groups.name,service_rule_groups.rules_json,
		service_rule_groups.target_group_id,COALESCE(proxy_groups.name,''),service_rule_groups.priority,service_rule_groups.enabled
		FROM service_rule_groups LEFT JOIN proxy_groups ON proxy_groups.id=service_rule_groups.target_group_id
		ORDER BY service_rule_groups.priority,service_rule_groups.id`)
	if err != nil {
		return []serviceRuleGroup{}
	}
	defer rows.Close()
	result := []serviceRuleGroup{}
	for rows.Next() {
		var item serviceRuleGroup
		var raw string
		var enabled int
		if rows.Scan(&item.ID, &item.Name, &raw, &item.TargetGroupID, &item.Target, &item.Priority, &enabled) != nil {
			continue
		}
		_ = json.Unmarshal([]byte(raw), &item.Rules)
		item.RuleCount = len(item.Rules)
		item.Enabled = enabled == 1
		result = append(result, item)
	}
	return result
}

func (a *App) handleServiceRuleGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, a.listServiceRuleGroups())
}

func (a *App) handleServiceRuleGroupAction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r.URL.Path, "/api/service-rule-groups/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		if _, err := a.db.Exec(`DELETE FROM service_rule_groups WHERE id=?`, id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodPatch {
		var input serviceRuleGroup
		if !decodeJSON(w, r, &input) || strings.TrimSpace(input.Name) == "" || input.TargetGroupID <= 0 {
			writeError(w, http.StatusBadRequest, "Name and outbound proxy group are required")
			return
		}
		var exists int
		if err := a.db.QueryRow(`SELECT COUNT(*) FROM proxy_groups WHERE id=? AND enabled=1`, input.TargetGroupID).Scan(&exists); err != nil || exists == 0 {
			writeError(w, http.StatusBadRequest, "Outbound proxy group not found")
			return
		}
		if _, err := a.db.Exec(`UPDATE service_rule_groups SET name=?,target_group_id=?,enabled=? WHERE id=?`, strings.TrimSpace(input.Name), input.TargetGroupID, boolInt(input.Enabled), id); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	methodNotAllowed(w)
}

func (a *App) handleRuleAction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r.URL.Path, "/api/rules/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		_, err := a.db.Exec(`DELETE FROM rules WHERE id=?`, id)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		w.WriteHeader(204)
		return
	}
	if r.Method == http.MethodPatch {
		var x routingRule
		if !decodeJSON(w, r, &x) || strings.TrimSpace(x.RuleType) == "" || strings.TrimSpace(x.Match) == "" || strings.TrimSpace(x.Target) == "" {
			writeError(w, http.StatusBadRequest, "Rule type, match, and target are required")
			return
		}
		if err := a.resolveRuleTarget(&x); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		_, err := a.db.Exec(`UPDATE rules SET rule_type=?,match_value=?,target=?,target_group_id=?,priority=?,enabled=? WHERE id=?`, x.RuleType, x.Match, x.Target, nullableID(x.TargetGroupID), x.Priority, boolInt(x.Enabled), id)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	methodNotAllowed(w)
}

func defaultManagedPath(directory, name string) string {
	var slug strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			slug.WriteRune(r)
		} else if slug.Len() > 0 {
			slug.WriteByte('-')
		}
	}
	value := strings.Trim(slug.String(), "-")
	if value == "" {
		value = "provider"
	}
	return "./" + directory + "/" + value + ".yaml"
}

func defaultRuleProviderPath(name string) string {
	return defaultManagedPath("ruleset", name)
}

func defaultSubscriptionPath(name string) string {
	return defaultManagedPath("proxy-providers", name)
}

func normalizeSubscription(x *subscription) {
	x.Name = strings.TrimSpace(x.Name)
	x.Path = defaultSubscriptionPath(x.Name)
}

func normalizeRuleProvider(x *ruleProvider) {
	x.Name = strings.TrimSpace(x.Name)
	x.Type = strings.ToLower(strings.TrimSpace(x.Type))
	if x.Type == "" {
		x.Type = "http"
	}
	if x.Type != "http" {
		x.Type = "http"
	}
	x.Behavior = strings.ToLower(strings.TrimSpace(x.Behavior))
	if x.Behavior != "domain" && x.Behavior != "ipcidr" && x.Behavior != "classical" {
		x.Behavior = "classical"
	}
	x.Format = strings.ToLower(strings.TrimSpace(x.Format))
	if x.Format != "yaml" && x.Format != "text" && x.Format != "mrs" {
		x.Format = "yaml"
	}
	// Rule-provider files always live in the managed ruleset directory. The
	// provider name is the single source of truth for the generated filename.
	x.Path = defaultRuleProviderPath(x.Name)
	if x.UpdateMode == "" {
		x.UpdateMode = "manual"
	}
	if x.IntervalMinutes < 1 {
		x.IntervalMinutes = 86400
	}
}

func (a *App) handleRuleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, 200, a.listRuleProviders())
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var x ruleProvider
	if !decodeJSON(w, r, &x) || x.Name == "" || !validHTTPURL(x.PrimaryURL) {
		writeError(w, 400, "Name and a valid primary URL are required")
		return
	}
	normalizeRuleProvider(&x)
	result, err := a.db.Exec(`INSERT INTO rule_providers(name,provider_type,behavior,provider_format,primary_url,backup_url,path,enabled,update_mode,interval_minutes,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, x.Name, x.Type, x.Behavior, x.Format, x.PrimaryURL, x.BackupURL, x.Path, boolInt(x.Enabled || x.ID == 0), x.UpdateMode, x.IntervalMinutes, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	x.ID, _ = result.LastInsertId()
	x.Enabled = true
	writeJSON(w, 201, x)
}
func (a *App) listRuleProviders() []ruleProvider {
	rows, err := a.db.Query(`SELECT id,name,COALESCE(provider_type,'http'),COALESCE(behavior,'classical'),COALESCE(provider_format,'yaml'),primary_url,COALESCE(backup_url,''),COALESCE(path,''),enabled,update_mode,interval_minutes,COALESCE(last_update_at,''),COALESCE(last_error,'') FROM rule_providers ORDER BY id DESC`)
	if err != nil {
		return []ruleProvider{}
	}
	defer rows.Close()
	result := []ruleProvider{}
	for rows.Next() {
		var x ruleProvider
		var enabled int
		_ = rows.Scan(&x.ID, &x.Name, &x.Type, &x.Behavior, &x.Format, &x.PrimaryURL, &x.BackupURL, &x.Path, &enabled, &x.UpdateMode, &x.IntervalMinutes, &x.LastUpdateAt, &x.LastError)
		normalizeRuleProvider(&x)
		x.Enabled = enabled == 1
		result = append(result, x)
	}
	return result
}
func (a *App) handleRuleProviderAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/update")
	id, ok := pathID(path, "/api/rule-providers/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/update") {
		var item ruleProvider
		if err := a.db.QueryRow(`SELECT id,name,primary_url,COALESCE(backup_url,'') FROM rule_providers WHERE id=?`, id).Scan(&item.ID, &item.Name, &item.PrimaryURL, &item.BackupURL); err != nil {
			writeError(w, http.StatusNotFound, "Rule provider not found")
			return
		}
		body, err := a.fetchRuleProvider(item.PrimaryURL)
		if err != nil && item.BackupURL != "" {
			body, err = a.fetchRuleProvider(item.BackupURL)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		if err != nil {
			_, _ = a.db.Exec(`UPDATE rule_providers SET last_update_at=?,last_error=? WHERE id=?`, now, err.Error(), id)
			writeError(w, http.StatusBadGateway, "Rule provider update failed: "+err.Error())
			return
		}
		_, _ = a.db.Exec(`UPDATE rule_providers SET last_update_at=?,last_error='',content=? WHERE id=?`, now, string(body), id)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "updatedAt": now})
		return
	}
	if r.Method == http.MethodDelete {
		var name string
		if err := a.db.QueryRow(`SELECT name FROM rule_providers WHERE id=?`, id).Scan(&name); err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "Rule provider not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rows, err := a.db.Query(`SELECT target FROM rules WHERE UPPER(rule_type)='RULE-SET' AND LOWER(match_value)=LOWER(?) ORDER BY priority,id`, name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		dependencies := []string{}
		for rows.Next() {
			var target string
			if rows.Scan(&target) == nil {
				dependencies = append(dependencies, fmt.Sprintf("routing rule %q", "RULE-SET "+name+" → "+target))
			}
		}
		rows.Close()
		for _, group := range a.listServiceRuleGroups() {
			for _, rule := range group.Rules {
				parts := strings.SplitN(rule, ",", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], "RULE-SET") && strings.EqualFold(strings.TrimSpace(parts[1]), name) {
					dependencies = append(dependencies, fmt.Sprintf("service rule group %q", group.Name))
					break
				}
			}
		}
		if strings.EqualFold(name, "geosite-cn") && a.settings().DNSEnhancedMode == "redir-host" {
			dependencies = append(dependencies, "client DNS policy")
		}
		if len(dependencies) > 0 {
			writeDependencyConflict(w, "rule provider", name, dependencies)
			return
		}
		_, err = a.db.Exec(`DELETE FROM rule_providers WHERE id=?`, id)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		w.WriteHeader(204)
		return
	}
	if r.Method == http.MethodPatch {
		var x ruleProvider
		if !decodeJSON(w, r, &x) || strings.TrimSpace(x.Name) == "" || !validHTTPURL(x.PrimaryURL) {
			writeError(w, http.StatusBadRequest, "Name and a valid primary URL are required")
			return
		}
		normalizeRuleProvider(&x)
		_, err := a.db.Exec(`UPDATE rule_providers SET name=?,provider_type=?,behavior=?,provider_format=?,primary_url=?,backup_url=?,path=?,enabled=?,update_mode=?,interval_minutes=? WHERE id=?`, x.Name, x.Type, x.Behavior, x.Format, x.PrimaryURL, x.BackupURL, x.Path, boolInt(x.Enabled), x.UpdateMode, x.IntervalMinutes, id)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	methodNotAllowed(w)
}

func (a *App) fetchRuleProvider(source string) ([]byte, error) {
	client := &http.Client{Timeout: 25 * time.Second}
	req, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", a.settings().DefaultUserAgent)
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("upstream status %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 10<<20))
}

func (a *App) handleGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, a.listGroups())
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input proxyGroup
	if !decodeJSON(w, r, &input) || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Type) == "" {
		writeError(w, http.StatusBadRequest, "Group name and type are required")
		return
	}
	if len(input.Proxies) == 0 {
		input.Proxies = []string{"DIRECT"}
	}
	if err := a.validateGroupMembers(input.Name, input.Proxies, 0); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	encoded, _ := json.Marshal(input.Proxies)
	result, err := a.db.Exec(`INSERT INTO proxy_groups(name,group_type,proxies_json,enabled,created_at) VALUES(?,?,?,?,?)`, input.Name, input.Type, string(encoded), boolInt(input.Enabled || input.ID == 0), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.ID, _ = result.LastInsertId()
	input.Enabled = true
	if err := a.seedServiceRuleGroups(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, input)
}

func (a *App) listGroups() []proxyGroup {
	rows, err := a.db.Query(`SELECT id,name,group_type,proxies_json,enabled FROM proxy_groups ORDER BY id`)
	if err != nil {
		return []proxyGroup{}
	}
	defer rows.Close()
	result := []proxyGroup{}
	for rows.Next() {
		var item proxyGroup
		var raw string
		var enabled int
		if rows.Scan(&item.ID, &item.Name, &item.Type, &raw, &enabled) != nil {
			continue
		}
		_ = json.Unmarshal([]byte(raw), &item.Proxies)
		item.Enabled = enabled == 1
		result = append(result, item)
	}
	return result
}

func groupReferences(members []string, groupNames map[int64]string) []string {
	result := []string{}
	for _, member := range members {
		if strings.HasPrefix(member, "group-id:") {
			id, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(member, "group-id:")), 10, 64)
			if name := groupNames[id]; name != "" {
				result = append(result, name)
			}
		} else if strings.HasPrefix(member, "group:") {
			if name := strings.TrimSpace(strings.TrimPrefix(member, "group:")); name != "" {
				result = append(result, name)
			}
		}
	}
	return result
}

func (a *App) validateGroupMembers(name string, members []string, currentID int64) error {
	seenMembers := map[string]bool{}
	for _, member := range members {
		key := strings.TrimSpace(member)
		if strings.EqualFold(key, "DIRECT") {
			key = "DIRECT"
		}
		if key != "" && seenMembers[key] {
			return fmt.Errorf("each proxy group member can only be added once")
		}
		seenMembers[key] = true
	}
	existingGroups := a.listGroups()
	groupNames := map[int64]string{}
	for _, group := range existingGroups {
		groupNames[group.ID] = group.Name
	}
	graph := map[string][]string{name: groupReferences(members, groupNames)}
	for _, group := range existingGroups {
		if group.ID == currentID {
			continue
		}
		graph[group.Name] = groupReferences(group.Proxies, groupNames)
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var hasCycle func(string) bool
	hasCycle = func(groupName string) bool {
		if visiting[groupName] {
			return true
		}
		if visited[groupName] {
			return false
		}
		visiting[groupName] = true
		for _, child := range graph[groupName] {
			if hasCycle(child) {
				return true
			}
		}
		visiting[groupName] = false
		visited[groupName] = true
		return false
	}
	if hasCycle(name) {
		return fmt.Errorf("a proxy group cannot include itself, directly or through another group")
	}
	return nil
}

func (a *App) handleGroupAction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r.URL.Path, "/api/groups/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		var name string
		if err := a.db.QueryRow(`SELECT name FROM proxy_groups WHERE id=?`, id).Scan(&name); err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "Proxy group not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		dependencies := a.dependentProxyGroups("proxy group", id, name)
		ruleRows, err := a.db.Query(`SELECT rule_type,match_value FROM rules WHERE target_group_id=? ORDER BY priority,id`, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for ruleRows.Next() {
			var ruleType, match string
			if ruleRows.Scan(&ruleType, &match) == nil {
				dependencies = append(dependencies, fmt.Sprintf("routing rule %q", ruleType+" "+match))
			}
		}
		ruleRows.Close()
		serviceRows, err := a.db.Query(`SELECT name FROM service_rule_groups WHERE target_group_id=? ORDER BY priority,id`, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for serviceRows.Next() {
			var serviceName string
			if serviceRows.Scan(&serviceName) == nil {
				dependencies = append(dependencies, fmt.Sprintf("service rule group %q", serviceName))
			}
		}
		serviceRows.Close()
		var dnsPolicyGroupID int64
		var dnsPolicyGroupValue string
		if settingsErr := a.db.QueryRow(`SELECT value FROM settings WHERE key='dns_policy_group_id'`).Scan(&dnsPolicyGroupValue); settingsErr == nil {
			dnsPolicyGroupID, _ = strconv.ParseInt(dnsPolicyGroupValue, 10, 64)
		}
		if dnsPolicyGroupID == id {
			dependencies = append(dependencies, "client DNS policy")
		}
		if len(dependencies) > 0 {
			writeDependencyConflict(w, "proxy group", name, dependencies)
			return
		}
		_, err = a.db.Exec(`DELETE FROM proxy_groups WHERE id=?`, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodPatch {
		var input proxyGroup
		if !decodeJSON(w, r, &input) || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Type) == "" {
			writeError(w, http.StatusBadRequest, "Group name and type are required")
			return
		}
		if len(input.Proxies) == 0 {
			input.Proxies = []string{"DIRECT"}
		}
		if err := a.validateGroupMembers(input.Name, input.Proxies, id); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		encoded, _ := json.Marshal(input.Proxies)
		_, err := a.db.Exec(`UPDATE proxy_groups SET name=?,group_type=?,proxies_json=?,enabled=? WHERE id=?`, input.Name, input.Type, string(encoded), boolInt(input.Enabled), id)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := a.seedServiceRuleGroups(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	methodNotAllowed(w)
}

func (a *App) handleAccessKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, 200, a.listAccessKeys())
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Name          string  `json:"name"`
		Key           string  `json:"key"`
		MonthlyDataGB float64 `json:"monthlyDataGB"`
	}
	if !decodeJSON(w, r, &input) || strings.TrimSpace(input.Name) == "" || input.MonthlyDataGB < 0 {
		writeError(w, 400, "Key name and a non-negative monthly data allowance are required")
		return
	}
	if input.Key == "" {
		input.Key = randomToken(16)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{8,80}$`).MatchString(input.Key) {
		writeError(w, 400, "Key must use 8-80 letters, numbers, hyphens, or underscores")
		return
	}
	preview := input.Key[:2] + "••••" + input.Key[len(input.Key)-2:]
	result, err := a.db.Exec(`INSERT INTO access_keys(name,key_hash,key_value,key_preview,monthly_data_gb,created_at) VALUES(?,?,?,?,?,?)`, input.Name, hashToken(input.Key), input.Key, preview, input.MonthlyDataGB, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		writeError(w, 400, "Key already exists")
		return
	}
	id, _ := result.LastInsertId()
	writeJSON(w, 201, accessKey{ID: id, Name: input.Name, Key: input.Key, KeyPreview: preview, Enabled: true, MonthlyDataGB: input.MonthlyDataGB})
}
func (a *App) listAccessKeys() []accessKey {
	rows, err := a.db.Query(`SELECT id,name,key_value,key_preview,enabled,monthly_data_gb,COALESCE(last_used_at,''),created_at FROM access_keys ORDER BY id DESC`)
	if err != nil {
		return []accessKey{}
	}
	defer rows.Close()
	result := []accessKey{}
	for rows.Next() {
		var x accessKey
		var enabled int
		_ = rows.Scan(&x.ID, &x.Name, &x.Key, &x.KeyPreview, &enabled, &x.MonthlyDataGB, &x.LastUsedAt, &x.CreatedAt)
		x.Enabled = enabled == 1
		result = append(result, x)
	}
	return result
}
func (a *App) handleAccessKeyAction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r.URL.Path, "/api/access-keys/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		_, err := a.db.Exec(`DELETE FROM access_keys WHERE id=?`, id)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		w.WriteHeader(204)
		return
	}
	if r.Method == http.MethodPatch {
		var input struct {
			Name          string  `json:"name"`
			Enabled       bool    `json:"enabled"`
			Key           string  `json:"key"`
			MonthlyDataGB float64 `json:"monthlyDataGB"`
		}
		if !decodeJSON(w, r, &input) || strings.TrimSpace(input.Name) == "" || input.MonthlyDataGB < 0 {
			writeError(w, http.StatusBadRequest, "Key name and a non-negative monthly data allowance are required")
			return
		}
		var err error
		if input.Key != "" {
			if !regexp.MustCompile(`^[A-Za-z0-9_-]{8,80}$`).MatchString(input.Key) {
				writeError(w, http.StatusBadRequest, "Key must use 8-80 letters, numbers, hyphens, or underscores")
				return
			}
			preview := input.Key[:2] + "••••" + input.Key[len(input.Key)-2:]
			_, err = a.db.Exec(`UPDATE access_keys SET name=?,key_hash=?,key_value=?,key_preview=?,enabled=?,monthly_data_gb=? WHERE id=?`, strings.TrimSpace(input.Name), hashToken(input.Key), input.Key, preview, boolInt(input.Enabled), input.MonthlyDataGB, id)
		} else {
			_, err = a.db.Exec(`UPDATE access_keys SET name=?,enabled=?,monthly_data_gb=? WHERE id=?`, strings.TrimSpace(input.Name), boolInt(input.Enabled), input.MonthlyDataGB, id)
		}
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		response := map[string]any{"ok": true}
		// The token is intentionally returned only when it has just been replaced.
		// Existing keys are stored as hashes and can never be recovered.
		if input.Key != "" {
			response["key"] = input.Key
		}
		writeJSON(w, 200, response)
		return
	}
	methodNotAllowed(w)
}

func (a *App) handleConfigPreview(w http.ResponseWriter, r *http.Request) {
	writeYAML(w, a.generateConfig())
}

func validClashMode(value string) bool {
	return value == "rule" || value == "global" || value == "direct"
}
func validLogLevel(value string) bool {
	return value == "silent" || value == "error" || value == "warning" || value == "info" || value == "debug"
}
func validDNSEnhancedMode(value string) bool {
	return value == "normal" || value == "fake-ip" || value == "redir-host"
}
func configList(value string) []string {
	items := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
func dnsNameserversWithProxy(value, proxyGroup string) string {
	items := configList(value)
	for index, item := range items {
		if proxyGroup != "" && !strings.Contains(item, "#") {
			items[index] = item + "#" + proxyGroup
		}
	}
	return strings.Join(items, "\n")
}

type dnsPolicyEntry struct {
	priority int
	sequence int
	key      string
	servers  string
}

func dnsPolicyKey(ruleType, match string) string {
	switch strings.ToUpper(strings.TrimSpace(ruleType)) {
	case "DOMAIN":
		return strings.TrimSpace(match)
	case "DOMAIN-SUFFIX":
		return "+." + strings.TrimPrefix(strings.TrimSpace(match), ".")
	case "RULE-SET":
		return "rule-set:" + strings.TrimSpace(match)
	default:
		return ""
	}
}

func buildDNSPolicyEntries(rules []routingRule, serviceGroups []serviceRuleGroup, providers []ruleProvider, domesticDNS, overseasDNS string) []dnsPolicyEntry {
	domainProviders := map[string]bool{}
	for _, provider := range providers {
		if provider.Enabled && provider.Behavior != "ipcidr" {
			domainProviders[strings.ToLower(provider.Name)] = true
		}
	}
	entries := []dnsPolicyEntry{}
	sequence := 0
	appendRule := func(priority int, ruleType, match, target string) {
		key := dnsPolicyKey(ruleType, match)
		if key == "" || strings.EqualFold(target, "REJECT") {
			return
		}
		if strings.EqualFold(ruleType, "RULE-SET") && !domainProviders[strings.ToLower(strings.TrimSpace(match))] {
			return
		}
		servers := domesticDNS
		if !strings.EqualFold(target, "DIRECT") {
			servers = dnsNameserversWithProxy(overseasDNS, target)
		}
		entries = append(entries, dnsPolicyEntry{priority: priority, sequence: sequence, key: key, servers: servers})
		sequence++
	}
	for _, rule := range rules {
		if rule.Enabled {
			appendRule(rule.Priority, rule.RuleType, rule.Match, rule.Target)
		}
	}
	for _, group := range serviceGroups {
		if !group.Enabled || group.Target == "" {
			continue
		}
		for _, rule := range group.Rules {
			parts := strings.SplitN(rule, ",", 2)
			if len(parts) == 2 {
				appendRule(group.Priority, parts[0], parts[1], group.Target)
			}
		}
	}
	entries = append(entries, dnsPolicyEntry{priority: 59, sequence: sequence, key: "rule-set:geosite-cn", servers: domesticDNS})
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].priority == entries[j].priority {
			return entries[i].sequence < entries[j].sequence
		}
		return entries[i].priority < entries[j].priority
	})
	seen := map[string]bool{}
	result := make([]dnsPolicyEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.key == "" || entry.servers == "" || seen[entry.key] {
			continue
		}
		seen[entry.key] = true
		result = append(result, entry)
	}
	return result
}

func writeConfigList(b *strings.Builder, indent, key string, value string) {
	items := configList(value)
	if len(items) == 0 {
		return
	}
	b.WriteString(indent + key + ":\n")
	for _, item := range items {
		b.WriteString(fmt.Sprintf("%s  - %q\n", indent, item))
	}
}
func writeSubscriptionProxy(b *strings.Builder, proxy map[string]any) {
	ordered := []string{"name", "type", "server", "port", "cipher", "password"}
	remaining := make(map[string]any, len(proxy))
	for key, value := range proxy {
		remaining[key] = value
	}
	lines := []string{}
	for _, key := range ordered {
		if value, ok := remaining[key]; ok {
			encoded, err := yaml.Marshal(map[string]any{key: value})
			if err == nil {
				lines = append(lines, strings.Split(strings.TrimSuffix(string(encoded), "\n"), "\n")...)
			}
			delete(remaining, key)
		}
	}
	if len(remaining) > 0 {
		if encoded, err := yaml.Marshal(remaining); err == nil {
			lines = append(lines, strings.Split(strings.TrimSuffix(string(encoded), "\n"), "\n")...)
		}
	}
	for index, line := range lines {
		if index == 0 {
			b.WriteString("  - " + line + "\n")
		} else {
			b.WriteString("    " + line + "\n")
		}
	}
}
func (a *App) generateConfig() string {
	proxies := a.allProxies()
	importedProxyConfigs := a.subscriptionConfigProxies()
	rules := a.listRules()
	serviceRuleGroups := a.listServiceRuleGroups()
	groups := a.listGroups()
	subscriptions := a.listSubscriptions()
	ruleProviders := a.listRuleProviders()
	client := a.settings()
	dnsPolicyGroup := ""
	for _, group := range groups {
		if group.Enabled && group.ID == client.DNSPolicyGroupID {
			dnsPolicyGroup = group.Name
			break
		}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("mixed-port: %d\nallow-lan: %t\nbind-address: %q\nmode: %s\nlog-level: %s\n", client.MixedPort, client.AllowLAN, client.BindAddress, client.Mode, client.LogLevel))
	b.WriteString("dns:\n")
	b.WriteString(fmt.Sprintf("  enable: %t\n  ipv6: %t\n  enhanced-mode: %s\n", client.DNSEnabled, client.DNSIPv6, client.DNSEnhancedMode))
	if client.DNSEnhancedMode == "fake-ip" {
		if client.DNSFakeIPRange != "" {
			b.WriteString(fmt.Sprintf("  fake-ip-range: %q\n", client.DNSFakeIPRange))
		}
		writeConfigList(&b, "  ", "fake-ip-filter", client.DNSFakeIPFilter)
	}
	b.WriteString(fmt.Sprintf("  use-hosts: %t\n", client.DNSUseHosts))
	writeConfigList(&b, "  ", "default-nameserver", client.DNSDefaultNameservers)
	if client.DNSEnhancedMode == "redir-host" && dnsPolicyGroup != "" {
		overseasNameservers := dnsNameserversWithProxy(client.DNSFallback, dnsPolicyGroup)
		writeConfigList(&b, "  ", "proxy-server-nameserver", client.DNSNameservers)
		writeConfigList(&b, "  ", "nameserver", overseasNameservers)
		b.WriteString("  nameserver-policy:\n")
		for _, policy := range buildDNSPolicyEntries(rules, serviceRuleGroups, ruleProviders, client.DNSNameservers, client.DNSFallback) {
			writeConfigList(&b, "    ", fmt.Sprintf("%q", policy.key), policy.servers)
		}
		writeConfigList(&b, "    ", "\"+.*\"", overseasNameservers)
	} else {
		writeConfigList(&b, "  ", "nameserver", client.DNSNameservers)
		if strings.TrimSpace(client.DNSFallback) != "" {
			writeConfigList(&b, "  ", "fallback", client.DNSFallback)
			b.WriteString(fmt.Sprintf("  fallback-filter:\n    geoip: %t\n", client.DNSFallbackGeoIP))
			writeConfigList(&b, "    ", "ipcidr", client.DNSFallbackIPCIDR)
		}
	}
	b.WriteString("\nproxies:\n")
	for _, p := range proxies {
		// Imported proxies are written from their original subscription maps below.
		if !p.Enabled || p.ID < 0 {
			continue
		}
		protocol := strings.ToLower(p.Protocol)
		b.WriteString(fmt.Sprintf("  - name: %q\n    type: %s\n    server: %s\n    port: %d\n", p.Name, strings.ToLower(p.Protocol), p.Address, p.Port))
		if protocol == "vless" || protocol == "vmess" {
			b.WriteString(fmt.Sprintf("    uuid: %s\n", p.Credential))
		} else {
			b.WriteString(fmt.Sprintf("    password: %s\n", p.Credential))
		}
		if p.SNI != "" {
			b.WriteString(fmt.Sprintf("    sni: %s\n", p.SNI))
		}
		if p.SkipVerify {
			b.WriteString("    skip-cert-verify: true\n")
		}
	}
	for _, proxy := range importedProxyConfigs {
		writeSubscriptionProxy(&b, proxy)
	}
	b.WriteString("\nproxy-groups:\n")
	if len(groups) == 0 {
		groups = []proxyGroup{{Name: "PROXY", Type: "select", Enabled: true}}
	}
	for _, group := range groups {
		if !group.Enabled {
			continue
		}
		b.WriteString(fmt.Sprintf("  - name: %q\n    type: %s\n    proxies:\n", group.Name, strings.ToLower(group.Type)))
		members := expandGroupMembers(group.Proxies, proxies, subscriptions, groups)
		if len(members) == 0 {
			for _, p := range proxies {
				if p.Enabled {
					members = append(members, p.Name)
				}
			}
			members = append(members, "DIRECT")
		}
		for _, member := range members {
			b.WriteString(fmt.Sprintf("      - %q\n", member))
		}
	}
	b.WriteString("\nrule-providers:\n")
	for _, provider := range ruleProviders {
		if !provider.Enabled {
			continue
		}
		normalizeRuleProvider(&provider)
		b.WriteString(fmt.Sprintf("  %q:\n    type: %s\n    behavior: %s\n    format: %s\n    url: %q\n    path: %s\n    interval: %d\n", provider.Name, provider.Type, provider.Behavior, provider.Format, provider.PrimaryURL, provider.Path, maxInt(provider.IntervalMinutes, 60)))
	}
	b.WriteString("\nrules:\n")
	type generatedRule struct {
		priority int
		sequence int
		value    string
	}
	generatedRules := []generatedRule{}
	sequence := 0
	for _, rule := range rules {
		if rule.Enabled {
			generatedRules = append(generatedRules, generatedRule{priority: rule.Priority, sequence: sequence, value: fmt.Sprintf("%s,%s,%s", rule.RuleType, rule.Match, rule.Target)})
			sequence++
		}
	}
	for _, group := range serviceRuleGroups {
		if !group.Enabled || group.Target == "" {
			continue
		}
		for _, rule := range group.Rules {
			generatedRules = append(generatedRules, generatedRule{priority: group.Priority, sequence: sequence, value: rule + "," + group.Target})
			sequence++
		}
	}
	sort.SliceStable(generatedRules, func(i, j int) bool {
		if generatedRules[i].priority == generatedRules[j].priority {
			return generatedRules[i].sequence < generatedRules[j].sequence
		}
		return generatedRules[i].priority < generatedRules[j].priority
	})
	for _, rule := range generatedRules {
		b.WriteString("  - " + rule.value + "\n")
	}
	b.WriteString("  - MATCH,DIRECT\n")
	return b.String()
}

// Membership references use stable database IDs, so renaming a source or group
// does not break a proxy group. Legacy name-based references are still read.
func expandGroupMembers(members []string, proxies []proxyServer, subscriptions []subscription, groups []proxyGroup) []string {
	subscriptionNames := map[int64]string{}
	for _, item := range subscriptions {
		subscriptionNames[item.ID] = item.Name
	}
	groupNames := map[int64]string{}
	for _, item := range groups {
		groupNames[item.ID] = item.Name
	}
	proxyNames := map[int64]string{}
	for _, item := range proxies {
		if item.ID > 0 {
			proxyNames[item.ID] = item.Name
		}
	}
	result := make([]string, 0, len(members))
	seen := map[string]bool{}
	appendMember := func(value string) {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	for _, member := range members {
		if strings.HasPrefix(member, "proxy-id:") {
			id, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(member, "proxy-id:")), 10, 64)
			appendMember(proxyNames[id])
			continue
		}
		if strings.HasPrefix(member, "subscription-id:") || strings.HasPrefix(member, "subscription:") {
			subscriptionName := ""
			if strings.HasPrefix(member, "subscription-id:") {
				id, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(member, "subscription-id:")), 10, 64)
				subscriptionName = subscriptionNames[id]
			} else {
				subscriptionName = strings.TrimSpace(strings.TrimPrefix(member, "subscription:"))
			}
			for _, proxy := range proxies {
				if strings.HasPrefix(proxy.Name, subscriptionName+" / ") {
					appendMember(proxy.Name)
				}
			}
			continue
		}
		if strings.HasPrefix(member, "group-id:") || strings.HasPrefix(member, "group:") {
			groupName := ""
			if strings.HasPrefix(member, "group-id:") {
				id, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(member, "group-id:")), 10, 64)
				groupName = groupNames[id]
			} else {
				groupName = strings.TrimSpace(strings.TrimPrefix(member, "group:"))
			}
			appendMember(groupName)
			continue
		}
		appendMember(member)
	}
	return result
}

func maxInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
func (a *App) handlePublicSubscription(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/sub/")
	if raw == "" {
		http.NotFound(w, r)
		return
	}
	var id int64
	var enabled int
	var name string
	var monthlyDataGB float64
	err := a.db.QueryRow(`SELECT id,name,enabled,monthly_data_gb FROM access_keys WHERE key_hash=?`, hashToken(raw)).Scan(&id, &name, &enabled, &monthlyDataGB)
	if err != nil || enabled != 1 {
		http.NotFound(w, r)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = a.db.Exec(`UPDATE access_keys SET last_used_at=? WHERE id=?`, now, id)
	// A client refresh should be based on the latest snapshots from every
	// enabled subscription. Serialize batches to avoid a burst of client
	// requests causing duplicate upstream refreshes.
	a.publicSubscriptionMu.Lock()
	a.refreshAllSubscriptions()
	a.publicSubscriptionMu.Unlock()
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	name = strings.TrimSpace(name)
	if name != "" {
		w.Header().Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte(name)))
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name + ".yaml"}))
	}
	if monthlyDataGB > 0 {
		nextMonth := time.Date(time.Now().UTC().Year(), time.Now().UTC().Month()+1, 1, 0, 0, 0, 0, time.UTC)
		totalBytes := uint64(monthlyDataGB * float64(1024*1024*1024))
		w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=0; download=0; total=%d; expire=%d", totalBytes, nextMonth.Unix()))
	}
	w.WriteHeader(200)
	_, _ = io.WriteString(w, a.generateConfig())
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived := deriveKey([]byte(password), salt, 120000)
	return "v1$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(derived), nil
}
func verifyPassword(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 3 {
		return false
	}
	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	actual := deriveKey([]byte(password), salt, 120000)
	return string(actual) == string(expected)
}
func deriveKey(password, salt []byte, rounds int) []byte {
	result := append([]byte{}, salt...)
	for i := 0; i < rounds; i++ {
		h := sha256.New()
		h.Write(result)
		h.Write(password)
		result = h.Sum(nil)
	}
	return result
}
func randomToken(size int) string {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return hex.EncodeToString(data)
}
func hashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func validHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
func pathID(path, prefix string) (int64, bool) {
	value := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if strings.Contains(value, "/") {
		return 0, false
	}
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil
}
func countProxyLines(body []byte) int { return strings.Count(string(body), "- name:") }
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(dest); err != nil {
		writeError(w, 400, "Invalid JSON")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeYAML(w http.ResponseWriter, value string) {
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.WriteHeader(200)
	_, _ = io.WriteString(w, value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func methodNotAllowed(w http.ResponseWriter) { writeError(w, 405, "Method not allowed") }
