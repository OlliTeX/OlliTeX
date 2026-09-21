package persistors

import (
	"errors"
	"fmt"
)

// Settings mirrors the top-level settings object object-persistor consumes
// (PersistorFactory.js):
//
//	{ backend, fallback: { backend, buckets, copyOnMiss },
//	  useSubdirectories, paths, s3, s3SSEC, gcs }
type Settings struct {
	Backend           string
	Fallback          *FallbackSettings
	UseSubdirectories bool
	Paths             []string
	S3                *S3Settings
	S3SSEC            *EncryptedS3Settings
	GCS               *GCSSettings
}

// FallbackSettings mirrors `settings.fallback` (backend + the migration
// persistor options).
type FallbackSettings struct {
	Backend    string
	Buckets    map[string]string
	CopyOnMiss bool
}

// Adapters holds the client-construction seams (Node: the SDK clients are
// constructed inside the persistors; Go injects them — see s3_seam.go /
// gcs_seam.go). Nil factories fall back to the no-client behaviour each
// persistor documents.
type Adapters struct {
	S3  S3ClientFactory
	GCS GCSStorageFactory
}

// Create mirrors module.exports (PersistorFactory.create):
//
//	Logger.info({backend, fallback?.backend}, 'Loading backend')
//	no backend          → SettingsError('no backend specified - config incomplete')
//	known backend       → its persistor
//	fallback configured → MigrationPersistor(primary, fallback, settings.fallback)
func Create(settings Settings, adapters Adapters) (Persistor, error) {
	fallbackBackend := ""
	if settings.Fallback != nil {
		fallbackBackend = settings.Fallback.Backend
	}
	Logger.Info(map[string]any{"backend": settings.Backend, "fallback": fallbackBackend}, "Loading backend")

	if settings.Backend == "" {
		return nil, NewSettingsError("no backend specified - config incomplete", nil)
	}

	persistor, err := getPersistor(settings, adapters)
	if err != nil {
		return nil, err
	}

	if settings.Fallback != nil && settings.Fallback.Backend != "" {
		fallback, ferr := getPersistor(withFallbackBackend(settings), adapters)
		if ferr != nil {
			return nil, ferr
		}
		migration := NewMigrationPersistor(persistor, fallback, MigrationSettings{
			CopyOnMiss: settings.Fallback.CopyOnMiss,
			Buckets:    settings.Fallback.Buckets,
		})
		return migration, nil
	}
	return persistor, nil
}

// ObjectPersistor mirrors index.js: `function ObjectPersistor(settings) {
// return PersistorFactory(settings) }` (the library's public entry point).
func ObjectPersistor(settings Settings, adapters Adapters) (Persistor, error) {
	return Create(settings, adapters)
}

func withFallbackBackend(s Settings) Settings {
	if s.Fallback == nil {
		return s
	}
	return Settings{
		Backend: s.Fallback.Backend,
		S3:      s.S3,
		S3SSEC:  s.S3SSEC,
		GCS:     s.GCS,
	}
}

// getPersistor mirrors the factory switch.
func getPersistor(settings Settings, adapters Adapters) (Persistor, error) {
	switch settings.Backend {
	case "aws-sdk", "s3":
		if settings.S3 == nil {
			return nil, NewSettingsError("no s3 settings provided", nil)
		}
		s3s := *settings.S3
		return NewS3Persistor(s3s, adapters.S3), nil
	case "s3SSEC":
		if settings.S3SSEC == nil {
			return nil, NewSettingsError("no s3SSEC settings provided", nil)
		}
		return NewPerProjectEncryptedS3Persistor(*settings.S3SSEC, adapters.S3)
	case "fs":
		return NewFSPersistor(FSSettings{
			UseSubdirectories: settings.UseSubdirectories,
		})
	case "gcs":
		if settings.GCS == nil {
			return nil, NewSettingsError("no gcs settings provided", nil)
		}
		return NewGcsPersistor(*settings.GCS, adapters.GCS)
	default:
		return nil, NewSettingsError("unknown backend", map[string]any{"backend": settings.Backend})
	}
}

// keep errors referenced for typed helpers
var _ = errors.New

var _ = fmt.Sprintf
