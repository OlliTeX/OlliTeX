package compilemanager

import (
	"os"
	"path/filepath"

	"ollitex/go/services/clsitypst/commandrunner"
	clsl "ollitex/go/services/clsitypst/logger"
	"ollitex/go/services/clsitypst/resourcewriter"
)

// WordcountResults mirrors the Node wordcount result object the controller
// embeds (clsi shape: {encode, textWords, headWords, outside, headers,
// elements, mathInline, mathDisplay, errors, messages}).
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

// Wordometer artifact names (Node WordcountInjector): the `__clsi_wc_*`
// files injected next to the project resources (user files are never
// mutated) and the rendered marker PDF.
const (
	WCMain       = "__clsi_wc_main.typ"
	WCWordometer = "__clsi_wordometer.typ"
	WCOutPDF     = "__clsi_wc_out.pdf"
)

// Wordcount ports wordcount (Node CompileManager.wordcount):
//
//  1. mkdir + prepare the compile dir
//  2. sync resources IF the request body (POST /wordcount) carries them;
//     GET (req == nil) counts the last-synced sources
//  3. the root resource must exist on disk (NotFoundError: "no compiled
//     state to word count: <root> not synced")
//  4. try: inject the wordometer (T7 seam), docker-compile
//     `typst compile __clsi_wc_main.typ __clsi_wc_out.pdf` (NOT the
//     'wordcount' compile group — it mounts /compile read-only, texcount
//     parity; the wordometer driver has to write the marker PDF) and read
//     the rendered marker back from the PDF (T7 seam); fall back to
//     `wc -w` when any step fails (plan §9: degrade, not fail)
//  5. finally: remove the artifacts
//
// D5: the PDF marker read is the clsi-parity default (Node reads the
// rendered TOTAL_WORDS/HEADING_WORDS/NUM_HEADINGS marker with pdfjs-dist;
// Go uses the T7 fixture-picked PDF text lib, injected via ReadPdfMarker).
func (m *Manager) Wordcount(projectID, userID, file, image string, req *Request) (WordcountResults, error) {
	compileDir := compileDirOf(m.Paths.CompilesDir, projectID, userID)

	// Node: file || (request && request.rootResourcePath) || 'main.typ'
	rootResource := file
	if rootResource == "" && req != nil {
		rootResource = req.RootResourcePath
	}
	if rootResource == "" {
		rootResource = "main.typ"
	}

	if _, err := m.MkdirAll(compileDir); err != nil {
		return WordcountResults{}, err
	}
	m.PrepareCompileDir(compileDir)

	// A POST /wordcount carries the project state as a compile request body
	// (same as clsi wordcountWithSync); a GET uses the last synced sources.
	if req != nil && len(req.RWResources) > 0 {
		_, _ = m.ResourceSync(rwRequest(req), compileDir)
	}

	// Node: fs.access(rootPath) existence check.
	fi, statErr := os.Stat(filepath.Join(compileDir, rootResource))
	rootExists := statErr == nil && fi != nil && fi.Mode().IsRegular()
	if !rootExists {
		return WordcountResults{}, newNotFoundError(
			"no compiled state to word count: " + rootResource + " not synced")
	}

	if m.InjectWordometer != nil && m.ReadPdfMarker != nil && m.RemoveArtifacts != nil {
		if err := m.InjectWordometer(compileDir, rootResource); err == nil {
			// finally: remove the artifacts regardless of outcome.
			defer m.RemoveArtifacts(compileDir)

			// Node: NOT the 'wordcount' compile group (that mounts
			// /compile read-only), the wordometer driver has to write the
			// marker PDF.
			_, rErr := runCmdOut(m.Runner, compileName(projectID, ""),
				[]string{"typst", "compile", WCMain, WCOutPDF},
				compileDir, image, wordcountTimeout(req, false), m.dwordcountEnv(), "")
			if rErr == nil {
				if total, heading, headings, ok := m.ReadPdfMarker(filepath.Join(compileDir, WCOutPDF)); ok {
					clsl.Debug(map[string]any{
						"projectId": projectID, "userId": userID,
						"totalWords": total, "headWords": heading,
						"numHeadings": headings,
					}, "word count results (wordometer)")
					// `#total-words` includes heading words (verified
					// against wordometer 0.1.5: headings are not
					// subtracted), so keep that parity: textWords = total.
					return WordcountResults{
						Encode:    "utf-8",
						TextWords: total,
						HeadWords: heading,
						Headers:   headings,
					}, nil
				}
			}
			// marker absent OR compile failed: fall through to the
			// fallback (Node: throw -> catch -> _wordcountWcFallback).
			clsl.Warn(map[string]any{
				"projectId": projectID, "userId": userID, "rootResource": rootResource,
			}, "wordometer wordcount failed; falling back to wc -w (plan §9)")
		}
	}

	return m.wcFallback(projectID, userID, rootResource, image, req)
}

