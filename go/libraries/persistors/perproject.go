package persistors

import (
	"bytes"
	"context"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
)

const AES256KeyLength = 32

// RootKeyEncryptionKey mirrors `class RootKeyEncryptionKey`:
//
//	kek (32 bytes — else 'kek is not 32 bytes long') + salt;
//	forProject(prefix) → SSECOptions(HKDF-SHA256(kek, salt, info=prefix, 32)).
type RootKeyEncryptionKey struct {
	kek  []byte
	salt []byte
}

// NewRootKeyEncryptionKey mirrors the constructor.
func NewRootKeyEncryptionKey(kek, salt []byte) (*RootKeyEncryptionKey, error) {
	if len(kek) != AES256KeyLength {
		return nil, fmt.Errorf("kek is not %d bytes long", AES256KeyLength)
	}
	return &RootKeyEncryptionKey{kek: kek, salt: salt}, nil
}

// ForProject mirrors `forProject(prefix)` (HKDF-SHA256 derivation).
func (k *RootKeyEncryptionKey) ForProject(prefix string) *SSECOptions {
	// Go 1.27 crypto/hkdf: Key(h, secret, salt, info, keyLength).
	key, err := hkdf.Key(sha256.New, k.kek, k.salt, prefix, AES256KeyLength)
	if err != nil {
		return NewSSECOptions(nil) // unreachable for a valid 32-byte KKE; defensive
	}
	return NewSSECOptions(key)
}

// EncryptedS3Settings mirrors the `Settings` typedef
// (S3PersistorSettings & EncryptionSettings).
type EncryptedS3Settings struct {
	S3Settings
	AutomaticallyRotateDEKEncryption bool
	DataEncryptionKeyBucketName      string
	IgnoreErrorsFromDEKReEncryption  bool
	PathToProjectFolder              func(bucketName, path string) string
	GetRootKeyEncryptionKeys         func() ([]*RootKeyEncryptionKey, error)
}

// PerProjectEncryptedS3Persistor is the 1:1 port of
// PerProjectEncryptedS3Persistor.js (SSE-C per-project DEKs derived from
// root KEKs; DEK generate/read/rotate under a dedicated S3 bucket).
type PerProjectEncryptedS3Persistor struct {
	*S3Persistor
	settings EncryptedS3Settings
	keysOnce *keysOnce
}

type keysOnce struct {
	done bool
	keys []*RootKeyEncryptionKey
	err  error
}

// NewPerProjectEncryptedS3Persistor mirrors the constructor:
//
//	settings.dataEncryptionKeyBucketName missing → Error('settings.dataEncryptionKeyBucketName is missing')
//	getRootKeyEncryptionKeys resolved (promise) with a non-empty list,
//	    else Error('no root kek provided').
func NewPerProjectEncryptedS3Persistor(settings EncryptedS3Settings, factory S3ClientFactory) (*PerProjectEncryptedS3Persistor, error) {
	if settings.DataEncryptionKeyBucketName == "" {
		return nil, fmt.Errorf("settings.dataEncryptionKeyBucketName is missing")
	}
	base := NewS3Persistor(settings.S3Settings, factory)
	return &PerProjectEncryptedS3Persistor{
		S3Persistor: base,
		settings:    settings,
		keysOnce:    &keysOnce{},
	}, nil
}

// availableKeyEncryptionKeys mirrors `await this.#availableKeyEncryptionKeysPromise`.
func (p *PerProjectEncryptedS3Persistor) availableKeyEncryptionKeys() ([]*RootKeyEncryptionKey, error) {
	ko := p.keysOnce
	if ko.done {
		return ko.keys, ko.err
	}
	keys, err := p.settings.GetRootKeyEncryptionKeys()
	if err != nil {
		ko.done = true
		ko.err = err
		return nil, err
	}
	if len(keys) == 0 {
		ko.done = true
		ko.err = fmt.Errorf("no root kek provided")
		return nil, ko.err
	}
	ko.done = true
	ko.keys = keys
	return keys, nil
}

