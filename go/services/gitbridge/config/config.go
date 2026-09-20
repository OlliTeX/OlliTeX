// Package config ports application/config/Config.java and the nested
// SwapStoreConfig / SwapJobConfig / RepoStoreConfig classes.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// RepoStore ports bridge/repo/RepoStoreConfig.java.
type RepoStore struct {
	MaxFileSize *int64 `json:"maxFileSize"`
	MaxFileNum  *int64 `json:"maxFileNum"`
}

// SwapStoreStore ports bridge/swap/store/SwapStoreConfig.java.
type SwapStoreStore struct {
	Type         *string `json:"type"`
	AwsAccessKey *string `json:"awsAccessKey"`
	AwsSecret    *string `json:"awsSecret"`
	S3BucketName *string `json:"s3BucketName"`
	AwsRegion    *string `json:"awsRegion"`
	AwsEndpoint  *string `json:"awsEndpoint"`
}

// SwapJob ports bridge/swap/job/SwapJobConfig.java.
type SwapJob struct {
	MinProjects       int    `json:"minProjects"`
	LowGiB            int    `json:"lowGiB"`
	HighGiB           int    `json:"highGiB"`
	IntervalMillis    int64  `json:"intervalMillis"`
	CompressionMethod string `json:"compressionMethod"`
	AllowUnsafeStores bool   `json:"allowUnsafeStores"`
}

// Config ports application/config/Config.java.
type Config struct {
	Port                 int             `json:"port"`
	BindIp               string          `json:"bindIp"`
	IdleTimeout          int             `json:"idleTimeout"`
	RootGitDirectory     string          `json:"rootGitDirectory"`
	AllowedCorsOrigins   string          `json:"allowedCorsOrigins"` // raw; split at parse
	APIBaseURL           string          `json:"apiBaseUrl"`
	PostbackURL          string          `json:"postbackBaseUrl"`
	ServiceName          string          `json:"serviceName"`
	Oauth2Server         string          `json:"oauth2Server"` // parsed as "true" only flag
	UserPasswordEnabled  string          `json:"userPasswordEnabled"`
	RepoStore            *RepoStore      `json:"repoStore"`
	SwapStore            *SwapStoreStore `json:"swapStore"`
	SwapJob              *SwapJob        `json:"swapJob"`
	SQLiteHeapLimitBytes int             `json:"sqliteHeapLimitBytes"`
}