// dwordcountEnv mirrors Node `{...Settings.clsi.docker.env, ...TYPST_DOCKER_ENV}`.
func (m *Manager) dwordcountEnv() map[string]string {
	env := map[string]string{}
	for k, v := range m.DockerEnv {
		env[k] = v
	}
	for k, v := range TYPST_DOCKER_ENV {
		env[k] = v
	}
	return env
}

// wordcountTimeout mirrors (request && request.timeout) || 60_000; fallback
// uses the 5*60*1000 budget (Node: wc fallback has its own).
func wordcountTimeout(req *Request, fallback bool) int64 {
	if req != nil && req.Timeout > 0 {
		return int64(req.Timeout)
	}
	if fallback {
		return 5 * 60 * 1000
	}
	return 60 * 1000
}

// rwRequest maps the (manager) Request onto the resourcewriter request the
// sync seam consumes.
func rwRequest(req *Request) *resourcewriter.Request {
	return &resourcewriter.Request{
		ProjectID:   req.ProjectID,
		UserID:      req.UserID,
		SyncType:    req.SyncType,
		SyncState:   req.SyncState,
		Resources:   req.RWResources,
		MetricsPath: req.MetricsOpts.Path,
	}
}

// wcFallback ports _wordcountWcFallback: `wc -w "<file>" || echo "0
// /compile/<file>"` with the 'wordcount' compile group (read-only mount is
// fine — wc only reads).
func (m *Manager) wcFallback(projectID, userID, file, image string, req *Request) (WordcountResults, error) {
	compileDir := compileDirOf(m.Paths.CompilesDir, projectID, userID)
	out, err := runCmdOut(m.Runner, compileName(projectID, userID),
		buildWCCommand(file), compileDir, image, wordcountTimeout(req, true), m.dwordcountEnv(), "wordcount")
	if err != nil {
		return WordcountResults{}, err
	}
	return parseWordcountOutput(out.Stdout), nil
}

// buildWCCommand ports the Node template:
//
//	`wc -w "${Path.join('$COMPILE_DIR', file)}" || echo "0 /compile/${file}"`
//
// ($COMPILE_DIR is substituted by DockerRunner; /compile/ is the sandbox
// mount root for the fallback echo.)
func buildWCCommand(file string) []string {
	return []string{
		"sh", "-c",
		`wc -w "` + "$COMPILE_DIR/" + file + `" || echo "0 /compile/` + file + `"`,
	}
}

// parseWordcountOutput ports _parseWordcountOutput: the first /\d+/ run of
// the `wc -w` "<N> <file>" stdout maps to textWords (all other counts 0).
func parseWordcountOutput(stdout string) WordcountResults {
	for i := 0; i < len(stdout); i++ {
		if stdout[i] >= '0' && stdout[i] <= '9' {
			n := 0
			j := i
			for j < len(stdout) && stdout[j] >= '0' && stdout[j] <= '9' {
				n = n*10 + int(stdout[j]-'0')
				j++
			}
			return WordcountResults{Encode: "utf-8", TextWords: n}
		}
	}
	return WordcountResults{Encode: "utf-8"}
}

// runCmdOut blocks on the generic Runner callback (mirrors the promisified
// Node form — the copied dockerrunner satisfies the Runner interface).
func runCmdOut(r commandrunner.Runner, projectID string, command []string,
	directory, image string, timeout int64, env map[string]string,
	group string) (*commandrunner.RunOutput, error) {
	outCh := make(chan *commandrunner.RunOutput, 1)
	errCh := make(chan error, 1)
	r.Run(projectID, command, directory, image, timeout, env, group, "",
		func(err error, out *commandrunner.RunOutput) {
			if err != nil {
				errCh <- err
				return
			}
			outCh <- out
		})
	select {
	case o := <-outCh:
		return o, nil
	case err := <-errCh:
		return nil, err
	}
}
