// Package conversionmanager ports services/clsi/app/js/ConversionManager.js
// (404L). It drives two docker-based pandoc/pdftocairo conversions, always
// through the sandboxed CommandRunner (the Docker runner), never in-process.
//
// Faithfulness notes:
//
//   - All "promises.run" calls are the awaited form of CommandRunner.run. In
//     Go we bridge the synchronous callback form to a blocking (out, err)
//     return — the Docker runner fires the callback on completion (sync or
//     from a goroutine alike).
//   - Lock functions acquire a LockManager lock on the conversion/compile dir
//     and release in `finally`.
//   - Error envelopes are faithful: unsupported type => OError (info {type /
//     conversionType}); non-zero pandoc/zip exit => ConversionError (pandoc)
//     or OError (zip) on the "try" path; any other error inside the "try" is
//     wrapped as `new OError('pandoc conversion failed').withCause(err)`; the
//     conversion dir is removed (best-effort) on error.
//   - compressOutput stages a uuid subdir so the archive root is flat (mirrors
//     the Node staging comments + WorkingDir=<uuid>, `--resource-path=..`,
//     `zip -r ../<id>.zip .`).
//
// The Runner is injected (the real implementation is dockerclient, not yet
// ported); tests use an in-process fake.
package conversionmanager

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"clsi/commandrunner"
	"clsi/config"
	oerrors "clsi/errors"
	"clsi/lockmanager"
)

// config tables (mirrors the JS module constants).
type latexConversionConfig struct {
	inputFilename string
	pandocArgs    []string
}

var conversionConfigs = map[string]latexConversionConfig{
	"docx":     {inputFilename: "input.docx", pandocArgs: []string{"--extract-media=.", "--from", "docx+citations", "--citeproc"}},
	"markdown": {inputFilename: "input.md", pandocArgs: []string{"--from", "markdown"}},
}

type pdfToJpegConfig struct {
	width   int
	quality int
}

var pdfToJpegConfigs = map[string]pdfToJpegConfig{
	"preview":   {width: 794, quality: 90},
	"thumbnail": {width: 190, quality: 50},
}

const (
	pdfToJpegInputFilename  = "input.pdf"
	pdfToJpegOutputFilename = "output.jpg"
	pdfToJpegOutputBasename = "output"
)

type latexExportConfig struct {
	fileExtension     string
	compressOutput    bool
	getPandocArgs     func(outputPath string) []string
}

var latexExportConfigs = map[string]latexExportConfig{
	"docx": {
		fileExtension:  "docx",
		compressOutput: false,
		getPandocArgs: func(outputPath string) []string {
			return []string{"--output", outputPath, "--from", "latex", "--to", "docx", "--citeproc", "--number-sections"}
		},
	},
	"markdown": {
		fileExtension:  "md",
		compressOutput: true,
		getPandocArgs: func(outputPath string) []string {
			return []string{"--output", outputPath, "--from", "latex", "--to", "markdown"}
		},
	},
	"html": {
		fileExtension:  "html",
		compressOutput: true,
		getPandocArgs: func(outputPath string) []string {
			return []string{"--output", outputPath, "--from", "latex", "--to", "html", "--standalone", "--mathml"}
		},
	},
}

// Manager wires the conversions (Runner + settings + lock seam).
type Manager struct {
	Runner          commandrunner.Runner
	CompilesDir     string
	TimeoutMs       int64
	PandocImage     string
	PdftocairoImage string

	acquire  func(key string) (*lockmanager.Lock, error)
	logDebug func(msg string, attrs map[string]any)
	uuid     func() string
}

// New wires the manager from config + runner (production path).
func New(runner commandrunner.Runner, cfg *config.Config, logDebug func(msg string, attrs map[string]any)) *Manager {
	if logDebug == nil {
		logDebug = func(string, map[string]any) {}
	}
	return &Manager{
		Runner:          runner,
		CompilesDir:     cfg.Path.CompilesDir,
		TimeoutMs:       int64(cfg.ConversionTimeoutSeconds) * 1000,
		PandocImage:     cfg.PandocImage,
		PdftocairoImage: cfg.PdftocairoImage,
		acquire:         lockmanager.Acquire,
		logDebug:        logDebug,
		uuid:            uuidV4,
	}
}

// runPromise bridges the synchronous Run callback to a blocking (out, err),
// mirroring CommandRunner.promises.run (resolve the output object).
func (m *Manager) runPromise(projectID string, command []string, directory string, image string,
	timeout int64, environment map[string]string, compileGroup string, cwd string) (*commandrunner.RunOutput, error) {
	var (
		out  *commandrunner.RunOutput
		rerr error
	)
	ch := make(chan struct{})
	m.Runner.Run(projectID, command, directory, image, timeout, environment, compileGroup, cwd, func(err error, o *commandrunner.RunOutput) {
		rerr, out = err, o
		close(ch)
	})
	<-ch
	return out, rerr
}

