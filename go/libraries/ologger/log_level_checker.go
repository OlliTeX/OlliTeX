package ologger

import (
	"time"
)

// TRACING_END_TIME_FILE is Node's hard-coded path the FileLogLevelChecker
// reads (Node: `/logging/tracingEndTime`).
const TRACING_END_TIME_FILE = "/logging/tracingEndTime"

// logLevelChecker is the shared LogLevelChecker base (Node: the base class with
// start/stop/checkLogLevel + a getTracingEndTime hook).
type logLevelChecker struct {
	logger       Logger
	defaultLevel string
	now          func() int64
	interval     time.Duration
	// getEndTime is the subclass hook (Node: getTracingEndTime on each
	// subclass). Set by the constructors.
	getEndTime func() (int64, error)

	cancel chan struct{}
}

// Start mirrors start(): check now, then re-check every minute (the interval is
// injectable; the goroutine is detached like Node's `unref()`).
func (c *logLevelChecker) Start() {
	c.CheckLogLevel()
	cancel := make(chan struct{})
	c.cancel = cancel
	go func() {
		t := time.NewTicker(c.interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				c.CheckLogLevel()
			case <-cancel:
				return
			}
		}
	}()
}

// Stop mirrors stop(): clearInterval (no-op if never started).
func (c *logLevelChecker) Stop() {
	if c.cancel != nil {
		close(c.cancel)
		c.cancel = nil
	}
}

// CheckLogLevel mirrors checkLogLevel(): trace while the end time is in the
// future, otherwise the default level; any error falls back to the default.
// Returns the underlying read error (Node swallows it, but the observable
// level is identical).
func (c *logLevelChecker) CheckLogLevel() error {
	end, err := c.getEndTime()
	if err != nil {
		c.logger.Level(c.defaultLevel)
		return err
	}
	if end > c.now() {
		c.logger.Level("trace")
	} else {
		c.logger.Level(c.defaultLevel)
	}
	return nil
}

// fileLogLevelChecker is FileLogLevelChecker (reads the end-time file).
type fileLogLevelChecker struct {
	logLevelChecker
}

// NewFileLogLevelChecker builds a FileLogLevelChecker.
func NewFileLogLevelChecker(logger Logger, defaultLevel string, now func() int64, readFile func(path string) (string, error), interval time.Duration) Checker {
	c := &fileLogLevelChecker{logLevelChecker: logLevelChecker{logger: logger, defaultLevel: defaultLevel, now: now, interval: interval}}
	c.getEndTime = func() (int64, error) {
		str, err := readFile(TRACING_END_TIME_FILE)
		if err != nil {
			return 0, err
		}
		return parseInt10(str), nil
	}
	return c
}

// gceLevelChecker is GCEMetadataLogLevelChecker (reads GCE project metadata).
type gceLevelChecker struct {
	logLevelChecker
}

// metadataURI mirrors the GCE URI the Node checker fetches:
// `http://metadata.google.internal/computeMetadata/v1/project/attributes/<name>-setLogLevelEndTime`.
func metadataURI(logger Logger) string {
	return "http://metadata.google.internal/computeMetadata/v1/project/attributes/" + logger.Name() + "-setLogLevelEndTime"
}

// NewGCEMetadataLogLevelChecker builds a GCEMetadataLogLevelChecker.
func NewGCEMetadataLogLevelChecker(logger Logger, defaultLevel string, now func() int64, fetchString func(uri string, headers map[string]string) (string, error), interval time.Duration) Checker {
	c := &gceLevelChecker{logLevelChecker: logLevelChecker{logger: logger, defaultLevel: defaultLevel, now: now, interval: interval}}
	c.getEndTime = func() (int64, error) {
		str, err := fetchString(metadataURI(logger), map[string]string{"Metadata-Flavor": "Google"})
		if err != nil {
			return 0, err
		}
		return parseInt10(str), nil
	}
	return c
}

// parseInt10 mirrors `parseInt(str, 10)`: leading signed integer, 0 when there
// is no digit (Node's NaN behaves identically in the `end > now` comparison).
func parseInt10(s string) int64 {
	i := 0
	if i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		if s[i] == '-' {
			neg = true
		}
		i++
	}
	if i >= len(s) || s[i] < '0' || s[i] > '9' {
		return 0
	}
	n := int64(0)
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int64(s[i]-'0')
		i++
	}
	if neg {
		n = -n
	}
	return n
}
