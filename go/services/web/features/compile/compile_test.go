package compile

import (
	"encoding/json"
	"regexp"
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// Node res.json omits undefined keys and preserves explicit null/[] —
// the branch shapes are byte-exact contract (oracles pinned 2026-09-15).
func TestRespShapes(t *testing.T) {
	// 1) too-recently-compiled — outputFiles [] present, archive EXPLICIT null,
	//    NO compileGroup/compiler (limits not fetched at that branch).
	recent := &resp{Status: "too-recently-compiled", OutputFiles: &[]ofOut{}}
	if got, want := mustJSON(t, recent),
		`{"status":"too-recently-compiled","outputFiles":[],"outputFilesArchive":null}`; got != want {
		t.Fatalf("recent shape:\n got %s\nwant %s", got, want)
	}

	// 2) validation-problems (no root doc) — NO limits keys.
	vp := &resp{Status: "validation-problems", OutputFiles: &[]ofOut{},
		ValidationProblems: json.RawMessage(`{"mainFile":"no main file specified"}`)}
	if got, want := mustJSON(t, vp),
		`{"status":"validation-problems","outputFiles":[],"outputFilesArchive":null,"validationProblems":{"mainFile":"no main file specified"}}`; got != want {
		t.Fatalf("validation shape:\n got %s\nwant %s", got, want)
	}

	// 3) clsi error-mapped branch — outputFiles [] present + limits present.
	mapErr := &resp{Status: "project-too-large", OutputFiles: &[]ofOut{},
		CompileGroup: "standard", Compiler: "pdflatex"}
	if got, want := mustJSON(t, mapErr),
		`{"status":"project-too-large","outputFiles":[],"outputFilesArchive":null,"compileGroup":"standard","compiler":"pdflatex"}`; got != want {
		t.Fatalf("mapped-error shape:\n got %s\nwant %s", got, want)
	}

	// 4) success (oracle shape; createdAt pinned for determinism).
	now := "2026-09-15T22:27:04.363Z"
	sz := float64(40459)
	success := &resp{
		Status: "success",
		OutputFiles: &[]ofOut{
			{Path: "output.aux", URL: "/project/6aa9c634e98b0fb9dba6b566/user/6aa4b8b573ef0e5094f4cbc0/build/b1/output/output.aux", Type: "aux", Build: "b1"},
			{Path: "output.pdf", URL: "/project/6aa9c634e98b0fb9dba6b566/user/6aa4b8b573ef0e5094f4cbc0/build/b1/output/output.pdf", Type: "pdf", Build: "b1", Ranges: json.RawMessage("[]"), Size: &sz, CreatedAt: &now},
		},
		Archive:      &ofArchive{Path: "output.zip", URL: "/project/6aa9c634e98b0fb9dba6b566/user/6aa4b8b573ef0e5094f4cbc0/build/b1/output/output.zip", Type: "zip"},
		CompileGroup: "standard",
		Compiler:     "pdflatex",
		Stats:        json.RawMessage(`{"isInitialCompile":1,"latex-runs":2,"pdf-size":40459}`),
		Timings:      json.RawMessage(`{"compileE2E":3875}`),
	}
	got := mustJSON(t, success)
	for _, sub := range []string{
		`"status":"success"`,
		`"outputFilesArchive":{"path":"output.zip","url":"/project/6aa9c634e98b0fb9dba6b566/user/6aa4b8b573ef0e5094f4cbc0/build/b1/output/output.zip","type":"zip"}`,
		`"compileGroup":"standard","compiler":"pdflatex"`,
		`"ranges":[],"size":40459,"createdAt":"2026-09-15T22:27:04.363Z"`,
		`"stats":{"isInitialCompile":1,"latex-runs":2,"pdf-size":40459}`,
		`"timings":{"compileE2E":3875}`,
	} {
		if !stringsContains(got, sub) {
			t.Fatalf("success missing %s:\n%s", sub, got)
		}
	}
	// key order pin: status first, outputFiles before archive, limits after archive.
	iStatus := stringsIndex(got, `"status"`)
	iFiles := stringsIndex(got, `"outputFiles"`)
	iArc := stringsIndex(got, `"outputFilesArchive"`)
	iGroup := stringsIndex(got, `"compileGroup"`)
	if !(iStatus < iFiles && iFiles < iArc && iArc < iGroup) {
		t.Fatalf("key order wrong:\n%s", got)
	}
	// the normal (non-pdf) file must NOT carry ranges/size/createdAt
	if iPDF := stringsIndex(got, `"output.pdf"`); iPDF > 0 {
		first := got[:iPDF]
		for _, bad := range []string{`"ranges"`, `"size"`, `"createdAt"`} {
			if stringsContains(first, bad) {
				t.Fatalf("non-pdf file leaked %s:\n%s", bad, first)
			}
		}
	} else {
		t.Fatalf("no output.pdf entry found:\n%s", got)
	}
	// the pdf file must carry explicit ranges (Node: file.ranges || [])
	if !stringsContains(got, `"ranges":[]`) {
		t.Fatalf("pdf file missing explicit ranges:\n%s", got)
	}
}

func stringsContains(haystack, needle string) bool {
	return stringsContainsFunc(haystack, needle)
}

func stringsIndex(haystack, needle string) int {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return -1
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func stringsContainsFunc(haystack, needle string) bool {
	return stringsIndex(haystack, needle) >= 0
}

func TestOutputPathname(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://clsi:3013/project/abc/output/output.pdf", "/project/abc/output/output.pdf"},
		{"http://127.0.0.1:3013/project/x/user/y/build/z/output/output.aux?sig=1", "/project/x/user/y/build/z/output/output.aux"},
		{"not a url with spaces", "not a url with spaces"},
	}
	for _, c := range cases {
		if got := outputPathname(c.in); got != c.want {
			t.Fatalf("pathname(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestGenBuildIdFormat(t *testing.T) {
	bid := genBuildId()
	if !regexp.MustCompile(`^[0-9a-f]{8,13}-[0-9a-f]{16}$`).MatchString(bid) {
		t.Fatalf("buildId format: %q", bid)
	}
	if genBuildId() == bid {
		t.Fatal("buildId must be unique per call")
	}
}

func TestRespOmitSemantics(t *testing.T) {
	// outputUrlPrefix is a PRESENT EMPTY STRING in clsi's response and must
	// be serialized through (Node passes compile.outputUrlPrefix verbatim —
	// observed live 2026-09-15: "outputUrlPrefix":""), while a MISSING
	// field must stay omitted. clsiServerId/clsiCacheShard same rule.
	ep := ""
	got := mustJSON(t, &resp{Status: "success", OutputFiles: &[]ofOut{}, OutputURLPrefix: &ep})
	if !stringsContains(got, `"outputUrlPrefix":""`) {
		t.Fatalf("present-empty outputUrlPrefix must serialize:\n%s", got)
	}
	nil := &resp{Status: "success", OutputFiles: &[]ofOut{}}
	if got := mustJSON(t, nil); stringsContains(got, `"outputUrlPrefix"`) {
		t.Fatalf("absent outputUrlPrefix must be omitted:\n%s", got)
	}
	// clsiServerId absent by default
	if stringsContains(mustJSON(t, &resp{Status: "success", OutputFiles: &[]ofOut{}}), `"clsiServerId"`) {
		t.Fatal("clsiServerId must be omitted when unset")
	}
}

func TestComputeLimitsDefaults(t *testing.T) {
	// no owner features → standard / 180s / free; raw compiler echoed verbatim
	lim := computeLimits(nil, "pdflatex")
	if lim.CompileGroup != "standard" || lim.Timeout != 180 || lim.BackendClass != "free" || lim.CompilerRaw != "pdflatex" {
		t.Fatalf("defaults: %+v", lim)
	}
	// alpha user → alpha group → premium backend
	ownerAlpha := primitive.D{{Key: "alphaProgram", Value: true}}
	lim = computeLimits(&ownerAlpha, "")
	if lim.CompileGroup != "alpha" || lim.BackendClass != "premium" {
		t.Fatalf("alpha: %+v", lim)
	}
	// owner features override
	ownerFeatures := primitive.D{{Key: "features", Value: primitive.D{
		{Key: "compileGroup", Value: "standard"},
		{Key: "compileTimeout", Value: int32(100)},
	}}}
	lim = computeLimits(&ownerFeatures, "xelatex")
	if lim.Timeout != 100 || lim.CompilerRaw != "xelatex" {
		t.Fatalf("features: %+v", lim)
	}
	// empty project compiler ⇒ response omits it ("" + omitempty)
	if got := computeLimits(nil, "").CompilerRaw; got != "" {
		t.Fatalf("empty compiler must stay empty: %q", got)
	}
}

func fakeProject() primitive.D {
	d1 := primitive.NewObjectID()
	d2 := primitive.NewObjectID()
	d3 := primitive.NewObjectID()
	return primitive.D{
		{Key: "_id", Value: primitive.NewObjectID()},
		{Key: "compiler", Value: "pdflatex"},
		{Key: "overleaf", Value: primitive.D{{Key: "history", Value: primitive.D{{Key: "id", Value: "hid123"}}}}},
		{Key: "rootDoc_id", Value: d1},
		{Key: "owner_ref", Value: primitive.NewObjectID()},
		{Key: "rootFolder", Value: primitive.A{
			primitive.D{
				{Key: "_id", Value: primitive.NewObjectID()},
				{Key: "name", Value: "rootFolder"},
				{Key: "docs", Value: primitive.A{
					primitive.D{{Key: "_id", Value: d1}, {Key: "name", Value: "main.tex"}},
					primitive.D{{Key: "_id", Value: d2}, {Key: "name", Value: "notes.md"}},
				}},
				{Key: "fileRefs", Value: primitive.A{
					primitive.D{
						{Key: "_id", Value: primitive.NewObjectID()},
						{Key: "name", Value: "frog.jpg"},
						{Key: "hash", Value: "h1"},
						{Key: "created", Value: primitive.DateTime(1726422000000)},
					},
				}},
				{Key: "folders", Value: primitive.A{
					primitive.D{
						{Key: "_id", Value: primitive.NewObjectID()},
						{Key: "name", Value: "src"},
						{Key: "docs", Value: primitive.A{
							primitive.D{{Key: "_id", Value: d3}, {Key: "name", Value: "chap.tex"}},
						}},
					},
				}},
			},
		}},
	}
}

func TestWalk(t *testing.T) {
	p := fakeProject()
	var root primitive.A
	for _, e := range p {
		if e.Key == "rootFolder" {
			root = e.Value.(primitive.A)
		}
	}
	docs, files := walk(root)
	if len(docs) != 3 {
		t.Fatalf("docs: %+v", docs)
	}
	paths := map[string]bool{}
	for _, d := range docs {
		paths[d.Path] = true
	}
	// ROOT docs are BARE (Node seeds rootFolder[0] at base ''), subfolder
	// docs carry 'src/...' — no root-folder name prefix.
	if !paths["main.tex"] || !paths["notes.md"] || !paths["src/chap.tex"] {
		t.Fatalf("doc paths: %+v", docs)
	}
	if len(files) != 1 || files[0].Path != "frog.jpg" || files[0].Hash != "h1" || files[0].Created != 1726422000000 {
		t.Fatalf("files: %+v", files)
	}
}

func TestCanRead(t *testing.T) {
	owner := primitive.NewObjectID()
	uid := "6aa4b8b573ef0e5094f4cbc0"

	uidOID, _ := primitive.ObjectIDFromHex(uid)

	cases := []struct {
		name string
		doc  primitive.D
		ok   bool
	}{
		{"owner", primitive.D{
			{Key: "owner_ref", Value: uidOID},
		}, true},
		{"collab", primitive.D{
			{Key: "owner_ref", Value: owner},
			{Key: "collaberator_refs", Value: primitive.A{uidOID}},
		}, true},
		{"readOnly ref", primitive.D{
			{Key: "owner_ref", Value: owner},
			{Key: "readOnly_refs", Value: primitive.A{uidOID}},
		}, true},
		{"public readOnly", primitive.D{
			{Key: "owner_ref", Value: owner},
			{Key: "publicAccesLevel", Value: "readOnly"},
		}, true},
		{"public readAndWrite", primitive.D{
			{Key: "owner_ref", Value: owner},
			{Key: "publicAccesLevel", Value: "readAndWrite"},
		}, true},
		{"token but not tokenBased", primitive.D{
			{Key: "owner_ref", Value: owner},
			{Key: "publicAccesLevel", Value: "off"},
			{Key: "tokenAccessReadOnly_refs", Value: primitive.A{uidOID}},
		}, false},
		{"token tokenBased", primitive.D{
			{Key: "owner_ref", Value: owner},
			{Key: "publicAccesLevel", Value: "tokenBased"},
			{Key: "tokenAccessReadOnly_refs", Value: primitive.A{uidOID}},
		}, true},
		{"stranger private", primitive.D{
			{Key: "owner_ref", Value: owner},
		}, false},
	}
	for _, c := range cases {
		if got := canRead(c.doc, uid, false); got != c.ok {
			t.Fatalf("%s: canRead=%v want %v", c.name, got, c.ok)
		}
	}
	// site admin always reads
	if !canRead(primitive.D{{Key: "owner_ref", Value: owner}}, uid, true) {
		t.Fatal("admin read should pass")
	}
}
