// P4.13b — new-project zip upload (POST /project/new/upload).
//
// Node oracle (pinned 2026-09-15):
//
//	UploadsRouter.mjs            route wiring + multer.single('qqfile') +
//	                             multerErrorHandler
//	ProjectUploadController.mjs  uploadProject: err → 422 InvalidZipFamily
//	                             (i18n message) | 500 {'Upload failed'};
//	                             ok → 200 {"success":true,"project_id"}
//	ProjectUploadManager.mjs     _extractZip → findRootDocFileFromDirectory
//	                             → getTitleFromTexContent || name
//	                             → _generateUniqueName (fixProjectName +
//	                             ensureNameIsUnique) → createBlankProject
//	                             → _initializeProjectWithZipContents
//	                             (topLevelDir, importDir, create entries
//	                             concurrent, createNewFolderStructure,
//	                             DU updateProjectStructure, TPDS no-op)
//	                             → setRootDocFromName → fail-cleanup
//	                             (ProjectDeleter ZIP_IMPORT_FAILURE)
//	ArchiveManager.mjs           entryCount/size/count guards, ignore
//	                             matcher, bad-path skip, empty detection
//	FileTypeManager.mjs          shouldIgnore (minimatch-verified) — ported
//	                             below; getType REUSED via upClassify (P4.13a)
//	FileSystemImportManager.mjs  importDir/readdir-order walk + importFile
//	FolderStructureBuilder.mjs   docEntries first then fileEntries, folders
//	                             created in first-reference order
//	ProjectRootDocManager.mjs    findRootDocFileFromDirectory (glob
//	                             tex|Rtex|Rnw case-insens, depth/main.tex/
//	                             size/path sort, first \documentclass, else
//	                             first root-folder file) + setRootDocFromName
//	                             (full path then basename, docs tree order)
//	ProjectDetailsHandler.mjs    fixProjectName + generateUniqueName
//	ProjectHelper.mjs            ensureNameIsUnique (numeric suffix +
//	                             likely-a-year guard)
//	ProjectEntityMongoUpdateHandler createNewFolderStructure (guarded
//	                             $set rootFolder + $inc version)
//
// Contract (Node oracles captured live 2026-09-15):
//   - anon POST                → CSRF 403 'Forbidden' text (global csrf first)
//   - no file part             → 400 {"success":false,"error":"invalid_upload_request"}
//   - file > 50MiB             → 422 {"success":false,"error":"File too large"}
//   - unexpected file field    → 500 generic HTML page (LIMIT_UNEXPECTED_FILE)
//   - bad zip / zero-entry     → 422 {"success":false,"error":"Invalid zip file"}
//   - dir-only zip (files>0 written=0) → 422 ...error":"Zip doesn’t contain any file"
//   - >300MiB uncomp or >2000 entities or entryCount>20000
//                             → 422 {"success":false,"error":"Zip contents too large"}
//   - import failure after project create
//                             → 500 {"success":false,"error":"Upload failed"}
//                               + project gone + deletedProjects record
//                               deleterData.deletedReason 'zip-import-failure'
//                                 (NO deleterId / deleterIpAddress fields)
//   - success                  → 200 {"success":true,"project_id":"<24hex>"}
//
// Zip → project mapping (pinned):
//   - zip entries: skip FileIgnorePattern matches, dir entries, bad paths
//     ('..' / absolute / non-normalized); keep original entry ORDER.
//   - single top-level dir (no root files, exactly one child dir) is stripped.
//   - project name: zip \title (detex, first line ≤30000 chars) || form name;
//     form name = basename(name,'.zip'); fixProjectName (trim/Untitled/- for
//     //150) then ensureNameIsUnique against ALL of the user's project names.
//   - root doc: candidates tex|rtex|rnw (case-insens.) sorted by
//     (depth asc, main.tex first, size asc, path asc); first with a line
//     ^\s*\\documentclass wins, else the first root-folder candidate; then
//     setRootDocFromName (full path then basename) → $set rootDoc_id.
//   - docs: docstore PUT rev 0 (lines); files: v1-history blob PUT (git blob
//     sha1); then guarded $set rootFolder + $inc version (→1); then DU
//     add-doc ops (joined docLines) then add-file ops, version 1, source null.

package projectlist

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var nzipPat = regexp.MustCompile(`^/project/new/upload$`)

