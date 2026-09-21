package persistors

import (
	"strings"
	"testing"

	"ollitex/go/libraries/oerror"
)

// --- PersistorFactory (port of test/unit/PersistorFactoryTests.js) ---------

func TestFactoryNoBackend(t *testing.T) {
	_, err := Create(Settings{}, Adapters{})
	if !isSettingsError(err) {
		t.Fatalf("want SettingsError, got %T (%v)", err, err)
	}
	if !strings.Contains(err.Error(), "no backend specified - config incomplete") {
		t.Fatalf("message: %v", err)
	}
}

func isSettingsError(err error) bool {
	_, ok := asPersistorError(err).(*SettingsError)
	return ok
}

func TestFactoryUnknownBackend(t *testing.T) {
	_, err := Create(Settings{Backend: "magic"}, Adapters{})
	if !isSettingsError(err) {
		t.Fatalf("want SettingsError, got %T (%v)", err, err)
	}
	full := oerror.GetFullInfo(err)
	if full["backend"] != "magic" {
		t.Fatalf("info.backend = %v", full)
	}
	if !strings.Contains(err.Error(), "unknown backend") {
		t.Fatalf("message: %v", err)
	}
}

func TestFactoryFS(t *testing.T) {
	p, err := Create(Settings{Backend: "fs", UseSubdirectories: true}, Adapters{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*FSPersistor); !ok {
		t.Fatalf("want *FSPersistor, got %T", p)
	}
	if settings, err := NewFSPersistor(FSSettings{UseSubdirectories: false}); err == nil {
		_ = settings
	}
}

func TestFactoryS3(t *testing.T) {
	f := newFakeS3(t)
	p, err := Create(Settings{
		Backend: "s3",
		S3:      &S3Settings{Key: "frog", Secret: "prince"},
	}, Adapters{S3: func(bucket string) (S3Client, error) { return f, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*S3Persistor); !ok {
		t.Fatalf("want *S3Persistor, got %T", p)
	}
}

func TestFactoryS3MissingSettings(t *testing.T) {
	_, err := Create(Settings{Backend: "s3"}, Adapters{})
	if !isSettingsError(err) || !strings.Contains(err.Error(), "no s3 settings provided") {
		t.Fatalf("got %v", err)
	}
}

func TestFactoryGCS(t *testing.T) {
	p, err := Create(Settings{
		Backend: "gcs",
		GCS:     &GCSSettings{Endpoint: GCSBackendEndpoint{ProjectID: "p"}},
	}, Adapters{GCS: func(map[string]any) (GCSStorage, error) { return newFakeGCS(), nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*GcsPersistor); !ok {
		t.Fatalf("want *GcsPersistor, got %T", p)
	}
}

func TestFactoryGCSMissingSettings(t *testing.T) {
	_, err := Create(Settings{Backend: "gcs"}, Adapters{})
	if !isSettingsError(err) || !strings.Contains(err.Error(), "no gcs settings provided") {
		t.Fatalf("got %v", err)
	}
}

func TestFactoryS3SSEC(t *testing.T) {
	f := newFakeS3(t)
	s := EncryptedS3Settings{
		S3Settings:                  S3Settings{Key: "frog", Secret: "prince"},
		DataEncryptionKeyBucketName: dekBucket,
		PathToProjectFolder:         func(b, p string) string { return "proj/" + p },
		GetRootKeyEncryptionKeys: func() ([]*RootKeyEncryptionKey, error) {
			return []*RootKeyEncryptionKey{mustKEK(t, 1)}, nil
		},
	}
	p, err := Create(Settings{Backend: "s3SSEC", S3SSEC: &s}, Adapters{S3: func(string) (S3Client, error) { return f, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*PerProjectEncryptedS3Persistor); !ok {
		t.Fatalf("want *PerProjectEncryptedS3Persistor, got %T", p)
	}
}

func TestFactoryS3SSECMissingSettings(t *testing.T) {
	_, err := Create(Settings{Backend: "s3SSEC"}, Adapters{})
	if !isSettingsError(err) || !strings.Contains(err.Error(), "no s3SSEC settings provided") {
		t.Fatalf("got %v", err)
	}
}

func TestFactoryFallback(t *testing.T) {
	f := newFakeS3(t)
	adapters := Adapters{S3: func(string) (S3Client, error) { return f, nil }}
	p, err := Create(Settings{
		Backend: "s3",
		S3:      &S3Settings{Key: "frog", Secret: "prince"},
		Fallback: &FallbackSettings{
			Backend:    "s3",
			Buckets:    map[string]string{migBucket: migFBBucket},
			CopyOnMiss: true,
		},
	}, adapters)
	if err != nil {
		t.Fatal(err)
	}
	mig, ok := p.(*MigrationPersistor)
	if !ok {
		t.Fatalf("want *MigrationPersistor, got %T", p)
	}
	// the migration must wrap the real primary
	if _, ok := mig.primary.(*S3Persistor); !ok {
		t.Fatalf("primary should be S3: %T", mig.primary)
	}
}

func TestObjectPersistorEntry(t *testing.T) {
	p, err := ObjectPersistor(Settings{Backend: "fs"}, Adapters{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*FSPersistor); !ok {
		t.Fatalf("want *FSPersistor, got %T", p)
	}
}
