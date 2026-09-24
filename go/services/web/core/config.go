package core

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config mirrors the settings the web app consumes (server-ce
// settings.js + services/web defaults). Only the keys the P0/P1 surface
// touches are read; each new feature adds its keys with a comment
// pointing at the Node source it mirrors.
type Config struct {
	// Profile: "web" (127.0.0.1:4000 under runit) or "api" (0.0.0.0:3000)
	// — selected by ENABLED_SERVICES exactly like the Node app.
	Profile string

	ListenAddr string // host:port (WEB_PORT / API override via WEB_GO_LISTEN)

	// sessions
	SessionSecrets []string // [OVERLEAF_SESSION_SECRET|CRYPTO_RANDOM, *_UPCOMING, *_FALLBACK]
	CookieName     string   // COOKIE_NAME || "overleaf.sid"
	CookieLength   time.Duration
	CookieLengthMS int64
	SecureCookie   bool   // OVERLEAF_SECURE_COOKIE set (server-ce: != null)
	SameSite       string // default "lax"
	RollingSession bool   // default true
	CookieDomain   string // COOKIE_DOMAIN (empty = absent cookie domain attr)

	// app identity / misc
	AppName           string   // APP_NAME || "OlliTeX" (settings.appName)
	SiteURL           string   // SITE_URL || "http://localhost:8000"
	AllowedOrigins    []string // ALLOWED_ORIGINS || siteUrl (comma list)
	ExpoHostname      bool     // EXPOSE_HOSTNAME
	AllowPublicAccess bool     // OVERLEAF_ALLOW_PUBLIC_ACCESS === 'true' (disables the global login gate)

	CacheStaticAssets bool // server-ce: true

	// backends
	RedisAddr     string // REDIS_HOST:REDIS_PORT (default 127.0.0.1:6379)
	RedisPassword string // REDIS_PASSWORD (empty = no AUTH — the e2e/live redis)
	RedisDB       string // REDIS_DB index ('' or '0' = default)
	MongoURI      string // mongoh chain (MONGO_CONNECTION_STRING || OVERLEAF_MONGO_URL || mongodb://HOST/sharelatex)

	// Feature directories
	PublicDir  string // services/web/public (static root)
	LocalesDir string // services/web/locales
	ViewsDir   string // not used by Go (templates live in go/...), kept for parity scripts

	// health checks
	SmokeTestUserID string // SMOKE_TEST_USER_ID (empty → /health_check/mongo 500, Node parity)

	// api profile
	APIUser     string // WEB_API_USER
	APIPassword string // WEB_API_PASSWORD

	// analytics feature gate: Node Features.hasFeature('analytics') =
	// Boolean(Settings.apis.v1.url) — apis.v1 unset in this stack, so the
	// whole analytics surface is the 202 short-circuit (U10.2).
	V1APIURL string // APIS_V1_URL (empty → feature off)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// LoadConfig reads the env contract (same variable names the Node stack
// runs on — the run scripts source /etc/overleaf/env.sh first, so both
// stacks see identical values).
func LoadConfig() (*Config, error) {
	profile := "web"
	if strings.Contains(os.Getenv("ENABLED_SERVICES"), "api") && !strings.Contains(os.Getenv("ENABLED_SERVICES"), "web") {
		profile = "api"
	} else if os.Getenv("ENABLED_SERVICES") == "" {
		// Node default is 'web'+? — server-ce run scripts always set it;
		// standalone `node app.mjs` (api) uses explicit env. Default web.
		profile = "web"
	}

	port := env("WEB_PORT", "4000")
	if profile == "api" {
		port = env("OVERLEAF_HTTP2_PORT", "3000")
	}
	host := "127.0.0.1"
	if profile == "api" {
		host = "0.0.0.0"
	}
	listen := env("WEB_GO_LISTEN", host+":"+port)

	cookieLen := int64(5 * 24 * 60 * 60 * 1000) // 5d default (server-ce)
	if v := os.Getenv("OVERLEAF_COOKIE_SESSION_LENGTH"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cookieLen = n
		}
	}

	secrets := []string{os.Getenv("OVERLEAF_SESSION_SECRET")}
	if secrets[0] == "" {
		secrets[0] = os.Getenv("CRYPTO_RANDOM")
	}
	secrets = append(secrets,
		os.Getenv("SESSION_SECRET_UPCOMING"),
		os.Getenv("SESSION_SECRET_FALLBACK"),
	)
	cleaned := secrets[:0]
	for _, s := range secrets {
		if s != "" {
			cleaned = append(cleaned, s)
		}
	}
	if len(cleaned) == 0 {
		return nil, &ConfigError{Msg: "No SESSION_SECRET provided (OVERLEAF_SESSION_SECRET or CRYPTO_RANDOM)"}
	}

	// Image-lineage contract (server-ce/config/settings.js — what Node web
	// actually reads in this tree): OVERLEAF_REDIS_HOST (default
	// 'dockerhost' in the stock image; e2e sets 'redis'), OVERLEAF_REDIS_PORT,
	// OVERLEAF_REDIS_PASS. services/web defaults accept REDIS_* too, so both
	// are honored.
	redisHost := env("OVERLEAF_REDIS_HOST", env("REDIS_HOST", "127.0.0.1"))
	redisPort := env("OVERLEAF_REDIS_PORT", env("REDIS_PORT", "6379"))
	redisPass := env("OVERLEAF_REDIS_PASS", env("REDIS_PASSWORD", ""))

	// OVERLEAF_MONGO_URL first (image contract); the other two kept as
	// fallbacks for host-routed deployments (mongoh chain).
	mongoURI := os.Getenv("OVERLEAF_MONGO_URL")
	if mongoURI == "" {
		mongoURI = os.Getenv("MONGO_CONNECTION_STRING")
	}
	if mongoURI == "" {
		mongoURI = "mongodb://" + env("MONGO_HOST", "127.0.0.1") + "/sharelatex"
	}

	// Node: siteUrl = process.env.OVERLEAF_SITE_URL || 'http://localhost'
	// (server-ce/config/settings.js) — keep SITE_URL as a legacy alias.
	siteURL := func() string {
		if v := os.Getenv("OVERLEAF_SITE_URL"); v != "" {
			return v
		}
		if v := os.Getenv("SITE_URL"); v != "" {
			return v
		}
		return "http://localhost"
	}()
	allowedOrigins := []string{siteURL}
	if v := os.Getenv("ALLOWED_ORIGINS"); v != "" {
		allowedOrigins = strings.Split(v, ",")
	}

	_, pb := os.LookupEnv("OVERLEAF_SECURE_COOKIE") // parity: server-ce `!= null`
	rolling := true
	if v := os.Getenv("COOKIE_ROLLING_SESSION"); v == "false" {
		rolling = false
	}

	var publicDir, localesDir string
	if d := os.Getenv("WEB_GO_PUBLIC_DIR"); d != "" {
		publicDir = d
	} else if d := os.Getenv("OVERLEAF_HOME"); d != "" {
		publicDir = d + "/services/web/public"
		localesDir = d + "/services/web/locales"
	}
	if publicDir == "" {
		publicDir = "/overleaf/services/web/public"
		localesDir = "/overleaf/services/web/locales"
	}

	return &Config{
		Profile:           profile,
		ListenAddr:        listen,
		SessionSecrets:    cleaned,
		CookieName:        env("COOKIE_NAME", "overleaf.sid"),
		CookieLength:      time.Duration(cookieLen) * time.Millisecond,
		CookieLengthMS:    cookieLen,
		SecureCookie:      pb,
		SameSite:          "lax",
		RollingSession:    rolling,
		CookieDomain:      os.Getenv("COOKIE_DOMAIN"),
		AppName:           env("APP_NAME", "OlliTeX"),
		SiteURL:           siteURL,
		AllowedOrigins:    allowedOrigins,
		ExpoHostname:      os.Getenv("EXPOSE_HOSTNAME") == "true",
		AllowPublicAccess: os.Getenv("OVERLEAF_ALLOW_PUBLIC_ACCESS") == "true",
		CacheStaticAssets: os.Getenv("CACHE_STATIC_ASSETS") != "false",
		RedisAddr:         redisHost + ":" + redisPort,
		RedisPassword:     redisPass,
		RedisDB:           env("REDIS_DB", ""),
		MongoURI:          mongoURI,
		PublicDir:         publicDir,
		LocalesDir:        localesDir,
		SmokeTestUserID:   os.Getenv("SMOKE_TEST_USER_ID"),
		APIUser:           os.Getenv("WEB_API_USER"),
		APIPassword:       os.Getenv("WEB_API_PASSWORD"),
	}, nil
}

type ConfigError struct{ Msg string }

func (e *ConfigError) Error() string { return "webgo config: " + e.Msg }