const (
	nzipMaxUpload     = 50 * 1024 * 1024  // Settings.maxUploadSize (env unset)
	nzipUncompMax     = 300 * 1024 * 1024 // 6 * maxUploadSize
	nzipMaxEntities   = 2000              // Settings.maxEntitiesPerProject
	nzipMaxEntryCount = 20000             // maxEntitiesPerProject * 10
	nzipNameMaxLen    = 150
)

// ---------- intake -------------------------------------------------------------

// nzipMultipart parses the single 'qqfile' part with Node's 50MiB
// fileSize limit. kind: 0 ok, 1 no-file (400), 2 file-too-large (422),
// 3 unexpected (500).
func nzipMultipart(cxt *core.Cxt) (data []byte, formName string, kind int) {
	req := cxt.Req
	if err := req.ParseMultipartForm(upMultipartCap + nzipMaxUpload); err != nil {
		return nil, "", 1
	}
	mf := req.MultipartForm
	if mf == nil || len(mf.File) == 0 {
		return nil, "", 1
	}
	var fileHdr *multipart.FileHeader
	for fname, hdrs := range mf.File {
		if len(hdrs) != 1 || fname != "qqfile" {
			return nil, "", 3 // LIMIT_UNEXPECTED_FILE → 500 page (Node)
		}
		fileHdr = hdrs[0]
	}
	if n, _ := mf.Value["name"]; len(n) > 0 {
		formName = n[0]
	}
	ff, oerr := fileHdr.Open()
	if oerr != nil {
		return nil, "", 3
	}
	defer ff.Close()
	data = make([]byte, 0, 1<<20)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := ff.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
			if len(data) > nzipMaxUpload {
				return nil, "", 2 // multer LIMIT_FILE_SIZE → 422 (read stopped)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, "", 3
		}
	}
	return data, formName, 0
}

// ---------- zip intake (ArchiveManager port) -----------------------------------

var nzipIgnoreDirs = []string{"__macosx", ".git", ".texpadtmp", ".r", ".venv", "venv"}
var nzipIgnoreExts = []string{
	".dvi", ".aux", ".log", ".toc", ".out", ".pdfsync", ".synctex",
	".synctex(busy)", ".fdb_latexmk", ".fls", ".nlo", ".ind", ".glo",
	".gls", ".glg", ".bbl", ".blg", ".doc", ".docx", ".gz", ".swp",
}

// nzipIgnore — FileTypeManager.shouldIgnore with the CE fileIgnorePattern
// (verified against live minimatch 2026-09-15): component dirs anywhere
// (+descendants), hidden files except .latexmkrc, ext-suffix list — all
// case-insensitive.
func nzipIgnore(entryName string) bool {
	lc := strings.ToLower(entryName)
	for _, p := range strings.Split(lc, "/") {
		for _, d := range nzipIgnoreDirs {
			if p == d {
				return true
			}
		}
	}
	parts := strings.Split(lc, "/")
	base := parts[len(parts)-1]
	if strings.HasPrefix(base, ".") && base != ".latexmkrc" {
		return true
	}
	for _, e := range nzipIgnoreExts {
		if strings.HasSuffix(lc, e) {
			return true
		}
	}
	return false
}

// nzipSafePath — ArchiveManager._checkFilePath (false for directory entries
// and skipped bad paths: '..' / absolute / non-normalized).
func nzipSafePath(entryName string) (string, bool) {
	t := strings.ReplaceAll(entryName, "\\", "/")
	if strings.HasSuffix(t, "/") {
		return "", false
	}
	for _, dir := range strings.Split(t, "/") {
		if dir == ".." || dir == "" {
			return "", false
		}
	}
	return t, true
}

type nzipEntry struct {
	path string // project-relative, without leading '/'
	data []byte
}

// nzipReadEntry — Part B B2: read one zip entry's bytes with a hard ceiling
// on ACTUAL consumption. The declared-size pre-check bounds `total`, but a
// data-descriptor (bit-3) zip's real stream can be larger than its declared
// sizes, so the read itself must be bounded; overflow reports back instead
// of silently growing memory.
func nzipReadEntry(rc io.Reader, max int64) (b []byte, overflow bool) {
	r := io.LimitReader(rc, max+1)
	d, err := io.ReadAll(r)
	if err != nil {
		return nil, false
	}
	if int64(len(d)) > max {
		return nil, true
	}
	return d, false
}