// buildProjectPaths mirrors #buildProjectPaths.
func (p *PerProjectEncryptedS3Persistor) buildProjectPaths(bucketName, pathName string) (projectFolder, dekPath string) {
	projectFolder = p.settings.PathToProjectFolder(bucketName, pathName)
	dekPath = path.Join(projectFolder, "dek")
	return projectFolder, dekPath
}

// currentKeyEncryptionKey mirrors #getCurrentKeyEncryptionKey.
func (p *PerProjectEncryptedS3Persistor) currentKeyEncryptionKey(projectFolder string) (*SSECOptions, error) {
	keys, err := p.availableKeyEncryptionKeys()
	if err != nil {
		return nil, err
	}
	return keys[0].ForProject(projectFolder), nil
}

// DataEncryptionKeySize mirrors getDataEncryptionKeySize.
func (p *PerProjectEncryptedS3Persistor) DataEncryptionKeySize(bucketName, pathName string) (int64, error) {
	projectFolder, dekPath := p.buildProjectPaths(bucketName, pathName)
	keys, err := p.availableKeyEncryptionKeys()
	if err != nil {
		return 0, err
	}
	for _, rootKEK := range keys {
		ssecOptions := rootKEK.ForProject(projectFolder)
		size, err := p.S3Persistor.GetObjectSize(
			p.settings.DataEncryptionKeyBucketName,
			dekPath,
			Opts{SSEC: ssecOptions},
		)
		if err == nil {
			return size, nil
		}
		if isForbiddenError(err) {
			continue
		}
		return 0, err
	}
	return 0, NewNoKEKMatchedError("no kek matched", nil)
}

// ForProject mirrors forProject.
func (p *PerProjectEncryptedS3Persistor) ForProject(bucketName, pathName string) (*CachedPerProjectEncryptedS3Persistor, error) {
	ssec, err := p.getDataEncryptionKeyOptions(bucketName, pathName)
	if err != nil {
		return nil, err
	}
	return NewCachedPerProjectEncryptedS3Persistor(p, ssec), nil
}

// ForProjectRO mirrors forProjectRO.
func (p *PerProjectEncryptedS3Persistor) ForProjectRO(bucketName, pathName string) (*CachedPerProjectEncryptedS3Persistor, error) {
	ssec, err := p.getExistingDataEncryptionKeyOptions(bucketName, pathName)
	if err != nil {
		return nil, err
	}
	return NewCachedPerProjectEncryptedS3Persistor(p, ssec), nil
}

// GenerateDataEncryptionKey mirrors generateDataEncryptionKey.
func (p *PerProjectEncryptedS3Persistor) GenerateDataEncryptionKey(bucketName, pathName string) (*CachedPerProjectEncryptedS3Persistor, error) {
	ssec, err := p.generateDataEncryptionKeyOptions(bucketName, pathName)
	if err != nil {
		return nil, err
	}
	return NewCachedPerProjectEncryptedS3Persistor(p, ssec), nil
}

// generateDataEncryptionKeyOptions mirrors #generateDataEncryptionKeyOptions
// (256-bit random DEK, written ifNoneMatch '*' under the current KEK).
func (p *PerProjectEncryptedS3Persistor) generateDataEncryptionKeyOptions(bucketName, pathName string) (*SSECOptions, error) {
	dataEncryptionKey := make([]byte, AES256KeyLength)
	if _, err := rand.Read(dataEncryptionKey); err != nil {
		return nil, err
	}
	projectFolder, dekPath := p.buildProjectPaths(bucketName, pathName)
	ssecOptions, err := p.currentKeyEncryptionKey(projectFolder)
	if err != nil {
		return nil, err
	}
	if err := p.S3Persistor.SendStream(
		p.settings.DataEncryptionKeyBucketName,
		dekPath,
		bytes.NewReader(dataEncryptionKey),
		Opts{
			IfNoneMatch:   "*",
			SSEC:          ssecOptions,
			ContentLength: 32,
		},
	); err != nil {
		return nil, err
	}
	return NewSSECOptions(dataEncryptionKey), nil
}

