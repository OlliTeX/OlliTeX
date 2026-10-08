package compilemanager

import (
	"context"
	"fmt"
	"testing"

	"ollitex/go/services/clsitex/commandrunner"
)

// zzProbe: E-item live repro — call runSynctex the way SyncFromPdf does and
// print the exact error (NotFound message names the missing dir/file).
type probeRunner struct{}

func (probeRunner) Run(projectID string, command []string, directory string,
	image string, timeout int64, environment map[string]string,
	compileGroup string, cwd string, callback func(err error, out *commandrunner.RunOutput)) string {
	return "probe"
}
func (probeRunner) Kill(containerID string, callback func(error))     {}
func (probeRunner) CanRunSyncTeXInOutputDir() bool                   { return true }

func TestZZProbeSynctexDir(t *testing.T) {
	m := New(probeRunner{}, nil)
	opts := SyncOpts{EditorID: "abc-def", BuildID: "1a11406a540-5a233a1e741c3b45", CompileFromClsiCache: false}
	cmd := []string{"synctex", "edit", "-o", "1:251:273:/compile/6ac54f1acb0b784fbc32d3be-671b6c9ef2e168f259294e9a/output.pdf"}
	_, _, err := m.runSynctex("6ac54f1acb0b784fbc32d3be", "671b6c9ef2e168f259294e9a", cmd, opts)
	fmt.Printf("PATHS compiles=%q output=%q synctexBase=%q\n", m.Paths.CompilesDir, m.Paths.OutputDir, m.Paths.SynctexBase)
	fmt.Printf("CAN_OUT=%v\n", m.Runner.CanRunSyncTeXInOutputDir())
	if err != nil {
		fmt.Printf("ERR=%v\n", err)
	} else {
		fmt.Println("OK")
	}
	_ = context.Background()
}