// nzipExtract — _isZipTooLarge + _extractZipFiles semantics.
// kind: 0 ok | 1 invalid_zip | 2 zip_contents_too_large | 3 empty_zip.
func nzipExtract(r *zip.Reader) ([]nzipEntry, int) {
	if len(r.File) > nzipMaxEntryCount {
		return nil, 2
	}
	seen := 0
	var total int64
	for _, f := range r.File {
		if nzipIgnore(f.Name) {
			continue
		}
		total += int64(f.UncompressedSize64)
		seen++
		if total > nzipUncompMax {
			return nil, 2
		}
		if seen > nzipMaxEntities {
			return nil, 2
		}
	}
	if seen == 0 {
		// Node: totalSizeInBytes stays null → NaN → InvalidZipFileError
		return nil, 1
	}
	var out []nzipEntry
	var actual int64 // Part B B2: ACTUAL consumed bytes (declared sizes can lie)
	for _, f := range r.File {
		if nzipIgnore(f.Name) {
			continue
		}
		rel, ok := nzipSafePath(f.Name)
		if !ok {
			continue // skip (directory or bad path)
		}
		rc, oerr := f.Open()
		if oerr != nil {
			return nil, 1
		}
		data, over := nzipReadEntry(rc, nzipUncompMax)
		rc.Close()
		if over {
			return nil, 2 // Part B B2: actual stream exceeded the budget (declared sizes can lie — data-descriptor zips)
		}
		actual += int64(len(data))
		if actual > nzipUncompMax {
			return nil, 2 // Part B B2: cumulative ACTUAL bytes bounded, not just declared
		}
		out = append(out, nzipEntry{path: rel, data: data})
	}
	if len(out) == 0 {
		return nil, 3 // EmptyZipFileError
	}
	return out, 0
}

// nzipTopLevel — findTopLevelDirectory: exactly one child and it is a
// directory (no root-level files) → strip that prefix.
func nzipTopLevel(entries []nzipEntry) string {
	top := map[string]bool{}
	rootFile := false
	for _, e := range entries {
		i := strings.Index(e.path, "/")
		if i < 0 {
			rootFile = true
			continue
		}
		top[e.path[:i]] = true
	}
	if !rootFile && len(top) == 1 {
		for k := range top {
			return k
		}
	}
	return ""
}

// ---------- root-doc + title (ProjectRootDocManager + DocumentHelper) ----------

var (
	nzipDocclassRX = regexp.MustCompile(`^\s*\\documentclass`)
	nzipTitleCur   = regexp.MustCompile(`\\[tT]itle\*?\s*\{([^}]+)\}`)
	nzipTitleSq    = regexp.MustCompile(`\\[tT]itle\s*\[([^\]]+)\]`)
	nzipSpacing    = regexp.MustCompile(`\\\[[A-Za-z0-9. ]*\]`)
	nzipCmdRX      = regexp.MustCompile(`\\(?:[a-zA-Z]+|.|)`)
	nzipMultiRX    = regexp.MustCompile(` +`)
)

// nzipDetex — DocumentHelper.detex (order-preserving replaces).
func nzipDetex(s string) string {
	s = strings.ReplaceAll(s, `\LaTeX`, "LaTeX")
	s = strings.ReplaceAll(s, `\TeX`, "TeX")
	s = strings.ReplaceAll(s, `\TikZ`, "TikZ")
	s = strings.ReplaceAll(s, `\BibTeX`, "BibTeX")
	s = nzipSpacing.ReplaceAllString(s, " ")
	s = nzipCmdRX.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "{}", " ")
	s = strings.ReplaceAll(s, "~", " ")
	s = strings.Map(func(r rune) rune {
		if r == '$' || r == '{' || r == '}' {
			return -1
		}
		return r
	}, s)
	s = nzipMultiRX.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// nzipTitle — getTitleFromTexContent (≤30000 chars, split '\n', first line
// matching curly-then-square \title).
func nzipTitle(content string) string {
	r := []rune(content)
	if len(r) > 30000 {
		r = r[:30000]
	}
	for _, line := range strings.Split(string(r), "\n") {
		if m := nzipTitleCur.FindStringSubmatch(line); m != nil {
			return nzipDetex(m[1])
		}
		if m := nzipTitleSq.FindStringSubmatch(line); m != nil {
			return nzipDetex(m[1])
		}
	}
	return ""
}