// getExistingDataEncryptionKeyOptions mirrors #getExistingDataEncryptionKeyOptions
// (try each KEK; on hit, optionally re-encrypt with the current KEK).
func (p *PerProjectEncryptedS3Persistor) getExistingDataEncryptionKeyOptions(bucketName, pathName string) (*SSECOptions, error) {
	projectFolder, dekPath := p.buildProjectPaths(bucketName, pathName)
	keys, err := p.availableKeyEncryptionKeys()
	if err != nil {
		return nil, err
	}
	var res io.ReadCloser
	kekIndex := 0
	for _, rootKEK := range keys {
		ssecOptions := rootKEK.ForProject(projectFolder)
		stream, err := p.S3Persistor.GetObjectStream(
			p.settings.DataEncryptionKeyBucketName,
			dekPath,
			Opts{SSEC: ssecOptions},
		)
		if err == nil {
			res = stream
			break
		}
		if isForbiddenError(err) {
			kekIndex++
			continue
		}
		return nil, err
	}
	if res == nil {
		return nil, NewNoKEKMatchedError("no kek matched", nil)
	}
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, res); err != nil {
		res.Close()
		return nil, err
	}
	res.Close()

	if kekIndex != 0 && p.settings.AutomaticallyRotateDEKEncryption {
		ssecOptions, err := p.currentKeyEncryptionKey(projectFolder)
		if err != nil {
			return nil, err
		}
		sendErr := p.S3Persistor.SendStream(
			p.settings.DataEncryptionKeyBucketName,
			dekPath,
			bytes.NewReader(buf.Bytes()),
			Opts{SSEC: ssecOptions},
		)
		if sendErr != nil {
			if p.settings.IgnoreErrorsFromDEKReEncryption {
				Logger.Warn(map[string]any{"err": sendErr, "dekPath": dekPath}, "failed to persist re-encrypted DEK")
			} else {
				return nil, sendErr
			}
		}
	}

	return NewSSECOptions(buf.Bytes()), nil
}

// getDataEncryptionKeyOptions mirrors #getDataEncryptionKeyOptions
// (existing → generate on NotFound → existing again on AlreadyWritten).
func (p *PerProjectEncryptedS3Persistor) getDataEncryptionKeyOptions(bucketName, pathName string) (*SSECOptions, error) {
	ssec, err := p.getExistingDataEncryptionKeyOptions(bucketName, pathName)
	if err == nil {
		return ssec, nil
	}
	if !isNotFoundError(err) {
		return nil, err
	}
	gen, err2 := p.generateDataEncryptionKeyOptions(bucketName, pathName)
	if err2 == nil {
		return gen, nil
	}
	if isAlreadyWrittenError(err2) {
		// concurrent initial write — the object now exists
		return p.getExistingDataEncryptionKeyOptions(bucketName, pathName)
	}
	return nil, err2
}

// isForbiddenError mirrors module isForbiddenError
// (ReadError|NotFoundError whose cause is 403/AccessDenied).
func isForbiddenError(err error) bool {
	if err == nil {
		return false
	}
	_, isRead := asPersistorError(err).(*ReadError)
	_, isNotFound := asPersistorError(err).(*NotFoundError)
	if !isRead && !isNotFound {
		return false
	}
	cause := causeOf(err)
	if cause == nil {
		return false
	}
	if se, ok := cause.(interface{ StatusCode() int }); ok && se.StatusCode() == 403 {
		return true
	}
	if se, ok := cause.(interface{ Code() string }); ok && se.Code() == "AccessDenied" {
		return true
	}
	return false
}

func causeOf(err error) any {
	if oe, ok := err.(interface{ OErrorCause() any }); ok {
		return oe.OErrorCause()
	}
	switch t := err.(type) {
	case *ReadError:
		return oerrorCause(t.OError)
	case *NotFoundError:
		return oerrorCause(t.OError)
	case *WriteError:
		return oerrorCause(t.OError)
	}
	return nil
}