// Load reads and parses a config file (ConfigFileException semantics).
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s struct {
		APIBaseURL           string          `json:"apiBaseUrl"`
		PostbackURL          string          `json:"postbackBaseUrl"`
		AllowedCorsOrigins   string          `json:"allowedCorsOrigins"`
		Oauth2Server         string          `json:"oauth2Server"`
		UserPasswordEnabled  string          `json:"userPasswordEnabled"`
		RepoStore            json.RawMessage `json:"repoStore"`
		SwapStore            json.RawMessage `json:"swapStore"`
		SwapJob              json.RawMessage `json:"swapJob"`
		SQLiteHeapLimitBytes *int            `json:"sqliteHeapLimitBytes"`
		Port                 int             `json:"port"`
		BindIp               string          `json:"bindIp"`
		IdleTimeout          int             `json:"idleTimeout"`
		RootGitDirectory     string          `json:"rootGitDirectory"`
		ServiceName          string          `json:"serviceName"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("invalid config file: %w", err)
	}
	config := &Config{}
	config.Port = s.Port
	config.BindIp = s.BindIp
	if s.BindIp == "" {
		return nil, fmt.Errorf("the property for bindIp is invalid. Check your config file.")
	}
	config.IdleTimeout = s.IdleTimeout
	config.RootGitDirectory = s.RootGitDirectory
	if s.RootGitDirectory == "" {
		return nil, fmt.Errorf("the property for rootGitDirectory is invalid. Check your config file.")
	}
	config.APIBaseURL = s.APIBaseURL
	if s.APIBaseURL == "" {
		return nil, fmt.Errorf("the property for apiBaseUrl is invalid. Check your config file.")
	}
	// Java: ensure trailing "/".
	if !strings.HasSuffix(config.APIBaseURL, "/") {
		config.APIBaseURL += "/"
	}
	config.ServiceName = s.ServiceName
	if s.ServiceName == "" {
		return nil, fmt.Errorf("the property for serviceName is invalid. Check your config file.")
	}
	config.AllowedCorsOrigins = s.AllowedCorsOrigins
	config.PostbackURL = s.PostbackURL
	if s.PostbackURL == "" {
		return nil, fmt.Errorf("the property for postbackBaseUrl is invalid. Check your config file.")
	}
	if !strings.HasSuffix(config.PostbackURL, "/") {
		config.PostbackURL += "/"
	}
	config.Oauth2Server = s.Oauth2Server
	config.UserPasswordEnabled = s.UserPasswordEnabled
	if s.RepoStore != nil {
		var rs RepoStore
		if err := json.Unmarshal(s.RepoStore, &rs); err != nil {
			return nil, fmt.Errorf("the property for repoStore is invalid. Check your config file.")
		}
		config.RepoStore = &rs
	}
	if s.SwapStore != nil {
		var ss SwapStoreStore
		if err := json.Unmarshal(s.SwapStore, &ss); err != nil {
			return nil, fmt.Errorf("the property for swapStore is invalid. Check your config file.")
		}
		config.SwapStore = &ss
	}
	if s.SwapJob != nil {
		var sj SwapJob
		if err := json.Unmarshal(s.SwapJob, &sj); err != nil {
			return nil, fmt.Errorf("the property for swapJob is invalid. Check your config file.")
		}
		config.SwapJob = &sj
	}
	if s.SQLiteHeapLimitBytes != nil {
		config.SQLiteHeapLimitBytes = *s.SQLiteHeapLimitBytes
	}
	return config, nil
}

func (c *Config) GetServiceName() string       { return c.ServiceName }
func (c *Config) GetPort() int                 { return c.Port }
func (c *Config) GetBindIp() string            { return c.BindIp }
func (c *Config) GetIdleTimeout() int          { return c.IdleTimeout }
func (c *Config) GetRootGitDirectory() string  { return c.RootGitDirectory }
func (c *Config) GetAPIBaseURL() string        { return c.APIBaseURL }
func (c *Config) GetPostbackURL() string       { return c.PostbackURL }
func (c *Config) GetSqliteHeapLimitBytes() int { return c.SQLiteHeapLimitBytes }

// OAuth2Server returns the oauth2 server URL, or nil if absent
// (Java: @Nullable String getOauth2Server()).
func (c *Config) OAuth2Server() *string {
	if c.Oauth2Server == "" {
		return nil
	}
	return &c.Oauth2Server
}

func (c *Config) IsUserPasswordEnabled() bool { return c.UserPasswordEnabled == "true" }

// GetAllowedCorsOrigins ports Config.getAllowedCorsOrigins (comma split).
func (c *Config) GetAllowedCorsOrigins() []string {
	trimmed := strings.TrimSpace(c.AllowedCorsOrigins)
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, ",")
}

// sanitisable is a log-safe copy of Config mirroring the Java class shape.
type sanitisable struct {
	Port                int               `json:"port"`
	BindIp              string            `json:"bindIp"`
	IdleTimeout         int               `json:"idleTimeout"`
	RootGitDirectory    string            `json:"rootGitDirectory"`
	AllowedCorsOrigins  string            `json:"allowedCorsOrigins"`
	APIBaseURL          string            `json:"apiBaseUrl"`
	PostbackURL         string            `json:"postbackBaseUrl"`
	ServiceName         string            `json:"serviceName"`
	Oauth2Server        string            `json:"oauth2Server"`
	UserPasswordEnabled string            `json:"userPasswordEnabled"`
	RepoStore           *RepoStore        `json:"repoStore,omitempty"`
	SwapStore           *sanitisableStore `json:"swapStore,omitempty"`
	SwapJob             *SwapJob          `json:"swapJob,omitempty"`
}

type sanitisableStore struct {
	Type         *string `json:"type,omitempty"`
	AwsAccessKey *string `json:"awsAccessKey,omitempty"`
	AwsSecret    *string `json:"awsSecret,omitempty"`
	S3BucketName *string `json:"s3BucketName,omitempty"`
	AwsRegion    *string `json:"awsRegion,omitempty"`
	AwsEndpoint  *string `json:"awsEndpoint,omitempty"`
}

// ToSanitisedString mirrors Config.getSanitisedString (log-safe copy: AWS
// keys replaced by placeholders).
func (c *Config) ToSanitisedString() string {
	r := sanitisable{
		Port:                c.Port,
		BindIp:              c.BindIp,
		IdleTimeout:         c.IdleTimeout,
		RootGitDirectory:    c.RootGitDirectory,
		AllowedCorsOrigins:  c.AllowedCorsOrigins,
		APIBaseURL:          c.APIBaseURL,
		PostbackURL:         c.PostbackURL,
		ServiceName:         c.ServiceName,
		Oauth2Server:        c.Oauth2Server,
		UserPasswordEnabled: c.UserPasswordEnabled,
		RepoStore:           c.RepoStore,
		SwapJob:             c.SwapJob,
	}
	if c.SwapStore != nil {
		r.SwapStore = &sanitisableStore{
			Type:         c.SwapStore.Type,
			S3BucketName: c.SwapStore.S3BucketName,
			AwsRegion:    c.SwapStore.AwsRegion,
			AwsEndpoint:  c.SwapStore.AwsEndpoint,
		}
		if c.SwapStore.AwsAccessKey != nil {
			ph := "<awsAccessKey>"
			r.SwapStore.AwsAccessKey = &ph
		}
		if c.SwapStore.AwsSecret != nil {
			ph := "<awsSecret>"
			r.SwapStore.AwsSecret = &ph
		}
	}
	out, _ := json.Marshal(r)
	return string(out)
}