func (m *Manager) log(msg string, attrs map[string]any) {
	if m.logDebug != nil {
		m.logDebug(msg, attrs)
	}
}

func uuidV4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f|0x40 // version 4
	b[8] = b[8]&0x3f|0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// convertToLaTeX mirrors the inner convertToLaTeX (no lock).
func (m *Manager) convertToLaTeX(conversionId, conversionDir, inputPath, conversionType string) (string, error) {
	cc, ok := conversionConfigs[conversionType]
	if !ok {
		return "", oerrors.NewOError("unsupported conversion type", map[string]any{"conversionType": conversionType})
	}
	// Node: mkdir + copyFile are OUTSIDE the try/catch (raw failures).
	if err := os.MkdirAll(conversionDir, 0o755); err != nil {
		return "", err
	}
	newSourcePath := filepath.Join(conversionDir, cc.inputFilename)
	if err := copyFile(inputPath, newSourcePath); err != nil {
		return "", err
	}

	outputName := m.uuid() + ".zip"
	// "try" region: pandoc -> (best-effort) unlink input -> zip.
	var opErr error
	do := func() error {
		out, err := m.runPromise(conversionId,
			append([]string{"pandoc", cc.inputFilename, "--output", "main.tex", "--to", "latex", "--standalone"}, cc.pandocArgs...),
			conversionDir, m.PandocImage, m.TimeoutMs, nil, "conversions", "")
		if err != nil {
			return err
		}
		if out.ExitCode != 0 {
			return oerrors.NewConversionErrorT("Non-zero exit code from pandoc", conversionType, out.Stderr, out.ExitCode)
		}
		m.log("conversion command completed", map[string]any{"stdout": out.Stdout, "stderr": out.Stderr, "exitCode": out.ExitCode})
		_ = os.Remove(newSourcePath) // fs.unlink().catch(()=>{})
		zipOut, err := m.runPromise(conversionId,
			[]string{"zip", "-r", outputName, "."},
			conversionDir, m.PandocImage, m.TimeoutMs, nil, "conversions", "")
		if err != nil {
			return err
		}
		if zipOut.ExitCode != 0 {
			return oerrors.NewOError("Non-zero exit code from pandoc", map[string]any{"exitCode": zipOut.ExitCode, "stderr": zipOut.Stderr})
		}
		m.log("conversion output compressed", map[string]any{"stdout": zipOut.Stdout, "stderr": zipOut.Stderr, "exitCode": zipOut.ExitCode})
		return nil
	}
	opErr = do()
	if opErr != nil {
		_ = os.RemoveAll(conversionDir) // fs.rm(...).catch(()=>{})
		var convErr *oerrors.ConversionError
		if errors.As(opErr, &convErr) {
			return "", opErr
		}
		return "", oerrors.NewOError("pandoc conversion failed").WithCause(opErr)
	}
	return filepath.Join(conversionDir, outputName), nil
}

// convertLaTeXToDocumentInDir mirrors the inner convertLaTeXToDocumentInDir.
func (m *Manager) convertLaTeXToDocumentInDir(conversionId, compileDir, rootDocPath, docType string) (string, error) {
	cfg, ok := latexExportConfigs[docType]
	if !ok {
		return "", oerrors.NewOError("unsupported conversion type", map[string]any{"type": docType})
	}
	timeoutMs := m.TimeoutMs
	outputId := m.uuid()
	m.log("running pandoc latex-to-document in compile dir", map[string]any{"compileDir": compileDir, "rootDocPath": rootDocPath, "type": docType})

	if !cfg.compressOutput {
		outputName := outputId + "." + cfg.fileExtension
		out, err := m.runPromise(conversionId,
			append(append([]string{"pandoc", rootDocPath}, cfg.getPandocArgs(outputName)...), "--resource-path=."),
			compileDir, m.PandocImage, timeoutMs, nil, "conversions", "")
		if err != nil {
			return "", err
		}
		if out.ExitCode != 0 {
			return "", oerrors.NewConversionErrorT("pandoc latex-to-document conversion failed", docType, out.Stderr, out.ExitCode)
		}
		m.log("pandoc latex-to-document conversion completed", map[string]any{"stdout": out.Stdout, "stderr": out.Stderr, "exitCode": out.ExitCode})
		return filepath.Join(compileDir, outputName), nil
	}

	// Compressed output: stage a uuid subdir so the archive root is flat.
	staging := filepath.Join(compileDir, outputId)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", err
	}
	outputName := "main." + cfg.fileExtension
	finalOutputName := outputId + ".zip"

	out, err := m.runPromise(conversionId,
		append(append([]string{"pandoc", filepath.Join("..", rootDocPath)}, cfg.getPandocArgs(outputName)...), "--resource-path=..", "--extract-media=."),
		compileDir, m.PandocImage, timeoutMs, map[string]string{"TEXINPUTS": "..:"}, "conversions", outputId)
	if err != nil {
		return "", err
	}
	if out.ExitCode != 0 {
		return "", oerrors.NewConversionErrorT("pandoc latex-to-document conversion failed", docType, out.Stderr, out.ExitCode)
	}
	m.log("pandoc latex-to-document conversion completed", map[string]any{"stdout": out.Stdout, "stderr": out.Stderr, "exitCode": out.ExitCode})

	zipOut, zipErr := m.runPromise(conversionId,
		[]string{"zip", "-r", filepath.Join("..", finalOutputName), "."},
		compileDir, m.PandocImage, timeoutMs, nil, "conversions", outputId)
	if zipErr != nil {
		return "", zipErr
	}
	if zipOut.ExitCode != 0 {
		return "", oerrors.NewOError("zip compression of export failed", map[string]any{"exitCode": zipOut.ExitCode, "stdout": zipOut.Stdout, "stderr": zipOut.Stderr})
	}
	m.log("export compressed", map[string]any{"stdout": zipOut.Stdout, "stderr": zipOut.Stderr, "exitCode": zipOut.ExitCode})

	return filepath.Join(compileDir, finalOutputName), nil
}

