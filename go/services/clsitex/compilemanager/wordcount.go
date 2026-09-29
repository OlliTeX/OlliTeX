package compilemanager

import (
	"context"
	"strconv"
	"strings"

	cerrors "ollitex/go/services/clsitex/errors"
	"ollitex/go/services/clsitex/logger"
	"ollitex/go/services/clsitex/metrics"
	"ollitex/go/services/clsitex/resourcewriter"
)

// WordcountResults mirrors the _parseWordcountFromOutput object the
// controller embeds under `texcount` (Node: res.json({texcount: result})).
type WordcountResults struct {
	Encode      string `json:"encode"`
	TextWords   int    `json:"textWords"`
	HeadWords   int    `json:"headWords"`
	Outside     int    `json:"outside"`
	Headers     int    `json:"headers"`
	Elements    int    `json:"elements"`
	MathInline  int    `json:"mathInline"`
	MathDisplay int    `json:"mathDisplay"`
	Errors      int    `json:"errors"`
	Messages    string `json:"messages"`
}

// parseWordcountFromOutput ports _parseWordcountFromOutput. Mirrors Node's
// `line.split(':')` first-two-segments destructuring (a "path: a: b" line
// yields info "a") and per-line independent checks (no else-chain).
func parseWordcountFromOutput(output string) WordcountResults {
	var results WordcountResults
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, ":", 2)
		data := parts[0]
		info := ""
		if len(parts) > 1 {
			info = parts[1]
		}
		if strings.Contains(data, "Encoding") {
			results.Encode = strings.TrimSpace(info)
		}
		if strings.Contains(data, "in text") {
			results.TextWords = wcInt(info)
		}
		if strings.Contains(data, "in head") {
			results.HeadWords = wcInt(info)
		}
		if strings.Contains(data, "outside") {
			results.Outside = wcInt(info)
		}
		if strings.Contains(data, "of head") {
			results.Headers = wcInt(info)
		}
		if strings.Contains(data, "Number of floats/tables/figures") {
			results.Elements = wcInt(info)
		}
		if strings.Contains(data, "Number of math inlines") {
			results.MathInline = wcInt(info)
		}
		if strings.Contains(data, "Number of math displayed") {
			results.MathDisplay = wcInt(info)
		}
		if data == "(errors" {
			// errors reported as (errors:123)
			results.Errors = wcInt(info)
		}
		if strings.Contains(line, "!!! ") {
			// errors logged as !!! message !!!
			results.Messages += line + "\n"
		}
	}
	return results
}

// wcInt mirrors Node's parseInt(info, 10): leading optional sign + leading
// digit run (trailing garbage ignored); Go returns 0 where Node yields NaN.
func wcInt(info string) int {
	s := strings.TrimSpace(info)
	i := 0
	for i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	j := i
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i {
		return 0
	}
	n, _ := strconv.Atoi(s[i:j])
	return n
}

// syncResourcesForWordcount ports _syncResourcesForWordcount: always write
// (texcount reads every included file, so they all have to be current)
// under the compile lock; MissingUpdates propagates raw, the rest tagged.
// Node Metrics.inc('wordcount_sync_resources') maps to the metrics generic
// bridge (plain count counter, no dedicated handle).
func (m *Manager) syncResourcesForWordcount(projectID, userID, filename, compileDir string,
	req *Request) error {
	lock, err := m.Acquire(compileDir)
	if err != nil {
		return err
	}
	defer lock.Release()

	metrics.GenericInc("wordcount_sync_resources", 1)

	var syncErr error
	if req.IsCompileFromHistory {
		_, syncErr = m.HistorySync(
			context.Background(), projectID, userID, req.HRW, compileDir,
			map[string]any{}, map[string]any{})
	} else {
		rw := &resourcewriter.Request{
			ProjectID:   projectID,
			UserID:      userID,
			SyncType:    req.SyncType,
			SyncState:   req.SyncState,
			Resources:   req.RWResources,
			MetricsPath: req.MetricsOpts.Path,
		}
		_, syncErr = m.ResourceSync(rw, compileDir)
	}
	if syncErr != nil {
		if cerrors.IsMissingUpdates(syncErr) {
			return syncErr
		}
		return cerrors.Tag(syncErr, "error syncing resources for wordcount", map[string]any{
			"projectId": projectID,
			"userId":    userID,
			"filename":  filename,
		})
	}
	return nil
}

// Wordcount ports wordcount: run texcount -nocol -inc against the LITERAL
// `$COMPILE_DIR/<filename>` argument (DockerRunner substitutes it inside
// the sandbox), with the 60s timeout and 'wordcount' compile group. Node
// passes the query `image` verbatim (undefined => no image); this port
// passes "" for that case. req==nil skips the sync (controller wordcount
// vs wordcountWithSync).
func (m *Manager) Wordcount(projectID, userID, filename, image string,
	req *Request) (WordcountResults, error) {
	logger.Debug(map[string]any{
		"projectId": projectID, "userId": userID, "filename": filename, "image": image,
	}, "running wordcount")

	filePath := "$COMPILE_DIR/" + filename
	command := []string{"texcount", "-nocol", "-inc", filePath}
	compileDir := compileDirOf(m.Paths.CompilesDir, projectID, userID)
	const timeout = int64(60 * 1000)
	compileName := compileName(projectID, userID)

	if image != "" && !m.isImageNameAllowed(image) {
		return WordcountResults{}, &cerrors.InvalidParameter{Message: "invalid image"}
	}

	created, err := m.MkdirAll(compileDir)
	if err != nil {
		return WordcountResults{}, cerrors.Tag(err, "error ensuring dir for wordcount",
			map[string]any{"projectId": projectID, "userId": userID, "filename": filename})
	}

	if created && req != nil && req.CompileFromClsiCache {
		// Bootstrap the compile dir on this CLSI: restore the cached
		// outputs too, so the next compile does not start from scratch.
		if _, derr := m.DownloadLatestCompileCache(projectID, userID, compileDir); derr != nil {
			logger.Warn(map[string]any{"err": derr.Error(), "projectId": projectID,
				"userId": userID}, "failed to populate compile dir from cache")
		}
	}

	if req != nil {
		if serr := m.syncResourcesForWordcount(projectID, userID, filename, compileDir, req); serr != nil {
			return WordcountResults{}, serr
		}
	}

	out, runErr := runOut(m.Runner, compileName, command, compileDir, image,
		timeout, nil, "wordcount")
	if runErr != nil {
		return WordcountResults{}, cerrors.Tag(runErr, "error reading word count output",
			map[string]any{"command": command, "compileDir": compileDir,
				"projectId": projectID, "userId": userID})
	}
	results := parseWordcountFromOutput(out.Stdout)
	logger.Debug(map[string]any{"projectId": projectID, "userId": userID,
		"wordcount": results}, "word count results")
	return results, nil
}
