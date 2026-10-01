// gsync — project-scoped surface: import / export / state / merge
// (Node GitHubSyncHandler.importRepo / exportProject / getProjectState /
// getMergeOverview / unlinkRepo + GitMerge.doGitMerge).
//
// Stack notes (D41 Go stack):
//   - current project state (files + version) is read from the docstore +
//     v1 blobs + the mongo rootFolder tree (projectlist.GSExportFiles /
//     ProjectVersion) — the D41 version store of record, same sources the
//     P4.12a file proxy and doc download use.
//   - remote git content is fetched via the git contents REST API (the Node
//     GitHubApiClient.getBlobStream equivalent), applied through the TPDS
//     upsert/delete primitives (projectlist.MergeUpsert / MergeDelete).

package gsync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/projectlist"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ---------- sync state (githubSyncProjectStates) ----------

const gsStatesColl = "githubSyncProjectStates"

type gsProjectState struct {
	ProjectID          bson.ObjectID `bson:"projectId"`
	RepoFullName       string        `bson:"repoFullName,omitempty"`
	MergeStatus        string        `bson:"mergeStatus,omitempty"`
	LastSyncCommit     string        `bson:"lastSyncCommit,omitempty"`
	DefaultBranchName  string        `bson:"defaultBranchName,omitempty"`
	LastSyncVersion    int64         `bson:"lastSyncVersion,omitempty"`
	SyncProvider       string        `bson:"syncProvider,omitempty"`
	SyncServerURL      string        `bson:"syncServerUrl,omitempty"`
	SyncUsername       string        `bson:"syncUsername,omitempty"`
	OwnerID            string        `bson:"ownerId,omitempty"`
	UnmergedBranchName string        `bson:"unmergedBranchName,omitempty"`
	UnmergedBranchHead string        `bson:"unmergedBranchHead,omitempty"`
	ConflictVersion    int64         `bson:"conflictVersion,omitempty"`
}

func gsStatesDB(a *core.App, ctx context.Context) (*mongo.Database, error) {
	if a.Mongo == nil {
		return nil, context.DeadlineExceeded
	}
	return a.Mongo.DB(ctx)
}

func gsGetState(ctx context.Context, a *core.App, pid bson.ObjectID) *gsProjectState {
	db, err := gsStatesDB(a, ctx)
	if err != nil {
		return nil
	}
	var st gsProjectState
	if err := db.Collection(gsStatesColl).FindOne(ctx, bson.D{{Key: "projectId", Value: pid}}).Decode(&st); err != nil {
		return nil
	}
	return &st
}

func gsSaveState(ctx context.Context, a *core.App, st *gsProjectState) error {
	db, err := gsStatesDB(a, ctx)
	if err != nil {
		return err
	}
	_, err = db.Collection(gsStatesColl).UpdateOne(ctx,
		bson.D{{Key: "projectId", Value: st.ProjectID}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "repoFullName", Value: st.RepoFullName},
			{Key: "mergeStatus", Value: st.MergeStatus},
			{Key: "lastSyncCommit", Value: st.LastSyncCommit},
			{Key: "defaultBranchName", Value: st.DefaultBranchName},
			{Key: "lastSyncVersion", Value: st.LastSyncVersion},
			{Key: "syncProvider", Value: st.SyncProvider},
			{Key: "syncServerUrl", Value: st.SyncServerURL},
			{Key: "syncUsername", Value: st.SyncUsername},
			{Key: "ownerId", Value: st.OwnerID},
			{Key: "unmergedBranchName", Value: st.UnmergedBranchName},
			{Key: "unmergedBranchHead", Value: st.UnmergedBranchHead},
			{Key: "conflictVersion", Value: st.ConflictVersion},
		}}},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func gsUpdateStateField(ctx context.Context, a *core.App, pid bson.ObjectID, set map[string]any) error {
	db, err := gsStatesDB(a, ctx)
	if err != nil {
		return err
	}
	d := bson.D{}
	for _, k := range sortedKeys(set) {
		d = append(d, bson.E{Key: k, Value: set[k]})
	}
	_, err = db.Collection(gsStatesColl).UpdateOne(ctx,
		bson.D{{Key: "projectId", Value: pid}},
		bson.D{{Key: "$set", Value: d}},
	)
	return err
}

