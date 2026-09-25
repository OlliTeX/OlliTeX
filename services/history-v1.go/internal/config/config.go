// Package config is the 1:1 port of the Node `history-v1` config layer:
// `config/default.json` + `config/custom-environment-variables.json`.
//
// Every field maps to a Node `config.get('a.b.c')` key, and every env override
// mirrors `custom-environment-variables.json` (the ONLY source of env names).
// Numeric-looking values in default.json are strings and get parsed; env vars
// are parsed from strings.
package config

import (
	"os"
	"strconv"
)

// Config mirrors every key of `config/default.json` +
// `config/custom-environment-variables.json`.
type Config struct {
	// database
	DatabaseURL         string // HISTORY_CONNECTION_STRING
	DatabaseURLReadOnly string // HISTORY_FOLLOWER_CONNECTION_STRING
	HerokuDatabaseURL   string // DATABASE_URL
	DatabasePoolMin     int    // DATABASE_POOL_MIN, default 2
	DatabasePoolMax     int    // DATABASE_POOL_MAX, default 10

	// persistor (s3 / gcs / fallback)
	PersistorBackend                string // PERSISTOR_BACKEND, default "s3"
	PersistorS3Key                  string // AWS_ACCESS_KEY_ID
	PersistorS3Secret               string // AWS_SECRET_ACCESS_KEY
	PersistorS3Endpoint             string // AWS_S3_ENDPOINT
	PersistorS3PathStyle            *bool  // AWS_S3_PATH_STYLE (no default key)
	PersistorS3MaxRetries           int    // S3_MAX_RETRIES, default 1
	PersistorS3Timeout              int    // S3_TIMEOUT ms, default 8000
	PersistorS3SignedUrlExpiryInMs  int64  // default.json 1800000
	PersistorGCSDDeleteBucketSuffix string // GCS_DELETED_BUCKET_SUFFIX
	PersistorGCSUnlockBeforeDelete  *bool  // GCS_UNLOCK_BEFORE_DELETE
	PersistorGCSApiEndpoint         string // GCS_API_ENDPOINT
	PersistorGCSProjectId           string // GCS_PROJECT_ID
	PersistorGCSMaxRetries          int    // GCS_MAX_RETRIES (no default key -> 0)
	PersistorGCSIdempotencyStrategy string // GCS_IDEMPOTENCY_STRATEGY
	PersistorGCSDeleteConcurrency   int    // default.json gcs.deleteConcurrency "50"
	PersistorFallbackBackend        string // PERSISTOR_FALLBACK_BACKEND
	PersistorFallbackBuckets        string // PERSISTOR_BUCKET_MAPPING

	// backupPersistor (s3SSEC)
	BackupKeyEncryptionKeys string // BACKUP_KEY_ENCRYPTION_KEYS
	BackupBackend           string // backupPersistor.backend default "s3SSEC" (no env override)
	BackupS3Key             string // AWS_ACCESS_KEY_ID
	BackupS3Secret          string // AWS_SECRET_ACCESS_KEY
	BackupS3Endpoint        string // AWS_S3_ENDPOINT
	BackupS3PathStyle       *bool  // AWS_S3_PATH_STYLE
	BackupS3MaxRetries      int    // BACKUP_S3_MAX_RETRIES, default 1
	BackupS3Timeout         int    // BACKUP_S3_TIMEOUT ms, default 120000

	// bucket names
	GlobalBucket             string // OVERLEAF_EDITOR_BLOBS_BUCKET
	ProjectBucket            string // OVERLEAF_EDITOR_PROJECT_BLOBS_BUCKET
	ChunksBucket             string // OVERLEAF_EDITOR_CHUNKS_BUCKET
	ZipBucket                string // OVERLEAF_EDITOR_ZIPS_BUCKET
	BackupChunksBucket       string // BACKUP_OVERLEAF_EDITOR_CHUNKS_BUCKET
	BackupDEKsBucket         string // BACKUP_OVERLEAF_EDITOR_DEKS_BUCKET
	BackupGlobalBlobsBucket  string // BACKUP_OVERLEAF_EDITOR_GLOBAL_BLOBS_BUCKET
	BackupProjectBlobsBucket string // BACKUP_OVERLEAF_EDITOR_PROJECT_BLOBS_BUCKET

	// health checks
	HealthCheckBlobs    bool // HEALTH_CHECK_BLOBS
	HealthCheckProjects bool // HEALTH_CHECK_PROJECTS

	// backup RPO + soft delete + deletes
	BackupRPOInMS             int  // BACKUP_RPO_IN_MS, default 3600000
	MinSoftDeletionPeriodDays int  // MIN_SOFT_DELETION_PERIOD_DAYS, default 90
	MaxDeleteKeys             int  // maxDeleteKeys default 1000 (no env)
	UseDeleteObjects          bool // useDeleteObjects default true (no env)
	ClusterWorkers            int  // CLUSTER_WORKERS default 1

	// chunk store
	HistoryStoreConcurrency int // HISTORY_STORE_CONCURRENCY, default 4

	// zip store
	ZipStoreZipTimeoutMs int // ZIP_STORE_ZIP_TIMEOUT_MS, default 360000

	// misc
	HasProjectsWithoutHistory bool // hasProjectsWithoutHistory default false

	// mongo
	MongoURI string // MONGO_CONNECTION_STRING (default dev/test: mongodb://mongo:27017/sharelatex)

	// auth
	BasicHttpAuthPassword    string // STAGING_PASSWORD
	BasicHttpAuthOldPassword string // BASIC_HTTP_AUTH_OLD_PASSWORD
	JWTAuthKey               string // OT_JWT_AUTH_KEY
	JWTAuthOldKey            string // OT_JWT_AUTH_OLD_KEY
	JWTAuthAlgorithm         string // OT_JWT_AUTH_ALG, default HS256

	// history buffer rollout
	HistoryBufferLevel                      *int // HISTORY_BUFFER_LEVEL
	ForcePersistBuffer                      bool // FORCE_PERSIST_BUFFER
	NextHistoryBufferLevel                  int  // NEXT_HISTORY_BUFFER_LEVEL
	NextHistoryBufferLevelRolloutPercentage int  // NEXT_HISTORY_BUFFER_LEVEL_ROLLOUT_PERCENTAGE

	// redis clients (queue / history / lock)
	RedisQueueHost       string // QUEUES_REDIS_HOST
	RedisQueuePassword   string // QUEUES_REDIS_PASSWORD
	RedisQueuePort       string // QUEUES_REDIS_PORT (kept as string: empty -> default 6379)
	RedisHistoryHost     string // HISTORY_REDIS_HOST
	RedisHistoryPassword string // HISTORY_REDIS_PASSWORD
	RedisHistoryPort     string // HISTORY_REDIS_PORT
	RedisLockHost        string // REDIS_HOST
	RedisLockPassword    string // REDIS_PASSWORD
	RedisLockPort        string // REDIS_PORT

	// projectHistory
	ProjectHistoryHost string // PROJECT_HISTORY_HOST
	ProjectHistoryPort int    // PROJECT_HISTORY_PORT, default 3054

	// server
	MaxFileUploadSize  int64 // MAX_FILE_UPLOAD_SIZE default 52428800
	HTTPSOnly          bool  // HTTPS_ONLY default false
	HTTPRequestTimeout int64 // HTTP_REQUEST_TIMEOUT default 300000
	// Port: listen port, `process.env.PORT || 3100` (app.js).
	Port int
}

