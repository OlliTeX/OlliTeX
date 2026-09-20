// Package png2pdf ports services/clsi/app/js/Png2Pdf.js (96L).
//
// Node contract (Png2Pdf.test.js, 156L):
//
//   - isEnabled(): { enablePng2pdfConversions, clsi.dockerRunner===true,
//     path.sandboxedCompilesHostDirCache set, png2pdfImage set } (AND of all).
//   - convertPngFilesInCacheDir(projectId, cacheDir, relativePaths, stats,
//     timings):
//   - disabled -> no-op (no runner call).
//   - enabled -> CommandRunner.promises.run(projectId,
//     ['--in-place','--',...paths], cacheDir, image, timeoutMs*1000, {},
//     'png2pdf', null). stats.png2pdf = # "Converted X to PDF" stdout
//     lines; timings.png2pdf = timer.elapsed (port uses the Timer DoneMS).
//   - non-zero exit / transport error -> REJECT: OError('non-zero exit
//     code from png2pdf') tagged 'png2pdf conversion failed' with
//     {projectId,count}; timings STILL recorded (done() fires on throw too).
package png2pdf

import (
	"clsi/config"
	"clsi/errors"
	"clsi/logger"
	"clsi/metrics"
	"regexp"
)

// RunFunc is the promisified CommandRunner.run seam
// (Node: vi.doMock'd CommandRunner.promises.run). out = {stdout, stderr,
// exitCode}; err mirrors the docker run transport error (distinct from a
// non-zero container exit code).
type RunnerFunc func(projectID string, command []string, directory, image string,
	timeout int64, environment map[string]string, compileGroup, cwd string) (Output, error)

// Output is the promisified output shape {stdout, stderr, exitCode}.
type Output struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

var RunFunc RunnerFunc

// ClearSeams resets injection.
func ClearSeams() { RunFunc = nil }

var reConvertedLine = regexp.MustCompile(`(?m)^Converted .+ to PDF$`)

// IsEnabled mirrors Png2Pdf.isEnabled (reads the CLSI config).
func IsEnabled() bool {
	cfg := config.Get()
	return cfg.EnablePng2pdfConversions &&
		cfg.ClSI.DockerRunner &&
		cfg.PathSandbox.Cache != "" &&
		cfg.Png2pdfImage != ""
}

// Stats / Timings mirror the Node stats/timings object fields this module
// writes.
type Stats struct{ Png2pdf int }
type Timings struct{ Png2pdf int64 }

// ConvertPngFilesInCacheDir mirrors Png2Pdf.convertPngFilesInCacheDir.
// stats/timings are pointers (may be nil); on success stats are written, on
// error only timings (the done() runs before the throw).
func ConvertPngFilesInCacheDir(projectID, cacheProjectDir string, relativePaths []string, stats *Stats, timings *Timings) error {
	if !IsEnabled() {
		return nil
	}
	cfg := config.Get()
	timer := metrics.NewTimer("png2pdf")
	converted, runErr := runPng(cfg, projectID, cacheProjectDir, relativePaths)
	elapsed := timer.DoneMS()
	if runErr != nil {
		if timings != nil {
			timings.Png2pdf = elapsed
		}
		return errors.Tag(runErr, "png2pdf conversion failed", map[string]any{
			"projectId": projectID,
			"count":     len(relativePaths),
		})
	}
	metrics.Count("png2pdf-converted", converted)
	if stats != nil {
		stats.Png2pdf = converted
	}
	if timings != nil {
		timings.Png2pdf = elapsed
	}
	logger.Debug(map[string]any{"projectId": projectID, "attempted": len(relativePaths), "converted": converted},
		"png2pdf conversion completed")
	return nil
}

func runPng(cfg *config.Config, projectID, cacheProjectDir string, relativePaths []string) (int, error) {
	if RunFunc == nil {
		return 0, errors.NewOError("png2pdf runner not configured (no CommandRunner)")
	}
	out, err := RunFunc(projectID,
		append([]string{"--in-place", "--"}, relativePaths...),
		cacheProjectDir, cfg.Png2pdfImage, int64(cfg.ConversionTimeoutSeconds*1000),
		map[string]string{}, "png2pdf", "")
	if err != nil {
		return 0, err
	}
	if out.ExitCode != 0 {
		return 0, errors.NewOError("non-zero exit code from png2pdf", map[string]any{
			"exitCode": out.ExitCode,
			"stdout":   out.Stdout,
			"stderr":   out.Stderr,
		})
	}
	return len(reConvertedLine.FindAllString(out.Stdout, -1)), nil
}