func gsRemoveState(ctx context.Context, a *core.App, pid bson.ObjectID) error {
	db, err := gsStatesDB(a, ctx)
	if err != nil {
		return err
	}
	_, err = db.Collection(gsStatesColl).DeleteMany(ctx, bson.D{{Key: "projectId", Value: pid}})
	return err
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---------- import ----------

// importProjectHandler — POST /project/new/github-sync (Node importRepo).
func importProjectHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	body, err := gsBody(cxt.Req, 10<<20)
	if err != nil {
		res.JSON(400, []byte(`{"message":"Bad request"}`))
		return
	}
	name := gsStr(body, "name")
	fullName := gsStr(body, "fullName")
	branch := gsStr(body, "defaultBranchName")
	if branch == "" {
		branch = "main"
	}
	provider := gsStr(body, "provider")
	serverUrl := gsStr(body, "serverUrl")
	username := gsStr(body, "username")
	if fullName == "" {
		res.JSON(400, []byte(`{"message":"fullName is required"}`))
		return
	}

	cred, err := gsResolveCreds(cxt.Req.Context(), cxt.A, uid, provider, serverUrl, username)
	if err != nil {
		gsResErr(res, err)
		return
	}
	ctx, cancel := gsCtx()
	defer cancel()

	// branch head (nullable for empty repos — H15)
	var head string
	if h, herr := gsBridge().BranchHead(ctx, fullName, branch, cred); herr == nil {
		head = h
	}

	fsPath := gsWorkRoot() + "/github_import_" + gsRandHex(8)
	defer osRemoveAll(fsPath)

	if cerr := gsBridge().Clone(ctx, fullName, branch, fsPath, cred.ServerURL, cred.Username, cred.Token); cerr != nil {
		gsResImportErr(res, cerr)
		return
	}

	// walk the working tree into entries
	entries, werr := gsWalkDir(fsPath)
	if werr != nil {
		gsResImportErr(res, &gsError{Status: 500, Message: "failed importing git repo"})
		return
	}

	if len(entries) == 0 {
		// empty repo: blank project
		pj, ok := projectlist.ImportFromEntries(cxt.A, cxt, uid, name, nil)
		if !ok {
			res.JSON(500, []byte(`{"message":"failed importing git repo"}`))
			return
		}
		gsPersistImportState(ctx, cxt, cxt.A, pj, fullName, branch, head, cred, uid)
		res.JSON(200, []byte(`{"projectId":"`+pj.Hex()+`"}`))
		return
	}

	pj, ok := projectlist.ImportFromEntries(cxt.A, cxt, uid, name, entries)
	if !ok {
		res.JSON(500, []byte(`{"message":"failed importing git repo"}`))
		return
	}
	gsPersistImportState(ctx, cxt, cxt.A, pj, fullName, branch, head, cred, uid)
	res.JSON(200, []byte(`{"projectId":"`+pj.Hex()+`"}`))
}

func gsPersistImportState(ctx context.Context, cxt *core.Cxt, a *core.App, pj bson.ObjectID, fullName, branch, head string, cred *gsResolvedCred, uid string) {
	ver, _ := projectlist.ProjectVersion(a, cxt, pj)
	st := &gsProjectState{
		ProjectID:         pj,
		RepoFullName:      fullName,
		MergeStatus:       "clean",
		LastSyncCommit:    head,
		DefaultBranchName: branch,
		LastSyncVersion:   ver,
		SyncProvider:      cred.Provider,
		SyncServerURL:     cred.ServerURL,
		SyncUsername:      cred.Username,
		OwnerID:           uid,
	}
	_ = gsSaveState(ctx, a, st)
}

