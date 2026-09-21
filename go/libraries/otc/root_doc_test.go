package otc

// root_doc_test.go — 1:1 mirror of test/unit/root_doc.test.js (the Node oracle).

import (
	"reflect"
	"strings"
	"testing"
)

const (
	rdWithClass    = "\\documentclass{article}\n\\begin{document}\nhi\n"
	rdWithoutClass = "\\section{One}\nsome text\n"
)

// rdSnapshot builds a Snapshot of docs: pathname -> {content, metadata}.
func rdSnapshot(t *testing.T, docs map[string]map[string]any) *Snapshot {
	t.Helper()
	files := map[string]*File{}
	for p, spec := range docs {
		content, _ := spec["content"].(string)
		file, err := FileFromString(content, nil)
		if err != nil {
			t.Fatalf("FileFromString(%q): %v", p, err)
		}
		if md, ok := spec["metadata"].(map[string]any); ok {
			file.SetMetadata(md)
		}
		files[p] = file
	}
	fm, err := NewFileMap(files)
	if err != nil {
		t.Fatalf("NewFileMap: %v", err)
	}
	return NewSnapshot(fm, nil, nil, nil)
}

func rdSameOpsRaw(t *testing.T, label string, ops []Operation, want []map[string]any) {
	t.Helper()
	if len(ops) != len(want) {
		t.Fatalf("%s: %d ops, want %d", label, len(ops), len(want))
	}
	for i := range want {
		if got := ops[i].ToRaw(); !reflect.DeepEqual(got, want[i]) {
			t.Errorf("%s: ops[%d].ToRaw() = %v, want %v", label, i, got, want[i])
		}
	}
}

func TestRD_IsRootDocCandidate(t *testing.T) {
	if !IsRootDocCandidate("paper.tex", rdWithClass) {
		t.Error("paper.tex with a document class must be a candidate")
	}
	if !IsRootDocCandidate("paper.Rtex", rdWithClass) {
		t.Error("paper.Rtex with a document class must be a candidate")
	}
	if IsRootDocCandidate("chapter.tex", rdWithoutClass) {
		t.Error("a doc declaring no class must not be a candidate")
	}
	if IsRootDocCandidate("README.md", rdWithClass) {
		t.Error("a name a root doc cannot have must not be a candidate")
	}
	if !IsRootDocCandidate("paper.tex", "  \\documentclass{article}\n") {
		t.Error("an indented document class at a line start must match")
	}
	if IsRootDocCandidate("paper.tex", "% \\documentclass{article}\n") {
		t.Error("a commented-out document class must not match")
	}
	if IsRootDocCandidate("paper.tex", "\\input{\\documentclass{article}}\n") {
		t.Error("\\documentclass not at a line start must not match")
	}
	buried := strings.Repeat("x\n", 20000) + rdWithClass
	if IsRootDocCandidate("paper.tex", buried) {
		t.Error("a declaration past the scanned prefix must not match")
	}
}

func TestRD_ChooseRootDoc_OnlyCandidate(t *testing.T) {
	s := rdSnapshot(t, map[string]map[string]any{
		"notes.tex": {"content": rdWithoutClass},
		"paper.tex": {"content": rdWithClass},
	})
	chosen, err := ChooseRootDoc(s, []string{"paper.tex"})
	if err != nil {
		t.Fatalf("ChooseRootDoc: %v", err)
	}
	if chosen == nil || chosen.Pathname != "paper.tex" {
		t.Fatalf("chosen = %+v, want pathname paper.tex", chosen)
	}
	rdSameOpsRaw(t, "ops", chosen.Operations, []map[string]any{
		{"pathname": "paper.tex", "metadata": map[string]any{"main": true}},
	})
}

func TestRD_ChooseRootDoc_PrefersBesideProject(t *testing.T) {
	s := rdSnapshot(t, map[string]map[string]any{
		"chapters/one.tex": {"content": rdWithClass},
		"main.tex":         {"content": rdWithClass},
	})
	chosen, err := ChooseRootDoc(s, []string{"chapters/one.tex", "main.tex"})
	if err != nil {
		t.Fatalf("ChooseRootDoc: %v", err)
	}
	if chosen == nil || chosen.Pathname != "main.tex" {
		t.Fatalf("chosen = %+v, want pathname main.tex", chosen)
	}
}

func TestRD_ChooseRootDoc_OrderIndependent(t *testing.T) {
	s := rdSnapshot(t, map[string]map[string]any{
		"a/deep.tex": {"content": rdWithClass},
		"b/deep.tex": {"content": rdWithClass},
	})
	ab, err1 := ChooseRootDoc(s, []string{"a/deep.tex", "b/deep.tex"})
	ba, err2 := ChooseRootDoc(s, []string{"b/deep.tex", "a/deep.tex"})
	if err1 != nil || err2 != nil {
		t.Fatalf("ChooseRootDoc: %v / %v", err1, err2)
	}
	if ab == nil || ba == nil || ab.Pathname != ba.Pathname {
		t.Fatalf("order-dependent choice: %v / %v", ab, ba)
	}
}