func nzipDocclass(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if nzipDocclassRX.MatchString(line) {
			return true
		}
	}
	return false
}

func nzipBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// nzipRootDocExt — the old-path glob is tex|Rtex|Rnw, case-insensitive.
func nzipRootDocExt(base string) bool {
	e := ""
	if i := strings.LastIndex(base, "."); i >= 0 {
		e = base[i:]
	}
	switch strings.ToLower(e) {
	case ".tex", ".rtex", ".rnw":
		return true
	}
	return false
}

// nzipFindRootDoc — findRootDocFileFromDirectory: sort candidates by
// (depth, main.tex, size, path); first with \documentclass wins, else first
// root-folder candidate. Returns (relPath, content(norm), found).
func nzipFindRootDoc(entries []nzipEntry) (string, string, bool) {
	type cand struct {
		rel   string
		depth int
		size  int
		name  string
	}
	var cs []cand
	for _, e := range entries {
		base := nzipBase(e.path)
		if !nzipRootDocExt(base) {
			continue
		}
		cs = append(cs, cand{rel: e.path, depth: strings.Count(e.path, "/"), size: len(e.data), name: base})
	}
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.depth != b.depth {
			return a.depth < b.depth
		}
		if (a.name == "main.tex") != (b.name == "main.tex") {
			return a.name == "main.tex"
		}
		if a.size != b.size {
			return a.size < b.size
		}
		return a.rel < b.rel
	})
	get := func(rel string) string {
		for _, e := range entries {
			if e.path == rel {
				return string(e.data)
			}
		}
		return ""
	}
	rootDepth := 0
	firstRoot := -1
	for i, c := range cs {
		if c.depth == rootDepth {
			if firstRoot < 0 {
				firstRoot = i
			}
			break
		}
	}
	for _, c := range cs {
		norm := strings.ReplaceAll(get(c.rel), "\r", "")
		if nzipDocclass(norm) {
			return c.rel, norm, true
		}
	}
	if firstRoot >= 0 {
		rel := cs[firstRoot].rel
		return rel, strings.ReplaceAll(get(rel), "\r", ""), true
	}
	return "", "", false
}

// ---------- names (ProjectDetailsHandler + ProjectHelper) ----------------------