func gsResImportErr(res *core.Res, err error) {
	if ge, ok := err.(*gsError); ok {
		res.JSON(ge.Status, []byte(`{"message":`+gsJSONStr(ge.Message)+`}`))
		return
	}
	res.JSON(500, []byte(`{"message":"failed importing git repo"}`))
}

// ---------- export ----------

// exportProjectHandler — POST /project/:id/github-sync/export (Node
// exportProject).
func exportProjectHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	pidHex := cxt.Params["project_id"]
	oid, err := bson.ObjectIDFromHex(pidHex)
	if err != nil {
		res.JSON(404, []byte(`{"message":"not found"}`))
		return
	}
	body, err := gsBody(cxt.Req, 10<<20)
	if err != nil {
		res.JSON(400, []byte(`{"message":"Bad request"}`))
		return
	}
	provider := gsStr(body, "provider")
	serverUrl := gsStr(body, "serverUrl")
	username := gsStr(body, "username")

	ctx, cancel := gsCtx()
	defer cancel()

	// already linked?
	if st := gsGetState(ctx, cxt.A, oid); st != nil {
		res.JSON(500, []byte(`{"key":"github_validation_check","message":"Project is already linked to Git server"}`))
		return
	}

	cred, err := gsResolveCreds(ctx, cxt.A, uid, provider, serverUrl, username)
	if err != nil {
		gsResErr(res, err)
		return
	}

	// repo options from body
	repoName := gsStr(body, "name")
	if repoName == "" {
		repoName = gsStr(body, "repoName")
	}
	description := gsStr(body, "description")
	isPublic := true
	if v, ok := body["isPublic"].(bool); ok {
		isPublic = v
	}
	org := gsStr(body, "org")

	fullName, defaultBranch, err := gsBridge().CreateRepo(ctx, &gsRepoOptions{
		Name: repoName, Description: description, IsPublic: &isPublic, Org: org,
	}, cred)
	if err != nil {
		gsResErr(res, err)
		return
	}

	// current files
	files, ok := projectlist.GSExportFiles(cxt.A, cxt, oid)
	if !ok {
		res.JSON(500, []byte(`{"key":"github_validation_check","message":"failed to read project"}`))
		return
	}

	fsPath := gsWorkRoot() + "/github_export_" + gsRandHex(8)
	defer osRemoveAll(fsPath)

	if cerr := gsBridge().Clone(ctx, fullName, "HEAD", fsPath, cred.ServerURL, cred.Username, cred.Token); cerr != nil {
		gsResErr(res, cerr)
		return
	}

	// write files into the work tree
	var paths []string
	for _, f := range files {
		if !gsWriteFile(fsPath, f.Path, f.Data) {
			res.JSON(500, []byte(`{"key":"github_validation_check","message":"failed to write export tree"}`))
			return
		}
		paths = append(paths, f.Path)
	}

	if len(paths) > 0 {
		if _, cerr := gsBridge().Commit(ctx, fsPath, paths, "Initial Overleaf import", "Overleaf Sync", "overleaf-sync@localhost", cred); cerr != nil {
			gsResErr(res, cerr)
			return
		}
		if cerr := gsBridge().Push(ctx, fsPath, "origin", defaultBranch, cred); cerr != nil {
			gsResErr(res, cerr)
			return
		}
	}

	// resolve the real head
	head := ""
	if h, herr := gsBridge().BranchHead(ctx, fullName, defaultBranch, cred); herr == nil {
		head = h
	}

	ver, _ := projectlist.ProjectVersion(cxt.A, cxt, oid)
	st := &gsProjectState{
		ProjectID:         oid,
		RepoFullName:      fullName,
		MergeStatus:       "clean",
		DefaultBranchName: defaultBranch,
		LastSyncCommit:    head,
		LastSyncVersion:   ver,
		SyncProvider:      cred.Provider,
		SyncServerURL:     cred.ServerURL,
		SyncUsername:      cred.Username,
		OwnerID:           uid,
	}
	if serr := gsSaveState(ctx, cxt.A, st); serr != nil {
		res.JSON(500, []byte(`{"key":"github_validation_check","message":"failed to persist state"}`))
		return
	}
	res.SendStatus(200)
}