func TestRD_ChooseRootDoc_AlreadyRecorded(t *testing.T) {
	s := rdSnapshot(t, map[string]map[string]any{
		"chosen.tex": {"content": rdWithoutClass, "metadata": map[string]any{"main": true}},
		"paper.tex":  {"content": rdWithClass},
	})
	chosen, err := ChooseRootDoc(s, []string{"paper.tex"})
	if err != nil {
		t.Fatalf("ChooseRootDoc: %v", err)
	}
	if chosen != nil {
		t.Fatalf("must leave an already-recorded root doc alone, got %+v", chosen)
	}
}

func TestRD_ChooseRootDoc_NothingToCompile(t *testing.T) {
	s := rdSnapshot(t, map[string]map[string]any{
		"notes.tex": {"content": rdWithoutClass},
	})
	chosen, err := ChooseRootDoc(s, []string{})
	if err != nil {
		t.Fatalf("ChooseRootDoc: %v", err)
	}
	if chosen != nil {
		t.Fatalf("a project with nothing to compile must give null, got %+v", chosen)
	}
}

func TestRD_ChooseRootDoc_DropsUnknown(t *testing.T) {
	s := rdSnapshot(t, map[string]map[string]any{
		"main.tex": {"content": rdWithClass},
	})
	chosen, err := ChooseRootDoc(s, []string{"dropped.tex", "main.tex"})
	if err != nil {
		t.Fatalf("ChooseRootDoc: %v", err)
	}
	if chosen == nil || chosen.Pathname != "main.tex" {
		t.Fatalf("chosen = %+v, want main.tex", chosen)
	}
	dropped, err := ChooseRootDoc(s, []string{"dropped.tex"})
	if err != nil {
		t.Fatalf("ChooseRootDoc: %v", err)
	}
	if dropped != nil {
		t.Fatalf("a doc the project does not have must be dropped, got %+v", dropped)
	}
}

func TestRD_SetMainPathname_MovesRecord(t *testing.T) {
	s := rdSnapshot(t, map[string]map[string]any{
		"old.tex": {"content": rdWithClass, "metadata": map[string]any{"main": true}},
		"new.tex": {"content": rdWithClass},
	})
	ops, err := SetMainPathnameOperations(s, "new.tex")
	if err != nil {
		t.Fatalf("SetMainPathnameOperations: %v", err)
	}
	rdSameOpsRaw(t, "ops", ops, []map[string]any{
		{"pathname": "new.tex", "metadata": map[string]any{"main": true}},
		{"pathname": "old.tex", "metadata": map[string]any{}},
	})
}

func TestRD_SetMainPathname_NothingToDo(t *testing.T) {
	s := rdSnapshot(t, map[string]map[string]any{
		"main.tex": {"content": rdWithClass, "metadata": map[string]any{"main": true}},
	})
	ops, err := SetMainPathnameOperations(s, "main.tex")
	if err != nil {
		t.Fatalf("SetMainPathnameOperations: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("no-op expected, got %d ops", len(ops))
	}
}

func TestRD_SetMainPathname_KeepsOtherFlags(t *testing.T) {
	s := rdSnapshot(t, map[string]map[string]any{
		"old.tex": {"content": rdWithClass, "metadata": map[string]any{"main": true, "mainBibliography": true}},
		"new.tex": {"content": rdWithClass, "metadata": map[string]any{"mainBibliography": true}},
	})
	ops, err := SetMainPathnameOperations(s, "new.tex")
	if err != nil {
		t.Fatalf("SetMainPathnameOperations: %v", err)
	}
	rdSameOpsRaw(t, "ops", ops, []map[string]any{
		{"pathname": "new.tex", "metadata": map[string]any{"main": true, "mainBibliography": true}},
		{"pathname": "old.tex", "metadata": map[string]any{"mainBibliography": true}},
	})
}

func TestRD_SetMainPathname_RefusesNonDoc(t *testing.T) {
	s := rdSnapshot(t, map[string]map[string]any{
		"linked.tex": {"content": rdWithClass, "metadata": map[string]any{
			"provider": "project_file", "source_entity_path": "/main.tex", "source_project_id": "p",
		}},
	})
	if _, err := SetMainPathnameOperations(s, "linked.tex"); err == nil {
		t.Fatal("must refuse a file carrying other metadata")
	} else if !strings.Contains(err.Error(), "only a doc can be the root doc") {
		t.Fatalf("wrong error: %v", err)
	}
}