// nzipFixName — fixProjectName.
func nzipFixName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Untitled"
	}
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, `\`, "")
	r := []rune(name)
	if len(r) > nzipNameMaxLen {
		name = string(r[:nzipNameMaxLen])
	}
	return strings.TrimSpace(name)
}

// nzipUserNames — findAllUsersProjects owner+member project names.
func nzipUserNames(a *core.App, cxt *core.Cxt, uid string) []string {
	if a.Mongo == nil {
		return nil
	}
	oid, err := bson.ObjectIDFromHex(uid)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil
	}
	var seen []string
	find := func(filter bson.D) {
		cur, err := db.Collection("projects").Find(ctx, filter,
			options.Find().SetProjection(bson.D{{Key: "name", Value: 1}}))
		if err != nil {
			return
		}
		defer cur.Close(ctx)
		for cur.Next(ctx) {
			var d struct {
				Name string `bson:"name"`
			}
			if cur.Decode(&d) == nil && d.Name != "" {
				seen = append(seen, d.Name)
			}
		}
	}
	find(bson.D{{Key: "owner_ref", Value: oid}})
	find(bson.D{{Key: "collaborator_refs", Value: oid}})
	find(bson.D{{Key: "reviewer_refs", Value: oid}})
	find(bson.D{{Key: "readOnly_refs", Value: oid}})
	find(bson.D{
		{Key: "tokenAccessReadAndWrite_refs", Value: oid},
		{Key: "publicAccesLevel", Value: "tokenBased"},
	})
	find(bson.D{
		{Key: "tokenAccessReadOnly_refs", Value: oid},
		{Key: "publicAccesLevel", Value: "tokenBased"},
	})
	return seen
}

var nzipSufRX = regexp.MustCompile(` \((\d+)\)$`)

// nzipEnsureUnique — ensureNameIsUnique (only the numeric-suffix path is
// reachable here: suffixes=[]).
func nzipEnsureUnique(existing []string, name string) string {
	set := map[string]bool{}
	for _, n := range existing {
		set[n] = true
	}
	if !set[name] {
		return name
	}
	base := name
	n := 1
	if m := nzipSufRX.FindStringSubmatch(name); m != nil {
		base = name[:len(name)-len("("+m[1]+")")]
		base = strings.TrimSuffix(base, " ")
		n, _ = strconv.Atoi(m[1])
	}
	prefixRX := regexp.MustCompile(`^` + regexp.QuoteMeta(base) + ` \(\d+\)$`)
	samePrefix := 0
	for nn := range set {
		if prefixRX.MatchString(nn) {
			samePrefix++
		}
	}
	if n > 1000 && samePrefix < n/2 { // nIsLikelyAYear
		base = name
		n = 1
	}
	last := len(set) + n
	for n <= last {
		cand := base + " (" + strconv.Itoa(n) + ")"
		if !set[cand] {
			return cand
		}
		n++
	}
	return name
}

// ---------- tree build (FolderStructureBuilder) --------------------------------

type nzipDocE struct {
	id   bson.ObjectID
	path string // project path "/a/b.tex"
	rel  string // "a/b.tex" | "b.tex"
	name string
	ln   []string
}
type nzipFileE struct {
	id   bson.ObjectID
	path string
	rel  string
	name string
	hash string
}

type nzipFolder struct {
	id      bson.ObjectID
	name    string
	folders []nzipFolder
	docs    []bson.D
	files   []bson.D
}

// nzipBuildTree — FolderStructureBuilder: doc entries first (insertion
// order), then file entries; folders created in first-reference order.
func nzipBuildTree(docs []nzipDocE, files []nzipFileE) bson.D {
	root := nzipFolder{id: bson.NewObjectID(), name: "rootFolder"}
	fmap := map[string]*nzipFolder{"/": &root}

	mkdirp := func(dir string) *nzipFolder {
		key := "/" + dir
		if f, ok := fmap[key]; ok {
			return f
		}
		// create the ancestor chain (non-recursive)
		cur := &root
		acc := ""
		for _, part := range strings.Split(dir, "/") {
			acc2 := acc + "/" + part
			if f, ok := fmap[acc2]; ok {
				cur = f
				acc = acc2
				continue
			}
			f := &nzipFolder{id: bson.NewObjectID(), name: part}
			cur.folders = append(cur.folders, *f)
			fmap[acc2] = f
			cur = f
			acc = acc2
		}
		return cur
	}

	for _, de := range docs {
		var dir string
		if i := strings.LastIndex(de.rel, "/"); i >= 0 {
			dir = de.rel[:i]
		}
		f := mkdirp(dir)
		f.docs = append(f.docs, bson.D{
			{Key: "_id", Value: de.id},
			{Key: "name", Value: de.name},
		})
	}
	for _, fe := range files {
		var dir string
		if i := strings.LastIndex(fe.rel, "/"); i >= 0 {
			dir = fe.rel[:i]
		}
		f := mkdirp(dir)
		f.files = append(f.files, bson.D{
			{Key: "_id", Value: fe.id},
			{Key: "name", Value: fe.name},
			{Key: "created", Value: time.Now().UTC()},
			{Key: "rev", Value: 0},
			{Key: "hash", Value: fe.hash},
		})
	}
	var render func(f *nzipFolder) bson.D
	render = func(f *nzipFolder) bson.D {
		folders := make([]bson.D, 0, len(f.folders))
		for i := range f.folders {
			folders = append(folders, render(&f.folders[i]))
		}
		docs := f.docs
		if docs == nil {
			docs = []bson.D{}
		}
		files := f.files
		if files == nil {
			files = []bson.D{}
		}
		return bson.D{
			{Key: "_id", Value: f.id},
			{Key: "name", Value: f.name},
			{Key: "folders", Value: folders},
			{Key: "docs", Value: docs},
			{Key: "fileRefs", Value: files},
		}
	}
	return render(&root)
}

// ---------- mongo writes --------------------------------------------------------

var nzipValidRootExt = map[string]bool{
	".tex": true, ".rtex": true, ".ltx": true, ".rnw": true, ".typ": true,
}

// nzipWriteStructure — createNewFolderStructure (guarded $set + $inc).
func nzipWriteStructure(a *core.App, cxt *core.Cxt, pj bson.ObjectID, root bson.D) bool {
	if a.Mongo == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	res, err := db.Collection("projects").UpdateOne(ctx,
		bson.D{
			{Key: "_id", Value: pj},
			{Key: "rootFolder.0.folders.0", Value: bson.D{{Key: "$exists", Value: false}}},
			{Key: "rootFolder.0.docs.0", Value: bson.D{{Key: "$exists", Value: false}}},
			{Key: "rootFolder.0.files.0", Value: bson.D{{Key: "$exists", Value: false}}},
		},
		bson.D{
			{Key: "$set", Value: bson.D{{Key: "rootFolder", Value: bson.A{root}}}},
			{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
		})
	if err != nil {
		return false
	}
	return res.MatchedCount == 1
}

// nzipSetRootDoc — setRootDocFromName + setRootDoc over the final tree.
func nzipSetRootDoc(a *core.App, cxt *core.Cxt, pj bson.ObjectID, rootDocName string) bool {
	if a.Mongo == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 6*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	var doc bson.D
	if db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: pj}}).Decode(&doc) != nil {
		return false
	}
	rootFolder, _ := dget(doc, "rootFolder").(bson.A)
	if len(rootFolder) == 0 {
		return false
	}
	root0, _ := rootFolder[0].(bson.D)

	type drec struct {
		id   bson.ObjectID
		path string
	}
	var recs []drec
	var walk func(f bson.D, prefix string)
	walk = func(f bson.D, prefix string) {
		nm, _ := dget(f, "name").(string)
		p := prefix + "/" + nm
		if docs, ok := dget(f, "docs").(bson.A); ok {
			for _, dv := range docs {
				dd, ok := dv.(bson.D)
				if !ok {
					continue
				}
				id, ok2 := dget(dd, "_id").(bson.ObjectID)
				nm2, _ := dget(dd, "name").(string)
				if ok2 {
					recs = append(recs, drec{id: id, path: p + "/" + nm2})
				}
			}
		}
		if folders, ok := dget(f, "folders").(bson.A); ok {
			for _, fv := range folders {
				fo, ok := fv.(bson.D)
				if !ok {
					continue
				}
				walk(fo, p)
			}
		}
	}
	walk(root0, "")

	want := strings.Trim(rootDocName, "'") // Node: replace(/^'|'$/g,'')
	if !strings.HasPrefix(want, "/") {
		want = "/" + want
	}
	var pick bson.ObjectID
	found := false
	for _, r := range recs {
		if r.path == want {
			pick, found = r.id, true
			break
		}
	}
	if !found {
		base := nzipBase(want)
		for _, r := range recs {
			if nzipBase(r.path) == base {
				pick, found = r.id, true
				break
			}
		}
	}
	if !found {
		return true // Node: setRootDocFromName → undefined (no error, no set)
	}
	if !nzipValidRootExt[strings.ToLower(extOf(want))] {
		return false // UnsupportedFileTypeError → 500 'Upload failed'
	}
	_, uerr := db.Collection("projects").UpdateOne(ctx, bson.D{{Key: "_id", Value: pj}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "rootDoc_id", Value: pick}}}})
	return uerr == nil
}

func extOf(p string) string {
	i := strings.LastIndex(p, ".")
	if i < 0 || i == len(p)-1 {
		return ""
	}
	return p[i:]
}

// vaEsc — JSON-escape a fragment for the 400 body (Node emits no HTML
// escaping: only quote/backslash/control chars are escaped).
func vaEsc(s string) string {
	const hexd = "0123456789abcdef"
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20:
			b.WriteString(`\\u` + string([]byte{hexd[r>>12&0xf], hexd[(r>>8)&0xf], hexd[(r>>4)&0xf], hexd[r&0xf]}))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// nzipFailCleanup — ProjectDeleter.deleteProject(ZIP_IMPORT_FAILURE): same
// side-effect chain as P4.8's deleteProjectExec, but deleterData carries
// NO deleterId/deleterIpAddress (options.deleterUser/ipAddress undefined)
// and deletedReason 'zip-import-failure'.
func nzipFailCleanup(a *core.App, cxt *core.Cxt, pj bson.ObjectID, uid, ip string) {
	pid := pj.Hex()
	hist := strings.TrimSuffix(crHistoryBase(), "/")
	ds := strings.TrimSuffix(crDocstoreBase(), "/")
	// D41 slice-2: DU project delete+flush removed (DU retired).
	fireHTTP(cxt, "POST", hist+"/project/"+pid+"/flush", nil)
	fireHTTP(cxt, "POST", ds+"/project/"+pid+"/archive", nil)

	if a.Mongo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return
	}
	var procdoc bson.D
	if db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: pj}}).Decode(&procdoc) != nil {
		return
	}
	deleterData := ddBuildDeleterData(pj, procdoc, "zip-import-failure", "", "")
	_, _ = db.Collection("deletedProjects").UpdateOne(
		ctx,
		bson.D{{Key: "deleterData.deletedProjectId", Value: pj}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "project", Value: procdoc},
			{Key: "deleterData", Value: deleterData},
			{Key: "__v", Value: 0},
		}}},
		options.UpdateOne().SetUpsert(true))
	_, _ = db.Collection("projects").DeleteOne(ctx, bson.D{{Key: "_id", Value: pj}})
}

// ---------- handler -------------------------------------------------------------

func newzipHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		reqPath := strings.TrimPrefix(cxt.Req.URL.Path, "/")
		pbase := func() views.PageData { return pageBase(cxt, reqPath) }
		fail500 := func() { views.Error500Page(res.W, pbase()) }
		failImported := func() {
			res.JSON(500, []byte(`{"success":false,"error":"Upload failed"}`))
		}

		uid := ""
		if cxt.Sess != nil {
			uid = cxt.Sess.UserIDHex()
		}
		if uid == "" {
			res.SendStatus(401)
			return
		}

		data, formName, kind := nzipMultipart(cxt)
		switch kind {
		case 1:
			res.JSON(400, []byte(upNoFile400))
			return
		case 2:
			res.JSON(422, []byte(`{"success":false,"error":"File too large"}`))
			return
		case 3:
			fail500()
			return
		}

		// parseReq VA (REQ_VALIDATION_MODE=enforce-log in this build): the
		// uploadProject body schema is z.strictObject({type?: string,
		// name: string.nonempty(), relativePath?: filepath|''}) — issue order
		// below mirrors zod (strict/unknown keys first, then field order).
		vaFail := func(issue string) {
			res.JSON(400, []byte(`{"error":"Validation error: `+vaEsc(issue)+`","statusCode":400}`))
		}
		vals := cxt.Req.MultipartForm.Value
		unknown := []string{}
		for k := range vals {
			if k != "name" && k != "type" && k != "relativePath" {
				unknown = append(unknown, k)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			vaFail(`Unrecognized key: ` + `"` + unknown[0] + `"` + ` at ` + `"body"`)
			return
		}
		nameVals, hasName := vals["name"]
		if !hasName || len(nameVals) == 0 {
			vaFail(`Invalid input: expected string, received undefined at "body.name"`)
			return
		}
		if nameVals[0] == "" {
			vaFail(`Too small: expected string to have >=1 characters at "body.name"`)
			return
		}
		if rpv, hasRP := vals["relativePath"]; hasRP {
			rp := rpv[0]
			if strings.HasPrefix(rp, "/") {
				vaFail(`Path is absolute at "body.relativePath"`)
				return
			}
			for _, seg := range strings.Split(rp, "/") {
				if seg == ".." {
					vaFail(`Path traversal detected at "body.relativePath"`)
					return
				}
			}
		}

		// Node: name = Path.basename(body.name, '.zip').
		name := formName
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if strings.HasSuffix(name, ".zip") {
			name = name[:len(name)-4]
		}

		zr, zerr := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if zerr != nil {
			res.JSON(422, []byte(`{"success":false,"error":"Invalid zip file"}`))
			return
		}
		entries, ekind := nzipExtract(zr)
		switch ekind {
		case 1:
			res.JSON(422, []byte(`{"success":false,"error":"Invalid zip file"}`))
			return
		case 2:
			res.JSON(422, []byte(`{"success":false,"error":"Zip contents too large"}`))
			return
		case 3:
			res.JSON(422, []byte(`{"success":false,"error":"Zip doesn\u2019t contain any file"}`))
			return
		}

		// strip the single top-level dir (findTopLevelDirectory)
		top := nzipTopLevel(entries)
		if top != "" {
			prefix := top + "/"
			strip := make([]nzipEntry, 0, len(entries))
			for _, e := range entries {
				if strings.HasPrefix(e.path, prefix) {
					strip = append(strip, nzipEntry{path: e.path[len(prefix):], data: e.data})
				}
			}
			entries = strip
		}

		rootRel, rootContent, hasRoot := nzipFindRootDoc(entries)
		_ = rootRel
		projectName := ""
		if hasRoot {
			projectName = nzipTitle(rootContent)
		}
		if projectName == "" {
			projectName = name
			if projectName == "" {
				projectName = "Untitled"
			}
		}
		uniqueName := nzipEnsureUnique(nzipUserNames(a, cxt, uid), nzipFixName(projectName))

		// createBlankProject (P4.7 shape; blank: no docs/files, rootDoc null,
		// version 0 — the P4.8-style structure write below then $inc → 1).
		pj := bson.NewObjectID()
		rootID := bson.NewObjectID()
		u, okU := loadOwnerUser(a, cxt, uid)
		if !okU {
			u = crOwnerUser{spellCheckLanguage: "en"}
		}
		crInsertProject(a, cxt, pj, rootID, nil, uniqueName, uid,
			u.spellCheckLanguage, "pdflatex", bson.A{}, bson.A{}, 0)
		crInitHistory(cxt, pj.Hex())

		cleanup := func() {
			nzipFailCleanup(a, cxt, pj, uid, "")
			failImported()
		}

		// import (walk order = zip entry order — Node readdir of the freshly
		// extracted linear ext4 dir matches entry order; SafePath check per
		// file as in importFile/_validateProjectPath)
		var docs []nzipDocE
		var files []nzipFileE
		for _, e := range entries {
			projPath := "/" + e.path
			if !nzipCleanPath(projPath) {
				cleanup()
				return
			}
			kind2, lines2 := upClassify(e.data, e.path, false)
			if kind2 == "doc" {
				docID := bson.NewObjectID()
				if _, okps := upDocstorePut(pj.Hex(), docID.Hex(), lines2); !okps {
					cleanup()
					return
				}
				docs = append(docs, nzipDocE{id: docID, path: projPath, rel: e.path, name: nzipBase(e.path), ln: lines2})
			} else {
				fileID := bson.NewObjectID()
				hash := upGitBlobHash(e.data)
				if !upPutBlob(pj.Hex(), hash, e.data) {
					cleanup()
					return
				}
				files = append(files, nzipFileE{id: fileID, path: projPath, rel: e.path, name: nzipBase(e.path), hash: hash})
			}
		}

		root := nzipBuildTree(docs, files)
		if !nzipWriteStructure(a, cxt, pj, root) {
			cleanup()
			return
		}

		// D41 slice-2: DU structure op removed — nzipWriteStructure (mongo)
		// above is the tree of record; DU versioning defers to the Yjs hybrid.

		if hasRoot {
			if !nzipSetRootDoc(a, cxt, pj, rootRel) {
				cleanup()
				return
			}
		}

		res.JSON(200, []byte(`{"success":true,"project_id":"`+pj.Hex()+`"}`))
	}
}

// ---------- SafePath path validation (importFile._validateProjectPath) --------

var nzipBlockedExactSet = map[string]bool{
	"prototype": true, "constructor": true, "toString": true, "toLocaleString": true,
	"valueOf": true, "hasOwnProperty": true, "isPrototypeOf": true,
	"propertyIsEnumerable": true, "__defineGetter__": true, "__lookupGetter__": true,
	"__defineSetter__": true, "__lookupSetter__": true, "__proto__": true,
}

// nzipCleanPath — SafePath.isAllowedLength (0<len≤1024) + isCleanPath (each
// element isCleanFilename; top-level element not a BLOCKEDFILE name — the
// blocked list is case-SENSITIVE).
func nzipCleanPath(path string) bool {
	if len(path) == 0 || len(path) > 1024 {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	last := parts[len(parts)-1]
	if last == "" {
		return false
	}
	for i, p := range parts {
		if p == "" {
			continue
		}
		if !entCleanName(p) {
			return false
		}
		if i == 0 && nzipBlockedExactSet[p] {
			return false
		}
	}
	return true
}

var _ = http.MethodPost