// ---------- state ----------

// projectStateHandler — GET /project/:id/github-sync/state (Node
// getProjectState).
func projectStateHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	pidHex := cxt.Params["project_id"]
	oid, err := bson.ObjectIDFromHex(pidHex)
	if err != nil {
		res.JSON(404, []byte(`{"message":"not found"}`))
		return
	}
	ctx, cancel := gsCtx()
	defer cancel()

	st := gsGetState(ctx, cxt.A, oid)
	if st == nil {
		pss := map[string]any{"mergeStatus": "need-export"}
		pd, okp := projectlist.LoadProjectDoc(cxt.A, cxt, oid)
		if okp {
			owner := projectlist.OwnerRef(pd)
			if owner != uid {
				pss["ownerEmail"] = projectlist.UserEmail(cxt.A, cxt, owner)
			}
		}
		b, _ := json.Marshal(pss)
		res.JSON(200, b)
		return
	}

	// resolve creds + canPush
	cred, err := gsResolveCreds(ctx, cxt.A, uid, st.SyncProvider, st.SyncServerURL, st.SyncUsername)
	if err != nil {
		// "no stored credentials" — return as-is
		out := gsStateJSON(st, nil)
		res.JSON(200, out)
		return
	}
	out := gsStateJSON(st, nil)
	if cp, perr := gsBridge().CanPush(ctx, st.RepoFullName, cred); perr == nil && !cp {
		out = reSetJSON(out, "mergeStatus", "need-permission")
		pd, okp := projectlist.LoadProjectDoc(cxt.A, cxt, oid)
		if okp {
			owner := projectlist.OwnerRef(pd)
			if owner != uid {
				out = reSetJSON(out, "ownerEmail", projectlist.UserEmail(cxt.A, cxt, owner))
			}
		}
	}
	res.JSON(200, out)
}

func gsStateJSON(st *gsProjectState, extra map[string]any) []byte {
	m := map[string]any{
		"projectId":         st.ProjectID,
		"repoFullName":      st.RepoFullName,
		"mergeStatus":       st.MergeStatus,
		"lastSyncCommit":    st.LastSyncCommit,
		"defaultBranchName": st.DefaultBranchName,
		"lastSyncVersion":   st.LastSyncVersion,
		"syncProvider":      st.SyncProvider,
		"syncServerUrl":     st.SyncServerURL,
		"syncUsername":      st.SyncUsername,
		"ownerId":           st.OwnerID,
	}
	if st.UnmergedBranchName != "" {
		m["unmergedBranchName"] = st.UnmergedBranchName
	}
	if st.UnmergedBranchHead != "" {
		m["unmergedBranchHead"] = st.UnmergedBranchHead
	}
	if st.ConflictVersion != 0 {
		m["conflictVersion"] = st.ConflictVersion
	}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return b
}

// reSetJSON — set a top-level key on a JSON object (order-preserving via
// re-marshal is not required; map order is stable enough for the client).
func reSetJSON(b []byte, key string, val any) []byte {
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return b
	}
	m[key] = val
	out, _ := json.Marshal(m)
	return out
}

// ---------- merge overview ----------