func oerrorCause(e interface{ Unwrap() error }) any {
	if e == nil {
		return nil
	}
	c := e.Unwrap()
	if c == nil {
		return nil
	}
	return c
}

// ---------------------------------------------------------------------------
// overrides (Node's subclass method overrides)

// SendStream — resolve the (existing-or-generated) DEK when absent.
func (p *PerProjectEncryptedS3Persistor) SendStream(bucketName, pathName string, source io.Reader, opts Opts) error {
	ssecOptions := opts.SSEC
	if ssecOptions == nil {
		s, err := p.getDataEncryptionKeyOptions(bucketName, pathName)
		if err != nil {
			return err
		}
		ssecOptions = s
	}
	return p.S3Persistor.SendStream(bucketName, pathName, source, withSSEC(opts, ssecOptions))
}

// GetObjectStream — existing DEK only.
func (p *PerProjectEncryptedS3Persistor) GetObjectStream(bucketName, pathName string, opts Opts) (io.ReadCloser, error) {
	ssecOptions := opts.SSEC
	if ssecOptions == nil {
		s, err := p.getExistingDataEncryptionKeyOptions(bucketName, pathName)
		if err != nil {
			return nil, err
		}
		ssecOptions = s
	}
	return p.S3Persistor.GetObjectStream(bucketName, pathName, withSSEC(opts, ssecOptions))
}

// GetObjectSize — existing DEK only.
func (p *PerProjectEncryptedS3Persistor) GetObjectSize(bucketName, pathName string, opts Opts) (int64, error) {
	ssecOptions := opts.SSEC
	if ssecOptions == nil {
		s, err := p.getExistingDataEncryptionKeyOptions(bucketName, pathName)
		if err != nil {
			return 0, err
		}
		ssecOptions = s
	}
	return p.S3Persistor.GetObjectSize(bucketName, pathName, withSSEC(opts, ssecOptions))
}

// GetObjectStorageClass — existing DEK only.
func (p *PerProjectEncryptedS3Persistor) GetObjectStorageClass(bucketName, pathName string, opts Opts) (string, error) {
	ssecOptions := opts.SSEC
	if ssecOptions == nil {
		s, err := p.getExistingDataEncryptionKeyOptions(bucketName, pathName)
		if err != nil {
			return "", err
		}
		ssecOptions = s
	}
	return p.S3Persistor.GetObjectStorageClass(bucketName, pathName, withSSEC(opts, ssecOptions))
}

// DirectorySize — listing needs no SSE-C credentials (super only).
func (p *PerProjectEncryptedS3Persistor) DirectorySize(bucketName, pathName, token string) (int64, error) {
	return p.S3Persistor.DirectorySize(bucketName, pathName, token)
}

// DeleteDirectory — validate via PathToProjectFolder, delete the prefix, and
// drop the DEK object when the project folder matches the deleted path.
func (p *PerProjectEncryptedS3Persistor) DeleteDirectory(bucketName, pathName, token string) error {
	projectFolder, dekPath := p.buildProjectPaths(bucketName, pathName)
	if err := p.S3Persistor.DeleteDirectory(bucketName, pathName, token); err != nil {
		return err
	}
	if projectFolder == pathName {
		return p.S3Persistor.DeleteObject(p.settings.DataEncryptionKeyBucketName, dekPath)
	}
	return nil
}

// GetObjectMd5Hash — the SSE-C ETag is not the content MD5: force the
// download-and-hash path (etagIsNotMD5).
func (p *PerProjectEncryptedS3Persistor) GetObjectMd5Hash(bucketName, pathName string, opts Opts) (string, error) {
	opts.EtagIsNotMD5 = true
	return p.S3Persistor.GetObjectMd5Hash(bucketName, pathName, opts)
}

