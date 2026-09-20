// Package util ports util/Util.java and util/Project.java.
package util

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Service-wide config (ported from static Util fields).
// ---------------------------------------------------------------------------

var (
	serviceName string
	postbackURL string
	port        int
)

func SetServiceName(name string) { serviceName = name }
func GetServiceName() string     { return serviceName }
func SetPostbackURL(url string)  { postbackURL = url }
func GetPostbackURL() string     { return postbackURL }
func SetPort(p int)              { port = p }
func GetPort() int               { return port }

// ---------------------------------------------------------------------------
// Project (ported from util/Project.java).
// ---------------------------------------------------------------------------

func IsValidProjectName(projectName string) bool {
	return projectName != "" && !strings.HasPrefix(projectName, ".")
}

func CheckValidProjectName(projectName string) error {
	if !IsValidProjectName(projectName) {
		return fmt.Errorf("[%s] invalid project name", projectName)
	}
	return nil
}

// ---------------------------------------------------------------------------
// String helpers (ported from util/Util.java).
// ---------------------------------------------------------------------------

// RemoveAllSuffixes ports Util.removeAllSuffixes.
// RemoveAllSuffixes("something.git///", "/", ".git") => "something"
func RemoveAllSuffixes(str string, suffixes ...string) string {
	for _, suffix := range suffixes {
		for {
			idx := strings.LastIndex(str, suffix)
			if idx < 0 {
				break
			}
			str = str[:idx]
		}
	}
	return str
}

// SplitURIPath ports Java requestUri.split("/") — the Java single-arg
// String.split(regex) drops trailing empty strings. Concretely for Oauth2Filter:
//
//	"/"           -> [""]      -> [1] throws AIOOBE -> ProductionErrorHandler 500
//	"/project"    -> ["", "project"]
//	"/project/"   -> ["", "project"]
//	""            -> [""]
//
// Go's strings.Split does not drop trailing empties, so this mirrors Java so
// the caller's segs[1] index and len guard match the filter's AIOOBE->500
// path exactly.
func SplitURIPath(uri string) []string {
	if uri == "" {
		return []string{""}
	}
	segs := strings.Split(uri, "/")
	for len(segs) > 0 && segs[len(segs)-1] == "" {
		segs = segs[:len(segs)-1]
	}
	if len(segs) == 0 {
		return []string{""}
	}
	return segs
}

// DeleteInDirectoryApartFrom ports Util.deleteInDirectoryApartFrom.
func DeleteInDirectoryApartFrom(directory string, apartFrom ...string) {
	excluded := map[string]bool{}
	for _, name := range apartFrom {
		excluded[name] = true
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, e := range entries {
		if excluded[e.Name()] {
			continue
		}
		full := filepath.Join(directory, e.Name())
		if e.IsDir() {
			DeleteDirectory(full)
			continue
		}
		if err := os.Remove(full); err != nil {
			slog.Debug("deleted file", "path", full, "err", err)
		}
	}
}

// DeleteDirectory ports Util.deleteDirectory.
func DeleteDirectory(directory string) error {
	if directory == "" {
		return nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		full := filepath.Join(directory, e.Name())
		if e.IsDir() {
			if err := DeleteDirectory(full); err != nil {
				return err
			}
		} else {
			if err := os.Remove(full); err != nil {
				return err
			}
		}
	}
	return os.Remove(directory)
}

// GetCodeFromResponse ports Util.getCodeFromResponse.
// "code" field, or for legacy no-code responses maps status to an error.
func GetCodeFromResponse(body map[string]interface{}) (string, error) {
	code, ok := body["code"].(string)
	if !ok {
		msg := "Unexpected error"
		if status, ok := body["status"].(string); ok {
			switch status {
			case "422":
				msg = "Unprocessable entity"
			case "404":
				msg = "Not found"
			case "403":
				msg = "Forbidden"
			}
		}
		return "", fmt.Errorf("%s", msg)
	}
	return code, nil
}

// Entries plural helper (Util.entries).
func Entries(entries int) string {
	if entries == 1 {
		return "entry"
	}
	return "entries"
}

var (
	linkSharingIDRe = regexp.MustCompile(`^[0-9]+[bcdfghjklmnpqrstvwxyz]{6,12}$`)
	projectIDRe     = regexp.MustCompile(`^[0-9a-f]{24}$`)
)

func IsLinkSharingID(id string) bool { return linkSharingIDRe.MatchString(id) }
func IsProjectID(id string) bool     { return projectIDRe.MatchString(id) }