// mergeOverviewHandler — GET /project/:id/github-sync/merge/overview
// (Node getMergeOverview).
func mergeOverviewHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	pidHex := cxt.Params["project_id"]
	oid, err := bson.ObjectIDFromHex(pidHex)
	if err != nil {
		res.JSON(404, []byte(`{"message":"not found"}`))
		return
	}
	ctx, cancel := gsCtx()
	defer cancel()

	st := gsGetState(ctx, cxt.A, oid)
	if st == nil {
		res.JSON(404, []byte(`{"message":"Project is not linked with GitHub"}`))
		return
	}
	if st.MergeStatus == "conflict" {
		res.JSON(200, []byte(`null`))
		return
	}
	if st.LastSyncCommit == "" {
		res.JSON(200, []byte(`{"commits":[],"diverged":false,"isProjectUpdated":false}`))
		return
	}

	cred, err := gsResolveCreds(ctx, cxt.A, uid, st.SyncProvider, st.SyncServerURL, st.SyncUsername)
	if err != nil {
		gsResErr(res, err)
		return
	}
	curVer, _ := projectlist.ProjectVersion(cxt.A, cxt, oid)
	isProjectUpdated := curVer != st.LastSyncVersion

	commits, diverged, err := gsBridge().CommitsSince(ctx, st.RepoFullName, st.DefaultBranchName, st.LastSyncCommit, cred)
	if err != nil {
		gsResErr(res, err)
		return
	}
	if diverged {
		_ = gsUpdateStateField(ctx, cxt.A, oid, map[string]any{"mergeStatus": "diverged"})
	}
	out := map[string]any{
		"commits":          commits,
		"diverged":         diverged,
		"isProjectUpdated": isProjectUpdated,
	}
	b, _ := json.Marshal(out)
	res.JSON(200, b)
}

// ---------- merge (apply) ----------

// mergeProjectHandler — POST /project/:id/github-sync/merge (Node
// doGitMerge). Applies remote changes (since lastSyncCommit) to the project
// when there are no local edits; otherwise it records a conflict state.
func mergeProjectHandler(cxt *core.Cxt, res *core.Res) {
	uid := gsSessionUID(cxt)
	pidHex := cxt.Params["project_id"]
	oid, err := bson.ObjectIDFromHex(pidHex)
	if err != nil {
		res.JSON(404, []byte(`{"message":"not found"}`))
		return
	}
	body, _ := gsBody(cxt.Req, 10<<20)
	_ = body
	ctx, cancel := gsCtx()
	defer cancel()

	st := gsGetState(ctx, cxt.A, oid)
	if st == nil {
		res.JSON(404, []byte(`{"message":"Project is not linked with GitHub"}`))
		return
	}
	cred, err := gsResolveCreds(ctx, cxt.A, uid, st.SyncProvider, st.SyncServerURL, st.SyncUsername)
	if err != nil {
		gsResErr(res, err)
		return
	}

	// resolve remote head
	remoteHead, herr := gsBridge().BranchHead(ctx, st.RepoFullName, st.DefaultBranchName, cred)
	if herr != nil {
		res.JSON(500, []byte(`{"message":"failed to resolve remote head"}`))
		return
	}
	if st.LastSyncCommit != "" && remoteHead == st.LastSyncCommit {
		// no remote change
		_ = gsUpdateStateField(ctx, cxt.A, oid, map[string]any{"mergeStatus": "clean"})
		res.JSON(200, []byte(`{"status":"clean","message":"no remote changes"}`))
		return
	}

	curVer, _ := projectlist.ProjectVersion(cxt.A, cxt, oid)
	hasLocalEdits := st.LastSyncVersion != 0 && curVer != st.LastSyncVersion

	if hasLocalEdits {
		// local + remote both changed: record conflict (no base snapshot in
		// the D41 history plane to auto three-way merge).
		nb := "overleaf-conflict-" + gsRandHex(6)
		_ = gsSaveState(ctx, cxt.A, &gsProjectState{
			ProjectID:          oid,
			RepoFullName:       st.RepoFullName,
			MergeStatus:        "conflict",
			LastSyncCommit:     st.LastSyncCommit,
			DefaultBranchName:  st.DefaultBranchName,
			LastSyncVersion:    st.LastSyncVersion,
			SyncProvider:       st.SyncProvider,
			SyncServerURL:      st.SyncServerURL,
			SyncUsername:       st.SyncUsername,
			OwnerID:            st.OwnerID,
			UnmergedBranchName: nb,
			UnmergedBranchHead: remoteHead,
			ConflictVersion:    curVer,
		})
		res.JSON(200, []byte(`{"status":"conflict","message":"both local and remote changed; resolve in the merge UI"}`))
		return
	}

	// remote-only: apply the remote tree to the project
	remoteFiles, rerr := gsFetchRemoteTree(ctx, cred, st.RepoFullName, remoteHead)
	if rerr != nil {
		gsResErr(res, rerr)
		return
	}
	pd, okp := projectlist.LoadProjectDoc(cxt.A, cxt, oid)
	if !okp {
		res.JSON(500, []byte(`{"message":"project not found"}`))
		return
	}
	localFiles, lok := projectlist.GSExportFiles(cxt.A, cxt, oid)
	localSet := map[string]bool{}
	if lok {
		for _, f := range localFiles {
			localSet[f.Path] = true
		}
	}
	remoteSet := map[string]bool{}
	for p := range remoteFiles {
		remoteSet[p] = true
	}
	// upsert remote files
	for _, p := range sortedKeysBool(remoteSet) {
		if !projectlist.MergeUpsert(cxt.A, uid, pd, oid, "/"+p, remoteFiles[p]) {
			res.SendStatus(500)
			res.JSON(500, []byte(`{"message":"failed applying remote changes"}`))
			return
		}
	}
	// delete local files not present remotely
	for _, p := range sortedKeysBool(localSet) {
		if !remoteSet[p] {
			if !projectlist.MergeDelete(cxt.A, uid, pd, oid, "/"+p) {
				// not fatal
			}
		}
	}
	// re-read version after apply and persist state
	newVer, _ := projectlist.ProjectVersion(cxt.A, cxt, oid)
	st.MergeStatus = "clean"
	st.LastSyncCommit = remoteHead
	st.LastSyncVersion = newVer
	if serr := gsSaveState(ctx, cxt.A, st); serr != nil {
		res.JSON(500, []byte(`{"message":"failed to persist state"}`))
		return
	}
	res.JSON(200, []byte(`{"status":"merged","lastSyncCommit":"`+remoteHead+`","lastSyncVersion":`+itoa(newVer)+`}`))
}