// CopyObject — DEK for the destination (generate) + the source's existing DEK.
func (p *PerProjectEncryptedS3Persistor) CopyObject(bucketName, sourcePath, destinationPath string, opts Opts) error {
	ssecOptions := opts.SSEC
	if ssecOptions == nil {
		s, err := p.getDataEncryptionKeyOptions(bucketName, destinationPath)
		if err != nil {
			return err
		}
		ssecOptions = s
	}
	ssecSrcOptions := opts.SSECSrc
	if ssecSrcOptions == nil {
		s, err := p.getExistingDataEncryptionKeyOptions(bucketName, sourcePath)
		if err != nil {
			return err
		}
		ssecSrcOptions = s
	}
	return p.S3Persistor.CopyObject(bucketName, sourcePath, destinationPath, withSSCSrc(withSSEC(opts, ssecOptions), ssecSrcOptions))
}

// GetRedirectURL — not supported with SSE-C.
func (p *PerProjectEncryptedS3Persistor) GetRedirectURL(bucketName, pathName string) (string, error) {
	return "", NewNotImplementedError("signed links are not supported with SSE-C", nil)
}

// SendFile — not overridden by Node; the base S3 SendFile (with DEK
// resolution) applies.
func withSSEC(opts Opts, ssec *SSECOptions) Opts {
	opts.SSEC = ssec
	return opts
}

func withSSCSrc(opts Opts, ssecSrc *SSECOptions) Opts {
	opts.SSECSrc = ssecSrc
	return opts
}

// ---------------------------------------------------------------------------
// CachedPerProjectEncryptedS3Persistor — avoids repeated DEK fetches for a
// project (Node: a helper class, not a Persistor subclass in the same sense;
// it pins the project's SSE-C options into every call).

type CachedPerProjectEncryptedS3Persistor struct {
	parent            *PerProjectEncryptedS3Persistor
	projectKeyOptions *SSECOptions
}

func NewCachedPerProjectEncryptedS3Persistor(parent *PerProjectEncryptedS3Persistor, options *SSECOptions) *CachedPerProjectEncryptedS3Persistor {
	return &CachedPerProjectEncryptedS3Persistor{parent: parent, projectKeyOptions: options}
}

// SendFile mirrors the cached class's sendFile (read the local file, send
// it with the project's SSE-C options).
func (c *CachedPerProjectEncryptedS3Persistor) SendFile(bucketName, pathName, fsPath string) error {
	f, err := os.Open(fsPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return c.SendStream(bucketName, pathName, f, Opts{})
}

// GetObjectSize → parent (no cached option used by Node for size).
func (c *CachedPerProjectEncryptedS3Persistor) GetObjectSize(bucketName, pathName string) (int64, error) {
	return c.parent.GetObjectSize(bucketName, pathName, Opts{})
}

// ListDirectoryKeys → parent.
func (c *CachedPerProjectEncryptedS3Persistor) ListDirectoryKeys(bucketName, pathName string) ([]string, error) {
	return c.parent.ListDirectoryKeys(bucketName, pathName)
}

// ListDirectoryStats → parent.
func (c *CachedPerProjectEncryptedS3Persistor) ListDirectoryStats(bucketName, pathName string) ([]DirStat, error) {
	return c.parent.ListDirectoryStats(bucketName, pathName)
}

// SendStream — always carries the project's SSE-C options.
func (c *CachedPerProjectEncryptedS3Persistor) SendStream(bucketName, pathName string, source io.Reader, opts Opts) error {
	return c.parent.SendStream(bucketName, pathName, source, withSSEC(opts, c.projectKeyOptions))
}

// GetObjectStream — always carries the project's SSE-C options.
func (c *CachedPerProjectEncryptedS3Persistor) GetObjectStream(bucketName, pathName string, opts Opts) (io.ReadCloser, error) {
	return c.parent.GetObjectStream(bucketName, pathName, withSSEC(opts, c.projectKeyOptions))
}

var _ = errors.New
var _ = context.Background
