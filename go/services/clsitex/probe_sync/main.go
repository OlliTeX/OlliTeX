package main

import (
	"fmt"
	"os"
	"os/exec"

	"ollitex/go/services/clsitex/commandrunner"
	"ollitex/go/services/clsitex/compilemanager"
)

// probeRunner — runs the command locally with exec (the container has the
// synctex binary), delivering stdout through the callback like a real
// runner would.
type probeRunner struct{}

func (probeRunner) Run(projectID string, command []string, directory string,
	image string, timeout int64, environment map[string]string,
	compileGroup string, cwd string, callback func(err error, out *commandrunner.RunOutput)) string {
	name := "probe"
	out := &commandrunner.RunOutput{}
	out.Stdout = ""
	out.Stderr = ""
	if len(command) > 0 {
		c := exec.Command(command[0], command[1:]...)
		c.Dir = directory
		c.Env = environ(environment)
		var so, se string
		if b, e := c.Output(); e != nil {
			if ee, ok := e.(*exec.ExitError); ok {
				se = string(ee.Stderr)
			} else {
				fmt.Printf("probe exec error: %v\n", e)
				callback(e, out)
				return name
			}
		} else {
			so = string(b)
		}
		out.Stdout, out.Stderr = so, se
	}
	callback(nil, out)
	return name
}

func environ(env map[string]string) []string {
	base := append(os.Environ(), env2str(env)...)
	return base
}

func env2str(env map[string]string) []string {
	var out []string
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func (probeRunner) Kill(id string, cb func(error))       {}
func (probeRunner) CanRunSyncTeXInOutputDir() bool       { return true }

func main() {
	m := compilemanager.New(probeRunner{}, nil)
	fmt.Printf("PATHS compiles=%q output=%q synctexBase=%q\n",
		m.Paths.CompilesDir, m.Paths.OutputDir, m.Paths.SynctexBase)
	fmt.Printf("CAN_OUT=%v\n", m.Runner.CanRunSyncTeXInOutputDir())
	dir := "/var/lib/overleaf/data/output/6ac54f1acb0b784fbc32d3be-671b6c9ef2e168f259294e9a/generated-files/1a11406a540-5a233a1e741c3b45"
	if fi, err := os.Stat(dir); err != nil {
		fmt.Printf("STAT dir: ERR=%v\n", err)
	} else {
		fmt.Printf("STAT dir: OK mode=%v\n", fi.Mode())
	}
	if fi, err := os.Stat(dir + "/output.synctex.gz"); err != nil {
		fmt.Printf("STAT file: ERR=%v\n", err)
	} else {
		fmt.Printf("STAT file: OK size=%d\n", fi.Size())
	}
	res, err := m.SyncFromPdf("6ac54f1acb0b784fbc32d3be", "671b6c9ef2e168f259294e9a",
		1, 251, 273, compilemanager.SyncOpts{
			EditorID: "abc-def", BuildID: "1a11406a540-5a233a1e741c3b45",
			CompileFromClsiCache: false,
		})
	if err != nil {
		fmt.Printf("SyncFromPdf ERR=%v\n", err)
	} else {
		fmt.Printf("SyncFromPdf OK positions=%d\n", len(res.PdfPositions))
	}
}
