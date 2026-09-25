package config

import (
	"os"
	"testing"
)

func clearHistoryEnv(t *testing.T) {
	t.Helper()
	// Unset every env var the service reads (plus PORT, which overrides the listen port).
	for _, k := range []string{
		"HISTORY_CONNECTION_STRING",
		"HISTORY_FOLLOWER_CONNECTION_STRING",
		"DATABASE_URL",
		"DATABASE_POOL_MIN",
		"DATABASE_POOL_MAX",
		"PERSISTOR_BACKEND",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_S3_ENDPOINT",
		"AWS_S3_PATH_STYLE",
		"S3_MAX_RETRIES",
		"S3_TIMEOUT",
		"GCS_DELETED_BUCKET_SUFFIX",
		"GCS_UNLOCK_BEFORE_DELETE",
		"GCS_API_ENDPOINT",
		"GCS_PROJECT_ID",
		"GCS_MAX_RETRIES",
		"GCS_IDEMPOTENCY_STRATEGY",
		"PERSISTOR_FALLBACK_BACKEND",
		"PERSISTOR_BUCKET_MAPPING",
		"BACKUP_KEY_ENCRYPTION_KEYS",
		"BACKUP_S3_MAX_RETRIES",
		"BACKUP_S3_TIMEOUT",
		"OVERLEAF_EDITOR_BLOBS_BUCKET",
		"OVERLEAF_EDITOR_PROJECT_BLOBS_BUCKET",
		"OVERLEAF_EDITOR_CHUNKS_BUCKET",
		"OVERLEAF_EDITOR_ZIPS_BUCKET",
		"BACKUP_OVERLEAF_EDITOR_CHUNKS_BUCKET",
		"BACKUP_OVERLEAF_EDITOR_DEKS_BUCKET",
		"BACKUP_OVERLEAF_EDITOR_GLOBAL_BLOBS_BUCKET",
		"BACKUP_OVERLEAF_EDITOR_PROJECT_BLOBS_BUCKET",
		"HEALTH_CHECK_BLOBS",
		"HEALTH_CHECK_PROJECTS",
		"BACKUP_RPO_IN_MS",
		"MIN_SOFT_DELETION_PERIOD_DAYS",
		"CLUSTER_WORKERS",
		"HISTORY_STORE_CONCURRENCY",
		"ZIP_STORE_ZIP_TIMEOUT_MS",
		"MONGO_CONNECTION_STRING",
		"STAGING_PASSWORD",
		"BASIC_HTTP_AUTH_OLD_PASSWORD",
		"OT_JWT_AUTH_KEY",
		"OT_JWT_AUTH_OLD_KEY",
		"OT_JWT_AUTH_ALG",
		"HISTORY_BUFFER_LEVEL",
		"FORCE_PERSIST_BUFFER",
		"NEXT_HISTORY_BUFFER_LEVEL",
		"NEXT_HISTORY_BUFFER_LEVEL_ROLLOUT_PERCENTAGE",
		"QUEUES_REDIS_HOST",
		"QUEUES_REDIS_PASSWORD",
		"QUEUES_REDIS_PORT",
		"HISTORY_REDIS_HOST",
		"HISTORY_REDIS_PASSWORD",
		"HISTORY_REDIS_PORT",
		"REDIS_HOST",
		"REDIS_PASSWORD",
		"REDIS_PORT",
		"PROJECT_HISTORY_HOST",
		"PROJECT_HISTORY_PORT",
		"MAX_FILE_UPLOAD_SIZE",
		"HTTPS_ONLY",
		"HTTP_REQUEST_TIMEOUT",
		"PORT",
	} {
		os.Unsetenv(k)
		t.Cleanup(func() { os.Unsetenv(k) })
	}
}