// FromEnv builds a full Config from default.json values + env overrides listed
// in custom-environment-variables.json.
func FromEnv() *Config {
	c := &Config{
		// database
		DatabasePoolMin: 2,
		DatabasePoolMax: 10,
		// persistor
		PersistorBackend:               "s3",
		PersistorS3MaxRetries:          1,
		PersistorS3SignedUrlExpiryInMs: 1800000,
		PersistorGCSDeleteConcurrency:  50,
		PersistorS3Timeout:             8000,
		// backupPersistor
		BackupBackend:      "s3SSEC",
		BackupS3MaxRetries: 1,
		BackupS3Timeout:    120000,
		// backup RPO + soft delete + deletes
		BackupRPOInMS:             3600000,
		MinSoftDeletionPeriodDays: 90,
		MaxDeleteKeys:             1000,
		UseDeleteObjects:          true,
		HasProjectsWithoutHistory: false,
		ClusterWorkers:            1,
		// chunkStore
		HistoryStoreConcurrency: 4,
		// zip
		ZipStoreZipTimeoutMs: 360000,
		// mongo (production default; overridden via env)
		MongoURI: "mongodb://127.0.0.1:27017/sharelatex",
		// projectHistory
		ProjectHistoryPort: 3054,
		// server
		MaxFileUploadSize:  52428800,
		HTTPSOnly:          false,
		HTTPRequestTimeout: 300000,
		Port:               3100,
	}
	applyEnv(c)
	return c
}

