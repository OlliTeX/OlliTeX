// Package tikzmanager ports services/clsi/app/js/TikzManager.js (129L).
//
// For \tikzexternalize or pstool to work the main file must match the jobname
// (compileManager sets -jobname output), so a copy of the main file is written
// as <compileDir>/output.tex. Two steps:
//
//  1. CheckMainFile returns true if the main file USES tikz-externalize /
//     pstool (and no output.tex already exists in the resources).
//  2. InjectOutputFile copies the main file to <compileDir>/output.tex using
//     the exclusive "wx" flag so an existing output.tex is never clobbered.
//
// Promisified (Node CheckMainFile / InjectOutputFile / WriteOutputFileIfNeeded
// are promisified in CLSI; Go uses the error/bool value return).
package tikzmanager

import (
	"os"
	"path/filepath"

	"clsi/logger"
	"clsi/resourcewriter"
	"clsi/safereader"
)

// OutputTex mirrors TikzManager.OUTPUT_TEX.
const OutputTex = "output.tex"

// UsesTikzExternalize mirrors TikzManager.usesTikzExternalize.
func UsesTikzExternalize(content string) bool {
	return contains(content, "\\tikzexternalize") || contains(content, "{pstool}")
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// WriteOutputFileIfNeeded mirrors writeOutputFileIfNeeded(compileDir,
// snapshot, content). hasOutputTex stands in for snapshot.getFile('output.
// tex') (the compile port supplies that lookup). When no output.tex exists and
// the content uses tikz-externalize / pstool, the content is written to
// <compileDir>/output.tex.
func WriteOutputFileIfNeeded(compileDir string, hasOutputTex bool, content string) error {
	if hasOutputTex {
		return nil
	}
	if !UsesTikzExternalize(content) {
		return nil
	}
	return os.WriteFile(filepath.Join(compileDir, OutputTex), []byte(content), 0o644)
}

// CheckMainFile mirrors CheckMainFile(compileDir, mainFile, resources) —
// returns (needsMainFile, err). If any resource is already 'output.tex' it
// returns (false, nil) (no need to copy). Otherwise it reads the main file
// (up to 65536 bytes — tikz/pgf/pstool detection only needs to see the
// package uses) and reports whether a copy is needed.
func CheckMainFile(compileDir, mainFile string, resources []resourcewriter.Resource) (bool, error) {
	for _, r := range resources {
		if r.Path == OutputTex {
			logger.Debug(map[string]any{"compileDir": compileDir, "mainFile": mainFile},
				"output.tex already in resources")
			return false, nil
		}
	}
	p, err := resourcewriter.CheckPath(compileDir, mainFile)
	if err != nil {
		return false, err
	}
	content, _, err := safereader.ReadFile(p, 65536)
	if err != nil {
		return false, err
	}
	needs := UsesTikzExternalize(content)
	logger.Debug(map[string]any{"compileDir": compileDir, "mainFile": mainFile, "needsMainFile": needs},
		"checked for packages needing main file as output.tex")
	return needs, nil
}

// InjectOutputFile mirrors InjectOutputFile(compileDir, mainFile) — copies the
// main file to <compileDir>/output.tex using the exclusive "wx" flag so an
// existing output.tex is never overwritten.
func InjectOutputFile(compileDir, mainFile string) error {
	p, err := resourcewriter.CheckPath(compileDir, mainFile)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	logger.Debug(map[string]any{"compileDir": compileDir, "mainFile": mainFile},
		"copied file to output.tex as project uses package which requires it")
	f, err := os.OpenFile(filepath.Join(compileDir, OutputTex),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, werr := f.Write(content)
	return werr
}