func TestFromEnv_DefaultsMatchNodeDefaultJSON(t *testing.T) {
	clearHistoryEnv(t)
	c := FromEnv()

	// database
	if c.DatabasePoolMin != 2 {
		t.Errorf("DatabasePoolMin = %d, want 2", c.DatabasePoolMin)
	}
	if c.DatabasePoolMax != 10 {
		t.Errorf("DatabasePoolMax = %d, want 10", c.DatabasePoolMax)
	}
	// persistor
	if c.PersistorBackend != "s3" {
		t.Errorf("PersistorBackend = %q, want s3", c.PersistorBackend)
	}
	if c.PersistorS3MaxRetries != 1 {
		t.Errorf("PersistorS3MaxRetries = %d, want 1", c.PersistorS3MaxRetries)
	}
	if c.PersistorS3Timeout != 8000 {
		t.Errorf("PersistorS3Timeout = %d, want 8000", c.PersistorS3Timeout)
	}
	if c.PersistorS3SignedUrlExpiryInMs != 1800000 {
		t.Errorf("PersistorS3SignedUrlExpiryInMs = %d, want 1800000", c.PersistorS3SignedUrlExpiryInMs)
	}
	// backup persistor
	if c.BackupBackend != "s3SSEC" {
		t.Errorf("BackupBackend = %q, want s3SSEC", c.BackupBackend)
	}
	if c.BackupS3MaxRetries != 1 {
		t.Errorf("BackupS3MaxRetries = %d, want 1", c.BackupS3MaxRetries)
	}
	if c.BackupS3Timeout != 120000 {
		t.Errorf("BackupS3Timeout = %d, want 120000", c.BackupS3Timeout)
	}
	// RPO / soft delete
	if c.BackupRPOInMS != 3600000 {
		t.Errorf("BackupRPOInMS = %d, want 3600000", c.BackupRPOInMS)
	}
	if c.MinSoftDeletionPeriodDays != 90 {
		t.Errorf("MinSoftDeletionPeriodDays = %d, want 90", c.MinSoftDeletionPeriodDays)
	}
	if c.MaxDeleteKeys != 1000 {
		t.Errorf("MaxDeleteKeys = %d, want 1000", c.MaxDeleteKeys)
	}
	if !c.UseDeleteObjects {
		t.Errorf("UseDeleteObjects = %v, want true", c.UseDeleteObjects)
	}
	if c.ClusterWorkers != 1 {
		t.Errorf("ClusterWorkers = %d, want 1", c.ClusterWorkers)
	}
	// chunk store
	if c.HistoryStoreConcurrency != 4 {
		t.Errorf("HistoryStoreConcurrency = %d, want 4", c.HistoryStoreConcurrency)
	}
	// zip
	if c.ZipStoreZipTimeoutMs != 360000 {
		t.Errorf("ZipStoreZipTimeoutMs = %d, want 360000", c.ZipStoreZipTimeoutMs)
	}
	// misc
	if c.HasProjectsWithoutHistory {
		t.Errorf("HasProjectsWithoutHistory = %v, want false", c.HasProjectsWithoutHistory)
	}
	// projectHistory
	if c.ProjectHistoryPort != 3054 {
		t.Errorf("ProjectHistoryPort = %d, want 3054", c.ProjectHistoryPort)
	}
	// server
	if c.MaxFileUploadSize != 52428800 {
		t.Errorf("MaxFileUploadSize = %d, want 52428800", c.MaxFileUploadSize)
	}
	if c.HTTPSOnly {
		t.Errorf("HTTPSOnly = %v, want false", c.HTTPSOnly)
	}
	if c.HTTPRequestTimeout != 300000 {
		t.Errorf("HTTPRequestTimeout = %d, want 300000", c.HTTPRequestTimeout)
	}
	// PORT override (app.js)
	if c.Port != 3100 {
		t.Errorf("Port = %d, want 3100 (process.env.PORT || 3100)", c.Port)
	}
	// JWT algorithm default
	if c.JWTAuthAlgorithm != "HS256" {
		t.Errorf("JWTAuthAlgorithm = %q, want HS256", c.JWTAuthAlgorithm)
	}
}