func applyEnv(c *Config) {
	// database
	c.DatabaseURL = envStr("HISTORY_CONNECTION_STRING", c.DatabaseURL)
	c.DatabaseURLReadOnly = envStr("HISTORY_FOLLOWER_CONNECTION_STRING", c.DatabaseURLReadOnly)
	c.HerokuDatabaseURL = envStr("DATABASE_URL", c.HerokuDatabaseURL)
	c.DatabasePoolMin = envInt("DATABASE_POOL_MIN", c.DatabasePoolMin)
	c.DatabasePoolMax = envInt("DATABASE_POOL_MAX", c.DatabasePoolMax)
	// persistor
	c.PersistorBackend = envStr("PERSISTOR_BACKEND", c.PersistorBackend)
	c.PersistorS3Key = envStr("AWS_ACCESS_KEY_ID", c.PersistorS3Key)
	c.PersistorS3Secret = envStr("AWS_SECRET_ACCESS_KEY", c.PersistorS3Secret)
	c.PersistorS3Endpoint = envStr("AWS_S3_ENDPOINT", c.PersistorS3Endpoint)
	c.PersistorS3PathStyle = envBoolPtr("AWS_S3_PATH_STYLE", c.PersistorS3PathStyle)
	c.PersistorS3MaxRetries = envInt("S3_MAX_RETRIES", c.PersistorS3MaxRetries)
	c.PersistorS3Timeout = envInt("S3_TIMEOUT", c.PersistorS3Timeout)
	c.PersistorGCSDDeleteBucketSuffix = envStr("GCS_DELETED_BUCKET_SUFFIX", c.PersistorGCSDDeleteBucketSuffix)
	c.PersistorGCSUnlockBeforeDelete = envBoolPtr("GCS_UNLOCK_BEFORE_DELETE", c.PersistorGCSUnlockBeforeDelete)
	c.PersistorGCSApiEndpoint = envStr("GCS_API_ENDPOINT", c.PersistorGCSApiEndpoint)
	c.PersistorGCSProjectId = envStr("GCS_PROJECT_ID", c.PersistorGCSProjectId)
	c.PersistorGCSMaxRetries = envInt("GCS_MAX_RETRIES", c.PersistorGCSMaxRetries)
	c.PersistorGCSIdempotencyStrategy = envStr("GCS_IDEMPOTENCY_STRATEGY", c.PersistorGCSIdempotencyStrategy)
	c.PersistorFallbackBackend = envStr("PERSISTOR_FALLBACK_BACKEND", c.PersistorFallbackBackend)
	c.PersistorFallbackBuckets = envStr("PERSISTOR_BUCKET_MAPPING", c.PersistorFallbackBuckets)
	// backup
	c.BackupKeyEncryptionKeys = envStr("BACKUP_KEY_ENCRYPTION_KEYS", c.BackupKeyEncryptionKeys)
	// Note: backupPersistor.s3SSEC.key/secret map to AWS_ACCESS_KEY_ID /
	// AWS_SECRET_ACCESS_KEY (same as persistor.s3).
	c.BackupS3Key = envStr("AWS_ACCESS_KEY_ID", c.BackupS3Key)
	c.BackupS3Secret = envStr("AWS_SECRET_ACCESS_KEY", c.BackupS3Secret)
	c.BackupS3Endpoint = envStr("AWS_S3_ENDPOINT", c.BackupS3Endpoint)
	c.BackupS3PathStyle = envBoolPtr("AWS_S3_PATH_STYLE", c.BackupS3PathStyle)
	c.BackupS3MaxRetries = envInt("BACKUP_S3_MAX_RETRIES", c.BackupS3MaxRetries)
	c.BackupS3Timeout = envInt("BACKUP_S3_TIMEOUT", c.BackupS3Timeout)
	// buckets
	c.GlobalBucket = envStr("OVERLEAF_EDITOR_BLOBS_BUCKET", c.GlobalBucket)
	c.ProjectBucket = envStr("OVERLEAF_EDITOR_PROJECT_BLOBS_BUCKET", c.ProjectBucket)
	c.ChunksBucket = envStr("OVERLEAF_EDITOR_CHUNKS_BUCKET", c.ChunksBucket)
	c.ZipBucket = envStr("OVERLEAF_EDITOR_ZIPS_BUCKET", c.ZipBucket)
	c.BackupChunksBucket = envStr("BACKUP_OVERLEAF_EDITOR_CHUNKS_BUCKET", c.BackupChunksBucket)
	c.BackupDEKsBucket = envStr("BACKUP_OVERLEAF_EDITOR_DEKS_BUCKET", c.BackupDEKsBucket)
	c.BackupGlobalBlobsBucket = envStr("BACKUP_OVERLEAF_EDITOR_GLOBAL_BLOBS_BUCKET", c.BackupGlobalBlobsBucket)
	c.BackupProjectBlobsBucket = envStr("BACKUP_OVERLEAF_EDITOR_PROJECT_BLOBS_BUCKET", c.BackupProjectBlobsBucket)
	// health
	c.HealthCheckBlobs = envBool("HEALTH_CHECK_BLOBS", c.HealthCheckBlobs)
	c.HealthCheckProjects = envBool("HEALTH_CHECK_PROJECTS", c.HealthCheckProjects)
	// backup RPO + delete
	c.BackupRPOInMS = envInt("BACKUP_RPO_IN_MS", c.BackupRPOInMS)
	c.MinSoftDeletionPeriodDays = envInt("MIN_SOFT_DELETION_PERIOD_DAYS", c.MinSoftDeletionPeriodDays)
	c.ClusterWorkers = envInt("CLUSTER_WORKERS", c.ClusterWorkers)
	// chunk store
	c.HistoryStoreConcurrency = envInt("HISTORY_STORE_CONCURRENCY", c.HistoryStoreConcurrency)
	// zip
	c.ZipStoreZipTimeoutMs = envInt("ZIP_STORE_ZIP_TIMEOUT_MS", c.ZipStoreZipTimeoutMs)
	// misc
	c.MongoURI = envStr("MONGO_CONNECTION_STRING", c.MongoURI)
	// auth
	c.BasicHttpAuthPassword = envStr("STAGING_PASSWORD", c.BasicHttpAuthPassword)
	c.BasicHttpAuthOldPassword = envStr("BASIC_HTTP_AUTH_OLD_PASSWORD", c.BasicHttpAuthOldPassword)
	c.JWTAuthKey = envStr("OT_JWT_AUTH_KEY", c.JWTAuthKey)
	c.JWTAuthOldKey = envStr("OT_JWT_AUTH_OLD_KEY", c.JWTAuthOldKey)
	c.JWTAuthAlgorithm = envStr("OT_JWT_AUTH_ALG", c.JWTAuthAlgorithm)
	if c.JWTAuthAlgorithm == "" {
		c.JWTAuthAlgorithm = "HS256"
	}
	// history buffer rollout
	c.ForcePersistBuffer = envBool("FORCE_PERSIST_BUFFER", c.ForcePersistBuffer)
	if v := os.Getenv("HISTORY_BUFFER_LEVEL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.HistoryBufferLevel = &n
		}
	}
	c.NextHistoryBufferLevel = envInt("NEXT_HISTORY_BUFFER_LEVEL", c.NextHistoryBufferLevel)
	c.NextHistoryBufferLevelRolloutPercentage = envInt("NEXT_HISTORY_BUFFER_LEVEL_ROLLOUT_PERCENTAGE", c.NextHistoryBufferLevelRolloutPercentage)
	// redis (kept as strings; empty -> default 6379)
	c.RedisQueueHost = envStr("QUEUES_REDIS_HOST", c.RedisQueueHost)
	c.RedisQueuePassword = envStr("QUEUES_REDIS_PASSWORD", c.RedisQueuePassword)
	c.RedisQueuePort = envStr("QUEUES_REDIS_PORT", c.RedisQueuePort)
	c.RedisHistoryHost = envStr("HISTORY_REDIS_HOST", c.RedisHistoryHost)
	c.RedisHistoryPassword = envStr("HISTORY_REDIS_PASSWORD", c.RedisHistoryPassword)
	c.RedisHistoryPort = envStr("HISTORY_REDIS_PORT", c.RedisHistoryPort)
	c.RedisLockHost = envStr("REDIS_HOST", c.RedisLockHost)
	c.RedisLockPassword = envStr("REDIS_PASSWORD", c.RedisLockPassword)
	c.RedisLockPort = envStr("REDIS_PORT", c.RedisLockPort)
	// projectHistory
	c.ProjectHistoryHost = envStr("PROJECT_HISTORY_HOST", c.ProjectHistoryHost)
	c.ProjectHistoryPort = envInt("PROJECT_HISTORY_PORT", c.ProjectHistoryPort)
	// server
	c.MaxFileUploadSize = envInt64("MAX_FILE_UPLOAD_SIZE", c.MaxFileUploadSize)
	c.HTTPSOnly = envBool("HTTPS_ONLY", c.HTTPSOnly)
	c.HTTPRequestTimeout = envInt64("HTTP_REQUEST_TIMEOUT", c.HTTPRequestTimeout)
	c.Port = envInt("PORT", c.Port)
}

func envStr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt64(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func envBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		return v == "true"
	}
	return def
}

func envBoolPtr(k string, def *bool) *bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	b := v == "true"
	return &b
}
