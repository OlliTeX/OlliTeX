// Package config mirrors config/settings.defaults.cjs → Go env config.
package config

import (
	"net"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// Mongo
	MongoURL string
	// Listen
	ListenHost string
	Port       int
	// APIs
	DocUpdaterURL      string
	DocStoreURL        string
	FileStoreEnabled   bool
	FileStoreURL       string
	WebURL             string
	WebUser            string
	WebPass            string
	HistoryIDCacheSize int
	// Redis (lock)
	RedisLockHost     string
	RedisLockPort     int
	RedisLockPassword string
	// Redis (project history ops)
	RedisPHHost string
	RedisPHPort int
	RedisPHPass string
	// Health
	HealthCheckProjectID string
	// V1 history sync backend
	V1HistoryURL       string
	V1User             string
	V1Pass             string
	SyncRetriesMax     int
	SyncInterval       int
	V1RequestTimeoutMs int
	// Files
	UploadFolder              string
	MaxFileSizeInBytes        int
	ShortHistoryQueues        []string
	EstimateCompressionSample int
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// hostURL builds http://host:port from a single host env var with a default port.
func hostURL(hostEnv string, defPort int) string {
	host := envOr(hostEnv, "127.0.0.1")
	return "http://" + host + ":" + strconv.Itoa(defPort)
}

// Load reads the settings.defaults.cjs env-var set.
func Load() *Config {
	redisHost := envOr("REDIS_HOST", "127.0.0.1")
	redisPort := envOrInt("REDIS_PORT", 6379)
	redisPass := os.Getenv("REDIS_PASSWORD")

	phHost := envOr("HISTORY_REDIS_HOST", redisHost)
	phPort := redisPort
	if v := os.Getenv("HISTORY_REDIS_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			phPort = n
		}
	}
	phPass := os.Getenv("HISTORY_REDIS_PASSWORD")
	if phPass == "" {
		phPass = redisPass
	}

	// V1 history host: V1_HISTORY_HOST or HISTORY_V1_HOST, port 3100, /api path.
	v1Host := envOr("V1_HISTORY_HOST", envOr("HISTORY_V1_HOST", "127.0.0.1"))

	webHost := envOr("WEB_API_HOST", envOr("WEB_HOST", "127.0.0.1"))
	// vendor: port = WEB_API_PORT || WEB_PORT || 3000 ; user/pass = the raw env
	// (no fallback — unset means "no basic auth", matching settings.defaults).
	webPort := 3000
	if v := os.Getenv("WEB_API_PORT"); v == "" {
		if v2 := os.Getenv("WEB_PORT"); v2 != "" {
			if n, err := strconv.Atoi(v2); err == nil {
				webPort = n
			}
		}
	} else if n, err := strconv.Atoi(v); err == nil {
		webPort = n
	}
	fileStoreEnabled := os.Getenv("FILESTORE_ENABLED") != "false"

	shortQueues := []string{}
	for _, s := range strings.Split(os.Getenv("SHORT_HISTORY_QUEUES"), ",") {
		if s != "" {
			shortQueues = append(shortQueues, s)
		}
	}

	return &Config{
		MongoURL:                  envOr("MONGO_CONNECTION_STRING", "mongodb://"+envOr("MONGO_HOST", "127.0.0.1")+"/sharelatex"),
		ListenHost:                envOr("LISTEN_ADDRESS", "127.0.0.1"),
		Port:                      3054,
		DocUpdaterURL:             hostURL("DOCUPDATER_HOST", 3003),
		DocStoreURL:               hostURL("DOCSTORE_HOST", 3016),
		FileStoreEnabled:          fileStoreEnabled,
		FileStoreURL:              hostURL("FILESTORE_HOST", 3009),
		WebURL:                    "http://" + webHost + ":" + strconv.Itoa(webPort),
		WebUser:                   os.Getenv("WEB_API_USER"),
		WebPass:                   os.Getenv("WEB_API_PASSWORD"),
		HistoryIDCacheSize:        envOrInt("HISTORY_ID_CACHE_SIZE", 10000),
		RedisLockHost:             redisHost,
		RedisLockPort:             redisPort,
		RedisLockPassword:         redisPass,
		RedisPHHost:               phHost,
		RedisPHPort:               phPort,
		RedisPHPass:               phPass,
		HealthCheckProjectID:      os.Getenv("HEALTH_CHECK_PROJECT_ID"),
		V1HistoryURL:              envOr("V1_HISTORY_FULL_HOST", "http://"+v1Host+":3100/api"),
		V1User:                    envOr("V1_HISTORY_USER", "staging"),
		V1Pass:                    envOr("V1_HISTORY_PASSWORD", "password"),
		SyncRetriesMax:            30,
		SyncInterval:              2,
		V1RequestTimeoutMs:        envOrInt("V1_REQUEST_TIMEOUT", 300000),
		UploadFolder:              envOr("UPLOAD_FOLDER", "/tmp/"),
		MaxFileSizeInBytes:        100 * 1024 * 1024,
		ShortHistoryQueues:        shortQueues,
		EstimateCompressionSample: envOrInt("ESTIMATE_COMPRESSION_SAMPLE", 0),
	}
}

// Bind returns "host:port" for the http server.
func (c *Config) Bind() string { return net.JoinHostPort(c.ListenHost, strconv.Itoa(c.Port)) }

// KeySchema mirrors Settings.redis.project_history.key_schema
// (settings.defaults.cjs). Cluster-safe {id} form.
type KeySchema struct{}

func (KeySchema) ProjectHistoryOps(projectID string) string {
	return "ProjectHistory:Ops:{" + projectID + "}"
}
func (KeySchema) ProjectHistoryFirstOpTimestamp(projectID string) string {
	return "ProjectHistory:FirstOpTimestamp:{" + projectID + "}"
}
func (KeySchema) ProjectHistoryCachedHistoryID(projectID string) string {
	return "ProjectHistory:CachedHistoryId:{" + projectID + "}"
}
func (KeySchema) ProjectHistoryLock(projectID string) string {
	return "ProjectHistoryLock:{" + projectID + "}"
}

// Keys returns the redis key schema.
func (c *Config) Keys() KeySchema { return KeySchema{} }
