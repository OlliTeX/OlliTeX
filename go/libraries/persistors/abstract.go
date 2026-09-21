package persistors

import (
	"io"
)

// Opts is the union of the loose options objects the Node persistors accept
// on the different methods (Node: plain objects with optional keys):
//
//	sendStream:      sourceMd5, contentType, contentEncoding, contentLength, ifNoneMatch, ssecOptions
//	getObjectStream: start, end, autoGunzip, useSubdirectories (fs), ssecOptions
//	copyObject:      ssecSrcOptions, ssecOptions
//	getObjectSize / getObjectMd5Hash / checkIfObjectExists: ssecOptions, etagIsNotMD5
type Opts struct {
	Start             *int64
	End               *int64
	AutoGunzip        bool
	UseSubdirectories bool
	SourceMD5         string
	ContentType       string
	ContentEncoding   string
	ContentLength     int64
	IfNoneMatch       string
	EtagIsNotMD5      bool
	SSEC              *SSECOptions
	SSECSrc           *SSECOptions
}

// DirStat is one entry of listDirectoryStats (`{key, size}`).
type DirStat struct {
	Key  string
	Size int64
}

// Persistor is the AbstractPersistor contract. Node's AbstractPersistor
// methods all throw NotImplementedError with
// {method, location, target/source/name, ...} info — BasePersistor below
// implements exactly that.
type Persistor interface {
	SendFile(location, target, source string) error
	SendStream(location, target string, source io.Reader, opts Opts) error
	GetObjectStream(location, name string, opts Opts) (io.ReadCloser, error)
	GetRedirectURL(location, name string) (string, error)
	GetObjectSize(location, name string, opts Opts) (int64, error)
	GetObjectMd5Hash(location, name string, opts Opts) (string, error)
	CopyObject(location, fromName, toName string, opts Opts) error
	DeleteObject(location, name string) error
	DeleteDirectory(location, name string, continuationToken string) error
	CheckIfObjectExists(location, name string, opts Opts) (bool, error)
	DirectorySize(location, name string, continuationToken string) (int64, error)
	ListDirectoryKeys(location, prefix string) ([]string, error)
	ListDirectoryStats(location, prefix string) ([]DirStat, error)
}

// BasePersistor is the Go stand-in for `class AbstractPersistor` — the
// default implementations (each throws NotImplementedError with the same
// info the Node methods carry).
type BasePersistor struct{}

func notImpl(method string, info map[string]any) error {
	m := map[string]any{"method": method}
	for k, v := range info {
		m[k] = v
	}
	return NewNotImplementedError("method not implemented in persistor", m)
}

var _ Persistor = (*BasePersistor)(nil)

// SendFile — AbstractPersistor.sendFile default.
func (b *BasePersistor) SendFile(location, target, source string) error {
	return notImpl("sendFile", map[string]any{"location": location, "target": target, "source": source})
}

// SendStream — AbstractPersistor.sendStream default.
func (b *BasePersistor) SendStream(location, target string, source io.Reader, opts Opts) error {
	return notImpl("sendStream", map[string]any{"location": location, "target": target, "opts": opts})
}

// GetObjectStream — AbstractPersistor.getObjectStream default.
func (b *BasePersistor) GetObjectStream(location, name string, opts Opts) (io.ReadCloser, error) {
	return nil, notImpl("getObjectStream", map[string]any{"location": location, "name": name, "opts": opts})
}

// GetRedirectURL — AbstractPersistor.getRedirectUrl default.
func (b *BasePersistor) GetRedirectURL(location, name string) (string, error) {
	return "", notImpl("getRedirectUrl", map[string]any{"location": location, "name": name})
}

// GetObjectSize — AbstractPersistor.getObjectSize default.
func (b *BasePersistor) GetObjectSize(location, name string, opts Opts) (int64, error) {
	return 0, notImpl("getObjectSize", map[string]any{"location": location, "name": name, "opts": opts})
}

// GetObjectMd5Hash — AbstractPersistor.getObjectMd5Hash default.
func (b *BasePersistor) GetObjectMd5Hash(location, name string, opts Opts) (string, error) {
	return "", notImpl("getObjectMd5Hash", map[string]any{"location": location, "name": name, "opts": opts})
}

// CopyObject — AbstractPersistor.copyObject default.
func (b *BasePersistor) CopyObject(location, fromName, toName string, opts Opts) error {
	return notImpl("copyObject", map[string]any{"location": location, "fromName": fromName, "toName": toName, "opts": opts})
}

// DeleteObject — AbstractPersistor.deleteObject default.
func (b *BasePersistor) DeleteObject(location, name string) error {
	return notImpl("deleteObject", map[string]any{"location": location, "name": name})
}

// DeleteDirectory — AbstractPersistor.deleteDirectory default.
func (b *BasePersistor) DeleteDirectory(location, name string, continuationToken string) error {
	return notImpl("deleteDirectory", map[string]any{"location": location, "name": name})
}

// CheckIfObjectExists — AbstractPersistor.checkIfObjectExists default.
func (b *BasePersistor) CheckIfObjectExists(location, name string, opts Opts) (bool, error) {
	return false, notImpl("checkIfObjectExists", map[string]any{"location": location, "name": name, "opts": opts})
}

// DirectorySize — AbstractPersistor.directorySize default.
func (b *BasePersistor) DirectorySize(location, name string, continuationToken string) (int64, error) {
	return 0, notImpl("directorySize", map[string]any{"location": location, "name": name})
}

// ListDirectoryKeys — AbstractPersistor.listDirectoryKeys default.
func (b *BasePersistor) ListDirectoryKeys(location, prefix string) ([]string, error) {
	return nil, notImpl("listDirectoryKeys", map[string]any{"location": location, "prefix": prefix})
}

// ListDirectoryStats — AbstractPersistor.listDirectoryStats default.
func (b *BasePersistor) ListDirectoryStats(location, prefix string) ([]DirStat, error) {
	return nil, notImpl("listDirectoryStats", map[string]any{"location": location, "prefix": prefix})
}