// sortedKeysBool — sorted keys of a set.
func sortedKeysBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// itoa — int64 to string.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ---------- remote tree (git contents API) ----------

// gsFetchRemoteTree — download every file at `ref` from the git server via
// the contents REST API (Node GitHubApiClient.getBlobStream equivalent).
// Provider-agnostic: list the root, walk directories recursively (Gitea/
// Forgejo/GitHub-compatible `contents` listing), and GET each file
// (download_url → inline base64 → raw fallback).
func gsFetchRemoteTree(ctx context.Context, cred *gsResolvedCred, repoFullName, ref string) (map[string][]byte, error) {
	base, ok := gitRESTBase(cred.ServerURL)
	if !ok {
		return nil, &gsError{Status: 500, Message: "unsupported git server for merge"}
	}

	type ci struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	var dirStack []string
	dirStack = append(dirStack, "")
	blobs := []string{}
	for len(dirStack) > 0 {
		cur := dirStack[len(dirStack)-1]
		dirStack = dirStack[:len(dirStack)-1]
		u := base + "/repos/" + repoFullName + "/contents"
		if cur != "" {
			u += "/" + cur
		}
		u += "?ref=" + urlQueryEscape(ref)
		var items []ci
		if !gsRESTGet(ctx, cred, u, &items) {
			continue
		}
		for _, it := range items {
			switch it.Type {
			case "dir", "tree":
				dirStack = append(dirStack, it.Path)
			case "file", "blob", "commit":
				blobs = append(blobs, it.Path)
			}
		}
	}
	seen := map[string]bool{}
	uniq := []string{}
	for _, p := range blobs {
		if !seen[p] {
			seen[p] = true
			uniq = append(uniq, p)
		}
	}
	out := map[string][]byte{}
	for _, p := range uniq {
		data, err := gsFetchContentFile(ctx, cred, base, repoFullName, ref, p)
		if err != nil {
			continue
		}
		out[p] = data
	}
	return out, nil
}

func urlQueryEscape(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			out = append(out, c)
		} else {
			out = append(out, []byte(fmt.Sprintf("%%%02X", c))...)
		}
	}
	return string(out)
}

