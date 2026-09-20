// Package outputfileoptimiser ports services/clsi/app/js/OutputFileOptimiser.js.
//
// Node parity:
//
//   - optimiseFile(src, dst, cb) only acts on src matching /\/output\.pdf$/.
//   - checkIfPDFIsOptimised(file, cb): opens the file, allocates a 16 KiB
//     zero buffer, does one fs.read at offset 0 (short read at EOF is
//     normal) and reports true iff the ascii buffer contains
//     "/Linearized 1". Open/read error -> callback(err) (surfaces); close
//     error is swallowed by the quirky typeof-errReadClose check.
//   - optimisePDF(src, dst, cb): spawn('qpdf',
//     ['--linearize', '--newline-before-endstream', src, dst+'.opt'],
//     {stdio:'ignore'}); spawn error AND non-zero close code are
//     warn-swallowed (callback(null)); zero close code renames dst.opt ->
//     dst; a rename failure is warn-swallowed.
//
// Port notes:
//
//   - The qpdf exec is routed through RunQPDF (a package-level var whose
//     default value is defaultImpl, a real os/exec invocation); tests may
//     substitute RunQPDF.
//   - The swallow-and-warn contract is preserved through the warn hook
//     (a nil warn function == Node's logger.warn when the logger is a no-op).
package outputfileoptimiser

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
)

const headerSize = 16 * 1024

// linearizedMarker is the substring Node probes for.
const linearizedMarker = "/Linearized 1"

// CheckIfPDFIsOptimised ports checkIfPDFIsOptimised(file, cb).
func CheckIfPDFIsOptimised(file string) (isOptimised bool, err error) {
	f, ferr := os.Open(file)
	if ferr != nil {
		return false, ferr
	}
	defer f.Close()
	buf := make([]byte, headerSize)
	n, _ := f.ReadAt(buf, 0)
	buf = buf[:n]
	return bytes.Contains(buf, []byte(linearizedMarker)), nil
}

func defaultImpl(src, dstOpt string) error {
	cmd := exec.Command("qpdf", "--linearize", "--newline-before-endstream", src, dstOpt)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// RunQPDF is the injectable qpdf linearisation step:
//
//	qpdf --linearize --newline-before-endstream src dstOpt
var RunQPDF func(src, dstOpt string) error = defaultImpl

func isOutputPDF(src string) bool {
	return strings.HasSuffix(src, "/output.pdf")
}

// OptimiseFile ports optimiseFile(src, dst, cb).
func OptimiseFile(src, dst string, warn func(context map[string]any, args ...any)) error {
	if !isOutputPDF(src) {
		return nil
	}
	if warn == nil { // defensive: Node's logger always present
		warn = func(map[string]any, ...any) {}
	}
	isOptimised, cErr := CheckIfPDFIsOptimised(src)
	if cErr != nil {
		warn(map[string]any{"src": src}, "checkIfPDFIsOptimised error")
		return cErr
	}
	if isOptimised {
		return nil
	}
	tmpOutput := dst + ".opt"
	if qErr := RunQPDF(src, tmpOutput); qErr != nil {
		warn(map[string]any{"src": src, "qpdf": qErr.Error()}, "qpdf failed")
		return nil
	}
	if rErr := os.Rename(tmpOutput, dst); rErr != nil {
		warn(map[string]any{"tmp": tmpOutput, "dst": dst}, "failed to rename output of qpdf command")
	}
	return nil
}