func TestFromEnv_S3Overrides(t *testing.T) {
	clearHistoryEnv(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "k")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "s")
	t.Setenv("AWS_S3_ENDPOINT", "http://localhost:9000")
	t.Setenv("AWS_S3_PATH_STYLE", "true")
	t.Setenv("S3_MAX_RETRIES", "7")
	t.Setenv("S3_TIMEOUT", "9999")

	c := FromEnv()
	if c.PersistorS3Key != "k" {
		t.Errorf("PersistorS3Key = %q", c.PersistorS3Key)
	}
	if c.PersistorS3Secret != "s" {
		t.Errorf("PersistorS3Secret = %q", c.PersistorS3Secret)
	}
	if c.PersistorS3Endpoint != "http://localhost:9000" {
		t.Errorf("PersistorS3Endpoint = %q", c.PersistorS3Endpoint)
	}
	if c.PersistorS3PathStyle == nil || !*c.PersistorS3PathStyle {
		t.Errorf("PersistorS3PathStyle = %v, want true", c.PersistorS3PathStyle)
	}
	if c.PersistorS3MaxRetries != 7 {
		t.Errorf("PersistorS3MaxRetries = %d, want 7", c.PersistorS3MaxRetries)
	}
	if c.PersistorS3Timeout != 9999 {
		t.Errorf("PersistorS3Timeout = %d, want 9999", c.PersistorS3Timeout)
	}
}

func TestFromEnv_ListenPort(t *testing.T) {
	clearHistoryEnv(t)
	t.Setenv("PORT", "3333")
	c := FromEnv()
	if c.Port != 3333 {
		t.Errorf("Port = %d, want 3333", c.Port)
	}
}

func TestFromEnv_MongoURI(t *testing.T) {
	clearHistoryEnv(t)
	t.Setenv("MONGO_CONNECTION_STRING", "mongodb://db:27017/sharelatex")
	c := FromEnv()
	if c.MongoURI != "mongodb://db:27017/sharelatex" {
		t.Errorf("MongoURI = %q", c.MongoURI)
	}
}

func TestFromEnv_Auth(t *testing.T) {
	clearHistoryEnv(t)
	t.Setenv("OT_JWT_AUTH_KEY", "secret")
	t.Setenv("STAGING_PASSWORD", "pw")
	c := FromEnv()
	if c.JWTAuthKey != "secret" {
		t.Errorf("JWTAuthKey = %q", c.JWTAuthKey)
	}
	if c.BasicHttpAuthPassword != "pw" {
		t.Errorf("BasicHttpAuthPassword = %q", c.BasicHttpAuthPassword)
	}
	if c.JWTAuthOldKey != "" {
		t.Errorf("JWTAuthOldKey = %q, want empty", c.JWTAuthOldKey)
	}
}

func TestFromEnv_Buckets(t *testing.T) {
	clearHistoryEnv(t)
	t.Setenv("OVERLEAF_EDITOR_BLOBS_BUCKET", "global-blob-bucket")
	t.Setenv("OVERLEAF_EDITOR_PROJECT_BLOBS_BUCKET", "project-blob-bucket")
	c := FromEnv()
	if c.GlobalBucket != "global-blob-bucket" {
		t.Errorf("GlobalBucket = %q", c.GlobalBucket)
	}
	if c.ProjectBucket != "project-blob-bucket" {
		t.Errorf("ProjectBucket = %q", c.ProjectBucket)
	}
}

func TestFromEnv_BackupBuckets(t *testing.T) {
	clearHistoryEnv(t)
	t.Setenv("BACKUP_OVERLEAF_EDITOR_CHUNKS_CHUNKS_BUCKET", "")
	t.Setenv("BACKUP_OVERLEAF_EDITOR_PROJECT_BLOBS_BUCKET", "backup-blob-bucket")
	c := FromEnv()
	if c.BackupProjectBlobsBucket != "backup-blob-bucket" {
		t.Errorf("BackupProjectBlobsBucket = %q", c.BackupProjectBlobsBucket)
	}
}