func (m *Manager) convertPDFToJPEG(conversionId, conversionDir, inputPath, mode string) (string, error) {
	cfg, ok := pdfToJpegConfigs[mode]
	if !ok {
		return "", oerrors.NewOError("unsupported conversion mode", map[string]any{"mode": mode})
	}
	if err := os.MkdirAll(conversionDir, 0o755); err != nil {
		return "", err
	}
	newSourcePath := filepath.Join(conversionDir, pdfToJpegInputFilename)
	if err := copyFile(inputPath, newSourcePath); err != nil {
		return "", err
	}
	dstPath := filepath.Join(conversionDir, pdfToJpegOutputFilename)

	opErr := func() error {
		out, err := m.runPromise(conversionId,
			[]string{"pdftocairo", "-jpeg", "-jpegopt", fmt.Sprintf("quality=%d", cfg.quality),
				"-singlefile", "-scale-to-x", fmt.Sprintf("%d", cfg.width), "-scale-to-y", "-1",
				pdfToJpegInputFilename, pdfToJpegOutputBasename},
			conversionDir, m.PdftocairoImage, m.TimeoutMs, nil, "conversions", "")
		if err != nil {
			return err
		}
		if out.ExitCode != 0 {
			return oerrors.NewOError("Non-zero exit code from pdftocairo", map[string]any{"exitCode": out.ExitCode, "stderr": out.Stderr})
		}
		m.log("pdf-to-jpeg conversion completed", map[string]any{"stdout": out.Stdout, "stderr": out.Stderr, "exitCode": out.ExitCode})
		fi, err := os.Lstat(dstPath)
		if err != nil {
			// Node: fs.lstat rejects (e.g. ENOENT) -> outer catch wraps it.
			return err
		}
		if !fi.Mode().IsRegular() {
			return oerrors.NewOError("output.jpg is not a regular file", map[string]any{"stat": fi.Mode().String()})
		}
		_ = os.Remove(newSourcePath)
		return nil
	}()

	if opErr != nil {
		_ = os.RemoveAll(conversionDir)
		return "", oerrors.NewOError("pdf-to-jpeg conversion failed").WithCause(opErr)
	}
	return dstPath, nil
}

// WithLock functions (the exported promises surface the controller uses).

func (m *Manager) ConvertToLaTeXWithLock(conversionId, inputPath, conversionType string) (string, error) {
	conversionDir := filepath.Join(m.CompilesDir, conversionId)
	lock, err := m.acquire(conversionDir)
	if err != nil {
		return "", err
	}
	defer lock.Release()
	return m.convertToLaTeX(conversionId, conversionDir, inputPath, conversionType)
}

func (m *Manager) ConvertLaTeXToDocumentInDirWithLock(conversionId, compileDir, rootDocPath, docType string) (string, error) {
	lock, err := m.acquire(compileDir)
	if err != nil {
		return "", err
	}
	defer lock.Release()
	return m.convertLaTeXToDocumentInDir(conversionId, compileDir, rootDocPath, docType)
}

func (m *Manager) ConvertPDFToJPEGWithLock(conversionId, inputPath, mode string) (string, error) {
	conversionDir := filepath.Join(m.CompilesDir, conversionId)
	lock, err := m.acquire(conversionDir)
	if err != nil {
		return "", err
	}
	defer lock.Release()
	return m.convertPDFToJPEG(conversionId, conversionDir, inputPath, mode)
}