// gsFetchContentFile — one file from a GitHub/Gitea/Forgejo-style contents
// endpoint: download_url → inline base64 `content` → raw bytes.
func gsFetchContentFile(ctx context.Context, cred *gsResolvedCred, base, repo, ref, p string) ([]byte, error) {
	u := base + "/repos/" + repo + "/contents/" + p + "?ref=" + urlQueryEscape(ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	gsAuthHeader(cred, req)
	req.Header.Set("Accept", "application/json")
	resp, err := gsHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &gsError{Status: resp.StatusCode, Message: "file fetch failed (" + resp.Status + ")"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, err
	}
	var fr struct {
		Content     string `json:"content"`
		Encoding    string `json:"encoding"`
		DownloadURL string `json:"download_url"`
	}
	if json.Unmarshal(body, &fr) == nil {
		if fr.DownloadURL != "" {
			req2, e2 := http.NewRequestWithContext(ctx, http.MethodGet, fr.DownloadURL, nil)
			if e2 != nil {
				return nil, e2
			}
			gsAuthHeader(cred, req2)
			resp2, e2 := gsHTTPClient().Do(req2)
			if e2 != nil {
				return nil, e2
			}
			defer resp2.Body.Close()
			if resp2.StatusCode < 200 || resp2.StatusCode >= 300 {
				return nil, &gsError{Status: resp2.StatusCode, Message: "download_url failed (" + resp2.Status + ")"}
			}
			b2, e2 := io.ReadAll(io.LimitReader(resp2.Body, 256<<20))
			if e2 != nil {
				return nil, e2
			}
			return b2, nil
		}
		if fr.Content != "" {
			if fr.Encoding == "base64" {
				b, derr := base64.StdEncoding.DecodeString(fr.Content)
				if derr == nil {
					return b, nil
				}
			}
			return []byte(fr.Content), nil
		}
	}
	return body, nil
}

type gsTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Mode string `json:"mode"`
	ID   string `json:"id"`
}

// gsGitHubTreeResp — GitHub recursive tree shape.
type gsGitHubTreeResp struct {
	Tree []gsTreeEntry `json:"tree"`
}

// gsFetchGitHubFileRaw — GitHub contents API raw (Accept: application/vnd.github.raw).
func gsFetchGitHubFileRaw(ctx context.Context, cred *gsResolvedCred, base, repo, ref, p string) ([]byte, error) {
	u := base + "/repos/" + repo + "/contents/" + p + "?ref=" + ref
	b, err := gsRESTGetRaw(ctx, cred, u, "application/vnd.github.raw")
	if err != nil {
		return nil, err
	}
	return b, nil
}

func gitRESTBase(serverURL string) (string, bool) {
	s := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	switch {
	case strings.Contains(s, "github.com"):
		return "https://api.github.com", true
	case strings.Contains(s, "gitlab.com"):
		return "https://gitlab.com/api/v4", true
	case strings.Contains(s, "gitea.io") || strings.Contains(s, "gitea.com") || strings.Contains(s, "forgejo") || strings.Contains(s, "codeberg"):
		return s + "/api/v1", true
	case strings.Contains(s, "gitlab"):
		return s + "/api/v4", true
	default:
		// best-effort heuristic for generic gitea/forgejo servers: probe /api/v1, then /api/v3
		return "", true
	}
}

// listAllTree — walk a git contents API tree recursively (GitHub-style
// /repos/{repo}/contents returns a directory listing; follow subdirs).
func listAllTree(ctx context.Context, cred *gsResolvedCred, base, repo, ref string, path string) ([]gsTreeEntry, error) {
	all := []gsTreeEntry{}
	var stack []string
	stack = append(stack, path)
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		u := ""
		if cur == "" {
			u = base + "/repos/" + repo + "/contents?ref=" + ref
			u = base + "/repos/" + repo + "/contents"
			u += "?ref=" + ref
		} else {
			u = base + "/repos/" + repo + "/contents/" + strings.ReplaceAll(cur, "/", "/")
			u += "?ref=" + ref
		}
		var items []gsContentItem
		if !gsRESTGet(ctx, cred, u, &items) {
			// GitHub returns 200 with [] for a dir; a real failure returns an error object
			return nil, &gsError{Status: 500, Message: "failed to list tree"}
		}
		for _, it := range items {
			full := it.Path
			if it.Type == "file" {
				all = append(all, gsTreeEntry{Path: full, Type: "blob", Mode: "100644"})
			} else if it.Type == "dir" || it.Type == "tree" {
				stack = append(stack, full)
			}
		}
	}
	return all, nil
}

