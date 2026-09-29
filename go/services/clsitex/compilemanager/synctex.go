package compilemanager

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	cerrors "ollitex/go/services/clsitex/errors"
	"ollitex/go/services/clsitex/logger"
	synctexparser "ollitex/go/services/clsitex/synctexparser"
)

// SyncOpts mirrors the Node sync opts {imageName, editorId, buildId,
// compileFromClsiCache}.
type SyncOpts struct {
	ImageName            string
	EditorID             string
	BuildID              string
	CompileFromClsiCache bool
}

// SyncFromCodeResult mirrors the syncFromCode return {codePositions,
// downloadedFromCache}.
type SyncFromCodeResult struct {
	CodePositions       []synctexparser.Record `json:"codePositions"`
	DownloadedFromCache bool                   `json:"downloadedFromCache"`
}

// SyncFromPdfResult mirrors the syncFromPdf return {pdfPositions,
// downloadedFromCache}.
type SyncFromPdfResult struct {
	PdfPositions        []synctexparser.Record `json:"pdfPositions"`
	DownloadedFromCache bool                   `json:"downloadedFromCache"`
}

var (
	// Node: /^[a-f0-9-]+$/ on editorId.
	editorIDRegexp = regexp.MustCompile(`^[a-f0-9-]+$`)
	// OutputCacheManager.BUILD_REGEX /^[0-9a-f]+-[0-9a-f]+$/.
	buildIDRegexp = regexp.MustCompile(`^[0-9a-f]+-[0-9a-f]+$`)
)

// runSynctex ports _runSynctex: validate opts, resolve the directory from
// buildId + canRunSyncTeXInOutputDir, queue the synctex run on the output
// dir (downloading output.synctex.gz from clsi-cache when the run dir is
// cache-based and the file is missing), run the command, tag errors.
func (m *Manager) runSynctex(projectID, userID string, command []string,
	opts SyncOpts) (stdout string, downloadedFromCache bool, err error) {
	if opts.ImageName != "" && !m.isImageNameAllowed(opts.ImageName) {
		return "", false, &cerrors.InvalidParameter{Message: "invalid image"}
	}
	if opts.EditorID != "" && !editorIDRegexp.MatchString(opts.EditorID) {
		return "", false, &cerrors.InvalidParameter{Message: "invalid editorId"}
	}
	if opts.BuildID != "" && !buildIDRegexp.MatchString(opts.BuildID) {
		return "", false, &cerrors.InvalidParameter{Message: "invalid buildId"}
	}

	outputDir := filepath.Join(m.Paths.OutputDir, projectID, userID)
	runInOutputDir := opts.BuildID != "" && m.Runner.CanRunSyncTeXInOutputDir()

	directory := compileDirOf(m.Paths.CompilesDir, projectID, userID)
	if runInOutputDir {
		directory = filepath.Join(outputDir, "generated-files", opts.BuildID)
	}
	const timeout = int64(60 * 1000) // increased to allow for large projects
	compileName := compileName(projectID, userID)
	compileGroup := "synctex"
	if runInOutputDir {
		compileGroup = "synctex-output"
	}

	result, qErr := m.QueueOnOutputDir(outputDir, func() (any, error) {
		downloadedFromCache := false
		if cerr := m.checkFileExists(directory, "output.synctex.gz"); cerr != nil {
			// The synctex file is absent. Node then, separately, probes
			// output.log when compileFromClsiCache (for downloadedFromCache),
			// and attempts the clsi-cache download iff editorId+buildId are
			// present.
			if opts.CompileFromClsiCache {
				if ferr := m.checkFileExists(directory, "output.log"); ferr != nil {
					if cerrors.IsNotFoundError(ferr) {
						downloadedFromCache = true
					}
				}
			}
			if cerrors.IsNotFoundError(cerr) && opts.CompileFromClsiCache &&
				opts.EditorID != "" && opts.BuildID != "" {
				var dlErr error
				downloadedFromCache, dlErr = m.DownloadOutputDotSynctex(
					projectID, userID, opts.EditorID, opts.BuildID, directory)
				if dlErr != nil {
					logger.Warn(map[string]any{
						"err": dlErr.Error(), "projectId": projectID,
						"userId": userID, "editorId": opts.EditorID,
						"buildId": opts.BuildID,
					}, "failed to download output.synctex.gz from clsi-cache")
				}
				if cerr = m.checkFileExists(directory, "output.synctex.gz"); cerr != nil {
					return nil, cerr
				}
			} else {
				return nil, cerr
			}
		}
		out, runErr := runOut(m.Runner, compileName, command, directory,
			m.DefaultImage, timeout, nil, compileGroup)
		if runErr != nil {
			return nil, cerrors.Tag(runErr, "error running synctex", map[string]any{
				"command":   command,
				"projectId": projectID,
				"userId":    userID,
			})
		}
		return map[string]any{
			"stdout": out.Stdout, "downloadedFromCache": downloadedFromCache,
		}, nil
	})
	if qErr != nil {
		return "", false, qErr
	}
	res, _ := result.(map[string]any)
	return res["stdout"].(string), res["downloadedFromCache"].(bool), nil
}