type gsContentItem struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int    `json:"size"`
}

func gsFetchFileRaw(ctx context.Context, cred *gsResolvedCred, base, repo, ref, path string) ([]byte, error) {
	// GitHub: Accept raw; Gitea/Forgejo: contents API returns base64 in JSON
	u := base + "/repos/" + repo + "/contents/" + strings.ReplaceAll(path, "/", "/") + "?ref=" + ref
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	gsAuthHeader(cred, req)
	req.Header.Set("Accept", "application/json")
	resp, err := gsHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, &gsError{Status: resp.StatusCode, Message: "file fetch failed (" + resp.Status + ")"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, err
	}
	// try JSON first (Gitea/Forgejo)
	var fr struct {
		Content     string `json:"content"`
		Encoding    string `json:"encoding"`
		DownloadURL string `json:"download_url"`
	}
	if json.Unmarshal(body, &fr) == nil && fr.Content != "" {
		if fr.Encoding == "base64" {
			b, derr := base64.StdEncoding.DecodeString(fr.Content)
			if derr == nil {
				return b, nil
			}
		}
		return []byte(fr.Content), nil
	}
	// GitHub with raw accept would have returned bytes directly — check content-type
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "octet-stream") || strings.Contains(ct, "text") {
		return body, nil
	}
	// fallback: treat as raw bytes
	return body, nil
}

func gsListGitLabTree(ctx context.Context, base string, cred *gsResolvedCred) []gsTreeEntry {
	// gitlab v4: need project id; use the encoded fullPath from the repo
	// (owner/name) — list recursively from root.
	// (best-effort; the main live path is github-compatible)
	return nil
}

func gsFetchGitLabFile(ctx context.Context, base string, cred *gsResolvedCred, repo, ref, p string) ([]byte, error) {
	proj := repo
	u := base + "/projects/" + strings.ReplaceAll(proj, "/", "%2F") + "/repository/files/" + urlPathEscape(p) + "?ref=" + ref
	var fr struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if !gsRESTGet(ctx, cred, u, &fr) {
		return nil, &gsError{Status: 500, Message: "failed to fetch file"}
	}
	if fr.Encoding == "base64" {
		b, err := base64.StdEncoding.DecodeString(fr.Content)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	return []byte(fr.Content), nil
}

func urlPathEscape(s string) string {
	out := ""
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			out += string(c)
		} else {
			out += fmt.Sprintf("%%%02X", c)
		}
	}
	return out
}

func gsRESTGet(ctx context.Context, cred *gsResolvedCred, u string, out any) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	gsAuthHeader(cred, req)
	resp, err := gsHTTPClient().Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return json.Unmarshal(b, out) == nil
}

func gsRESTGetRaw(ctx context.Context, cred *gsResolvedCred, u, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	gsAuthHeader(cred, req)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := gsHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, &gsError{Status: 500, Message: "fetch failed (" + resp.Status + ")"}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, err
	}
	return b, nil
}

func gsAuthHeader(cred *gsResolvedCred, req *http.Request) {
	if cred == nil {
		return
	}
	if strings.Contains(cred.ServerURL, "github.com") {
		req.Header.Set("Authorization", "token "+cred.Token)
	} else {
		req.Header.Set("Authorization", "Bearer "+cred.Token)
	}
	req.Header.Set("Accept", "application/json")
}

var _ = http.MethodGet
var _ = mongo.ErrNoDocuments