// SyncFromCode ports syncFromCode: synctex view <line>:<column>:<input>
// against <baseDir>/output.pdf. baseDir is the sandboxed synctex prefix
// (Settings.path.synctexBaseDir; always '/compile' here — the container
// mounts the host compile dir there).
func (m *Manager) SyncFromCode(projectID, userID, filename string,
	line, column int, opts SyncOpts) (SyncFromCodeResult, error) {
	compileName := compileName(projectID, userID)
	baseDir := filepath.Join(m.Paths.SynctexBase, compileName)
	inputFilePath := filepath.Join(baseDir, filename)
	outputFilePath := filepath.Join(baseDir, "output.pdf")
	command := []string{
		"synctex", "view",
		"-i", strconv.Itoa(line) + ":" + strconv.Itoa(column) + ":" + inputFilePath,
		"-o", outputFilePath,
	}
	stdout, downloadedFromCache, err := m.runSynctex(projectID, userID, command, opts)
	if err != nil {
		return SyncFromCodeResult{}, err
	}
	logger.Debug(map[string]any{
		"projectId": projectID, "userId": userID, "filename": filename,
		"line": line, "column": column, "command": command, "stdout": stdout,
	}, "synctex code output")
	return SyncFromCodeResult{
		CodePositions:       synctexparser.ParseViewOutput(stdout),
		DownloadedFromCache: downloadedFromCache,
	}, nil
}

// SyncFromPdf ports syncFromPdf: synctex edit <page>:<h>:<v>:<output.pdf>.
func (m *Manager) SyncFromPdf(projectID, userID string, page, h, v int,
	opts SyncOpts) (SyncFromPdfResult, error) {
	compileName := compileName(projectID, userID)
	baseDir := filepath.Join(m.Paths.SynctexBase, compileName)
	outputFilePath := filepath.Join(baseDir, "output.pdf")
	command := []string{
		"synctex", "edit",
		"-o", strconv.Itoa(page) + ":" + strconv.Itoa(h) + ":" + strconv.Itoa(v) + ":" + outputFilePath,
	}
	stdout, downloadedFromCache, err := m.runSynctex(projectID, userID, command, opts)
	if err != nil {
		return SyncFromPdfResult{}, err
	}
	logger.Debug(map[string]any{
		"projectId": projectID, "userId": userID,
		"page": page, "h": h, "v": v, "stdout": stdout,
	}, "synctex pdf output")
	return SyncFromPdfResult{
		PdfPositions:        synctexparser.ParseEditOutput(stdout, baseDir),
		DownloadedFromCache: downloadedFromCache,
	}, nil
}

// checkFileExists ports _checkFileExists: stat the dir (ENOENT ->
// NotFoundError 'no output directory'), stat the file (ENOENT ->
// NotFoundError 'no output file'), non-file -> 'not a file' error.
func (m *Manager) checkFileExists(dir, filename string) error {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cerrors.NewNotFoundError("no output directory")
		}
		return err
	}
	file := filepath.Join(dir, filename)
	fi, err := os.Stat(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cerrors.NewNotFoundError("no output file")
		}
		return err
	}
	if !fi.Mode().IsRegular() {
		return errors.New("not a file")
	}
	return nil
}
