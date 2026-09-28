// Package appfactory is the production bundle for the project-history Go
// service: it wires the C-phase manager bundles the way the vendor Node
// module graph does (services/project-history/app/js/*.js — each manager's
// imports, made explicit as Deps seams in this port), then builds the D-phase
// HTTP surface (httpcontroller + internal/server router) and exports a
// ready-to-serve http.Handler.
//
// One Build(ctx, cfg) call is the whole application. Tests keep their fakes
// (server/wired_test.go); this package is production-only wiring.
package appfactory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	mongooptions "go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/libraries/otc"
	"ollitex/go/services/project-history/internal/blobmanager"
	"ollitex/go/services/project-history/internal/chunktranslator"
	"ollitex/go/services/project-history/internal/config"
	"ollitex/go/services/project-history/internal/diffgenerator"
	"ollitex/go/services/project-history/internal/diffmanager"
	"ollitex/go/services/project-history/internal/errrecorder"
	"ollitex/go/services/project-history/internal/flushmanager"
	"ollitex/go/services/project-history/internal/hashmanager"
	"ollitex/go/services/project-history/internal/healthchecker"
	"ollitex/go/services/project-history/internal/historyapimanager"
	"ollitex/go/services/project-history/internal/historyblobtranslator"
	"ollitex/go/services/project-history/internal/historystoremanager"
	"ollitex/go/services/project-history/internal/httpcontroller"
	"ollitex/go/services/project-history/internal/labelsmanager"
	"ollitex/go/services/project-history/internal/largefilemanager"
	"ollitex/go/services/project-history/internal/lockmanager"
	phmongo "ollitex/go/services/project-history/internal/mongo"
	"ollitex/go/services/project-history/internal/mongodrv"
	"ollitex/go/services/project-history/internal/redismanager"
	"ollitex/go/services/project-history/internal/redisx"
	"ollitex/go/services/project-history/internal/retrymanager"
	"ollitex/go/services/project-history/internal/server"
	"ollitex/go/services/project-history/internal/snapshotmanager"
	"ollitex/go/services/project-history/internal/summarizedupdatesmanager"
	"ollitex/go/services/project-history/internal/syncadapter"
	"ollitex/go/services/project-history/internal/syncmanager"
	"ollitex/go/services/project-history/internal/updatecompressor"
	"ollitex/go/services/project-history/internal/updatesprocessor"
	"ollitex/go/services/project-history/internal/updatetranslator"
	"ollitex/go/services/project-history/internal/webapimanager"
)

// Compile-time proof the production *Deps bundles satisfy the controller
// seams (server/wired_test.go proves the fake side).
var (
	_ httpcontroller.UPManager    = (*updatesprocessor.Deps)(nil)
	_ httpcontroller.SUMManager   = (*summarizedupdatesmanager.Deps)(nil)
	_ httpcontroller.DIFFManager  = (*diffmanager.Deps)(nil)
	_ httpcontroller.HSMManager   = (*historystoremanager.Deps)(nil)
	_ httpcontroller.WEBManager   = (*webapimanager.Deps)(nil)
	_ httpcontroller.SNAPManager  = (*snapshotmanager.Deps)(nil)
	_ httpcontroller.HCManager    = (*healthchecker.Deps)(nil)
	_ httpcontroller.SYNCManager  = (*syncmanager.Deps)(nil)
	_ httpcontroller.LBLManager   = (*labelsmanager.Deps)(nil)
	_ httpcontroller.APIManager   = (*historyapimanager.Deps)(nil)
	_ httpcontroller.RETRYManager = (*retrymanager.Deps)(nil)
	_ httpcontroller.FLUSHManager = (*flushmanager.Deps)(nil)
)

// App is the built service.
type App struct {
	Handler    http.Handler
	Controller *httpcontroller.Controller
	Deps       *httpcontroller.Deps
	// Close releases drivers (mongo + both redis clients).
	Close func()
}

// httpDoer — basic-auth HTTP seam (vendor fetch-utils subset).
type httpDoer struct {
	client *http.Client
	user   string
	pass   string
}

// Do — vendor request(): (body, status, nil) for ANY response; (nil, 0, err)
// on transport failure.
func (h httpDoer) Do(ctx context.Context, method, url string, query map[string]string, header map[string]string, body []byte) ([]byte, int, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, 0, err
	}
	if h.user != "" {
		req.SetBasicAuth(h.user, h.pass)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	if len(query) > 0 {
		q := req.URL.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return b, resp.StatusCode, nil
}

// Head — vendor fetchNothing(HEAD) for _checkBlobExists.
func (h httpDoer) Head(ctx context.Context, url string, _ map[string]string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	if h.user != "" {
		req.SetBasicAuth(h.user, h.pass)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// fileStoreReader — fetchStream(filestoreURL) (branch 2 of createBlob).
type fileStoreReader struct{ d httpDoer }

func (f fileStoreReader) FileStoreRead(ctx context.Context, filestoreURL string) ([]byte, error) {
	b, status, err := f.d.Do(ctx, http.MethodGet, filestoreURL, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("filestore request failed with status %d", status)
	}
	return b, nil
}

// localFileWriter — LocalFileWriter.bufferOnDisk (C13).
type localFileWriter struct{ root string }

func (w localFileWriter) WriteToDisk(_ context.Context, data []byte, _ string, fileID string) (string, error) {
	full := strings.TrimSuffix(w.root, "/") + "/" + fileID
	if err := os.MkdirAll(w.root, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", err
	}
	return full, nil
}

// rangeSeam — HistoryBlobTranslator seam (C4), wired to the B9 port.
type rangeSeam struct{}

func (rangeSeam) CreateRangeBlobDataFromUpdate(update map[string]any) (map[string]any, bool, error) {
	return historyblobtranslator.CreateRangeBlobDataFromUpdate(update)
}

// hashSeam — HashManager seam (C2 defaultHasher is wired via SetHasher too;
// this field keeps the seam explicit).
type hashSeam struct{}

func (hashSeam) GetBlobHash(path string) (string, int64, error) {
	return hashmanager.GetBlobHash(path)
}

// ---------------------------------------------------------------------------
// narrow vendor-collection adapters over the C1 mongo seam
// ---------------------------------------------------------------------------

type labelsAdapter struct{ c phmongo.Collection }

func (a labelsAdapter) Find(ctx context.Context, filter, projection map[string]any) ([]map[string]any, error) {
	it, err := a.c.Find(ctx, filter, phmongo.FindOpts{Projection: projection})
	if err != nil {
		return nil, err
	}
	defer it.Close(ctx)
	return it.All(ctx)
}
func (a labelsAdapter) InsertMany(ctx context.Context, docs []map[string]any) error {
	return a.c.InsertMany(ctx, docs)
}
func (a labelsAdapter) InsertOne(ctx context.Context, doc map[string]any) (any, error) {
	// Vendor (Node) mongoose generates `_id` on insert and returns the created
	// doc (label carries a usable `id`); the C1 phmongo seam discards the
	// driver's InsertedID, so generate it up front (1:1 with mongoose) and
	// return the stored id.
	if doc["_id"] == nil {
		doc["_id"] = primitive.NewObjectID()
	}
	id := doc["_id"]
	if err := a.c.InsertOne(ctx, doc, phmongo.InsertOneOpts{}); err != nil {
		return nil, err
	}
	return id, nil
}
func (a labelsAdapter) DeleteOne(ctx context.Context, filter map[string]any) error {
	// Node mongoose coerces a 24-hex string `_id` to ObjectId in query filters;
	// the raw Go driver does not — labels store ObjectID ids, so coerce to keep
	// the vendor delete-matching 1:1 (0-match would otherwise 204 silently).
	if id, ok := filter["_id"].(string); ok {
		if oid, err := primitive.ObjectIDFromHex(id); err == nil {
			filter["_id"] = oid
		}
	}
	_, err := a.c.DeleteOne(ctx, filter)
	return err
}
func (a labelsAdapter) UpdateMany(ctx context.Context, filter, set map[string]any) error {
	_, err := a.c.UpdateMany(ctx, filter, set, phmongo.UpdateOpts{})
	return err
}

type erStoreAdapter struct{ c phmongo.Collection }

func (a erStoreAdapter) FindOneAndUpdate(ctx context.Context, filter, update map[string]any, retAfter bool, projection map[string]any) (map[string]any, error) {
	return a.c.FindOneAndUpdate(ctx, filter, update, phmongo.FindOneAndUpdateOpts{ReturnAfter: retAfter, Projection: projection})
}
func (a erStoreAdapter) DeleteOne(ctx context.Context, filter map[string]any) (int64, error) {
	return a.c.DeleteOne(ctx, filter)
}
func (a erStoreAdapter) UpdateOne(ctx context.Context, filter, update map[string]any, upsert bool) error {
	_, err := a.c.UpdateOne(ctx, filter, update, phmongo.UpdateOpts{Upsert: upsert})
	return err
}
func (a erStoreAdapter) FindAll(ctx context.Context) ([]map[string]any, error) {
	it, err := a.c.Find(ctx, map[string]any{}, phmongo.FindOpts{})
	if err != nil {
		return nil, err
	}
	defer it.Close(ctx)
	return it.All(ctx)
}
func (a erStoreAdapter) FindOne(ctx context.Context, filter, projection map[string]any) (map[string]any, error) {
	return a.c.FindOne(ctx, filter, phmongo.FindOneOpts{Projection: projection})
}
func (a erStoreAdapter) InsertOne(ctx context.Context, doc map[string]any) error {
	return a.c.InsertOne(ctx, doc, phmongo.InsertOneOpts{})
}

type syncColl struct{ c phmongo.Collection }

func (a syncColl) FindOneState(ctx context.Context, projectID string) (map[string]any, bool, error) {
	d, err := a.c.FindOne(ctx, map[string]any{"project": projectID}, phmongo.FindOneOpts{})
	if err != nil {
		return nil, false, err
	}
	return d, d != nil, nil
}
func (a syncColl) InsertOneState(ctx context.Context, doc map[string]any) error {
	return a.c.InsertOne(ctx, doc, phmongo.InsertOneOpts{})
}
func (a syncColl) UpdateStateDoc(ctx context.Context, projectID string, update map[string]any, upsert bool) error {
	_, err := a.c.UpdateOne(ctx, map[string]any{"project": projectID}, update, phmongo.UpdateOpts{Upsert: upsert})
	return err
}
func (a syncColl) DeleteStateDoc(ctx context.Context, projectID string, match map[string]any) error {
	f := map[string]any{"project": projectID}
	for k, v := range match {
		f[k] = v
	}
	_, err := a.c.DeleteOne(ctx, f)
	return err
}
func (a syncColl) UpdateProjectsDoc(ctx context.Context, projectID string, update map[string]any) error {
	_, err := a.c.UpdateOne(ctx, map[string]any{"_id": projectID}, update, phmongo.UpdateOpts{})
	return err
}

// ---------------------------------------------------------------------------
// production closures
// ---------------------------------------------------------------------------

// syncRunWithLock — vendor LockManager.runWithLock(key, runner, callback):
// getLock → runner(extend, release); release(err1,...args) → lock.release;
// callback(err1||err2, ...args).
func syncRunWithLock(LOCK *lockmanager.LockManager, key string, runner func(extend func() error, release func(error, ...any) error), done func(error, ...any)) {
	val, err := LOCK.GetLock(key)
	if err != nil {
		done(err)
		return
	}
	extend := func() error { return LOCK.Extend(key, val) }
	release := func(err1 error, args ...any) error {
		err2 := LOCK.Release(key, val)
		e := err1
		if e == nil {
			e = err2
		}
		done(e, args...)
		return e
	}
	runner(extend, release)
}

// fileTreeDiffFold — app/js/FileTreeDiffGenerator.buildDiff(rawChunk, from,
// to) 1:1 (otc Chunk + otc.BuildFileTreeDiff + _buildDiffEntry).
func fileTreeDiffFold(rawChunk map[string]any, fromVersion, toVersion int) (any, error) {
	chunk, err := otc.ChunkFromRaw(rawChunkChunk(rawChunk))
	if err != nil {
		return nil, err
	}
	start := chunk.GetStartVersion()
	changes := chunk.GetChanges()
	if len(changes) < toVersion-start || toVersion < fromVersion {
		return nil, fmt.Errorf("file tree diff: version range out of chunk bounds")
	}
	initial := chunk.GetSnapshot()
	if e := initial.ApplyAll(changes[:fromVersion-start], true); e != nil {
		return nil, e
	}
	lo := fromVersion - start
	hi := toVersion - start
	diff := otc.BuildFileTreeDiff(changes[lo:hi], otc.FileTreeDiffOptions{
		InitialPathnames: initial.GetFilePathnames(),
		OnMoveCollision: func(entry *otc.FileTreeDiffEntry, op *otc.MoveFileOperation) {
			panic(moveCollision{pathname: op.Pathname, newPathname: op.NewPathname})
		},
	})
	entries := diff.OrderedEntries()
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, buildDiffEntry(entry, initial, fromVersion))
	}
	return out, nil
}

func rawChunkChunk(raw map[string]any) map[string]any {
	if c, ok := raw["chunk"].(map[string]any); ok {
		return c
	}
	return raw
}

type moveCollision struct {
	pathname    string
	newPathname string
}

func (m moveCollision) Error() string {
	return "trying to move to file that already exists"
}

// buildDiffEntry — vendor _buildDiffEntry 1:1.
func buildDiffEntry(entry *otc.FileTreeDiffEntry, initialSnapshot *otc.Snapshot, fromVersion int) map[string]any {
	origin := entry.Origin
	pathname := entry.Chain[len(entry.Chain)-1]
	renamed := origin != nil && pathname != *origin

	e := map[string]any{}
	if origin != nil {
		e["pathname"] = *origin
	} else {
		e["pathname"] = pathname
	}
	if renamed {
		e["newPathname"] = pathname
	}
	if entry.DeletedAtChangeIndex != nil {
		e["operation"] = "removed"
		e["deletedAtV"] = fromVersion + *entry.DeletedAtChangeIndex
	} else if origin == nil {
		e["operation"] = "added"
	} else if renamed {
		e["operation"] = "renamed"
	} else if entry.Edited {
		e["operation"] = "edited"
	}
	editedFirst := entry.File == nil && entry.FirstEditedAtChainIndex != nil && *entry.FirstEditedAtChainIndex == 0
	var file *otc.File
	if entry.File != nil {
		file = entry.File
	} else if origin != nil && initialSnapshot != nil {
		file = initialSnapshot.GetFile(*origin)
	}
	if file != nil && !editedFirst {
		e["editable"] = file.IsEditable()
	}
	return e
}

// sumChunkConv — B10 (S4): wrapped chunk → RawChunk → ConvertToSummarizedUpdates.
func sumChunkConv2(chunk map[string]any) ([]map[string]any, error) {
	c, err := chunktranslator.RawChunk(chunk)
	if err != nil {
		return nil, err
	}
	return chunktranslator.ConvertToSummarizedUpdates(c)
}

// ctDiffUpdates — D1: B10 convertToDiffUpdates (chunktranslator) with the
// vendor Deps (getHistoryId → int, getProjectBlob via C2).
func ctDiffUpdates(ctx context.Context, projectID string, chunk map[string]any, pathname string, from, to int, wa *webapimanager.Deps, hsm *historystoremanager.Deps) (map[string]any, error) {
	c, err := chunktranslator.RawChunk(chunk)
	if err != nil {
		return nil, err
	}
	deps := chunktranslator.Deps{
		GetHistoryID: func(pid string) (int, error) {
			s, e := wa.GetHistoryId(ctx, pid)
			if e != nil {
				return 0, e
			}
			return strconv.Atoi(s)
		},
		GetProjectBlob: func(historyID int, hash string) (string, error) {
			return hsm.GetProjectBlob(ctx, strconv.Itoa(historyID), hash)
		},
	}
	return chunktranslator.ConvertToDiffUpdates(deps, projectID, c, pathname, from, to)
}

// upConvertToChanges — B5 (UpdateTranslator.convertToChanges) over the seam
// shape (raw updates in, raw change maps out).
func upConvertToChanges() func(projectID string, updates []map[string]any) ([]map[string]any, error) {
	return func(projectID string, updates []map[string]any) ([]map[string]any, error) {
		uws := make([]updatetranslator.UpdateWithBlob, 0, len(updates))
		for _, u := range updates {
			uws = append(uws, updatetranslator.UpdateWithBlob{Update: u})
		}
		changes, err := updatetranslator.ConvertToChanges(projectID, uws)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(changes))
		for _, ch := range changes {
			out = append(out, ch.ToRaw())
		}
		return out, nil
	}
}

// rawBuildDiff — vendor app/js/DiffGenerator.buildDiff over the wire shape
// (updates {op:[{p,i,d,broken}], meta}) → the B6 port's Update/Part model.
func rawBuildDiff(initialContent string, updates []map[string]any) any {
	ds := make([]diffgenerator.Update, 0, len(updates))
	for _, u := range updates {
		ds = append(ds, rawUpdateToParts(u))
	}
	return diffgenerator.BuildDiff(initialContent, ds)
}

func rawUpdateToParts(u map[string]any) diffgenerator.Update {
	out := diffgenerator.Update{}
	ops, _ := u["op"].([]any)
	for _, ro := range ops {
		m, ok := ro.(map[string]any)
		if !ok {
			continue
		}
		p := diffgenerator.Part{}
		p.Broken = m["broken"] == true
		if n, ok := toInt(m["p"]); ok {
			p.P = n
		}
		if s, ok := m["i"].(string); ok {
			p.I = &s
		}
		if s, ok := m["d"].(string); ok {
			p.D = &s
		}
		out.Op = append(out.Op, p)
	}
	if meta, ok := u["meta"].(map[string]any); ok {
		pm := &diffgenerator.PartMeta{}
		if users, ok := meta["users"].([]any); ok {
			pm.Users = users
		}
		if n, ok := toInt(meta["start_ts"]); ok {
			v := n
			pm.StartTs = &v
		}
		if n, ok := toInt(meta["end_ts"]); ok {
			v := n
			pm.EndTs = &v
		}
		out.Meta = pm
	}
	return out
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// retryFailureFromRaw — errrecorder raw failure doc → retrymanager.Failure.
func retryFailureFromRaw(d map[string]any) retrymanager.Failure {
	f := retrymanager.Failure{}
	if v, ok := d["projectID"].(string); ok {
		f.ProjectID = v
	}
	if v, ok := d["error"].(string); ok {
		f.Error = v
	}
	if v, ok := intPtrOf(d["attempts"]); ok {
		f.Attempts = &v
	}
	if v, ok := intPtrOf(d["resyncAttempts"]); ok {
		f.ResyncAttempts = &v
	}
	return f
}

func intPtrOf(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}

// Build wires everything for cfg.
func Build(ctx context.Context, cfg *config.Config) (*App, error) {
	if cfg == nil {
		cfg = config.Load()
	}
	timeout := requestTimeout(cfg)
	httpc := &http.Client{Timeout: timeout}

	// ---- Mongo (vendor mongodb.js: five named collections) -----------------
	mclient, err := mongo.Connect(ctx, mongooptions.Client().ApplyURI(cfg.MongoURL))
	if err != nil {
		return nil, fmt.Errorf("appfactory: mongo connect: %w", err)
	}
	if err := mclient.Ping(ctx, nil); err != nil {
		_ = mclient.Disconnect(ctx)
		return nil, fmt.Errorf("appfactory: cannot connect to mongo (vendor: fatal): %w", err)
	}
	dbName := "sharelatex"
	if i := strings.Index(strings.TrimPrefix(cfg.MongoURL, "mongodb://"), "/"); i > 0 {
		if rest := strings.TrimPrefix(cfg.MongoURL, "mongodb://"); len(rest) > i+1 {
			seg := strings.SplitN(rest[i+1:], "?", 2)[0]
			if seg != "" {
				dbName = seg
			}
		}
	}
	mdb := mongodrv.NewDB(mongodrv.DriverDB(mclient.Database(dbName)))

	// ---- Redis (lock + project-history ops) --------------------------------
	rdLock, err := redisx.Dialer{Host: cfg.RedisLockHost, Port: cfg.RedisLockPort, Password: cfg.RedisLockPassword}.Open()
	if err != nil {
		_ = mclient.Disconnect(ctx)
		return nil, fmt.Errorf("appfactory: redis lock: %w", err)
	}
	rdPH, err := redisx.Dialer{Host: cfg.RedisPHHost, Port: cfg.RedisPHPort, Password: cfg.RedisPHPass}.Open()
	if err != nil {
		_ = rdLock.Close()
		_ = mclient.Disconnect(ctx)
		return nil, fmt.Errorf("appfactory: redis PH: %w", err)
	}
	RED := redismanager.New(rdPH, config.KeySchema{})
	LOCK := lockmanager.New(rdLock)

	// ---- HTTP seams ---------------------------------------------------------
	webHTTP := httpDoer{client: httpc, user: cfg.WebUser, pass: cfg.WebPass}
	histHTTP := httpDoer{client: httpc, user: cfg.V1User, pass: cfg.V1Pass}

	// ---- error recorder (B12) ------------------------------------------------
	erDeps := &errrecorder.Deps{
		Store: erStoreAdapter{c: mdb.ProjectHistoryFailures},
	}

	// ---- web api manager (WebApiManager.js) ---------------------------------
	wa := &webapimanager.Deps{
		WebAPIURL: cfg.WebURL,
		WebUser:   cfg.WebUser,
		WebPass:   cfg.WebPass,
		FetchNothing: func(ctx context.Context, url string, body map[string]any, _ time.Duration) (*webapimanager.Response, error) {
			var b []byte
			if body != nil {
				j, err := json.Marshal(body)
				if err != nil {
					return nil, err
				}
				b = j
			}
			out, status, err := webHTTP.Do(ctx, http.MethodPost, url, nil, map[string]string{"content-type": "application/json"}, b)
			if err != nil {
				return nil, err
			}
			if out == nil {
				out = []byte{}
			}
			return &webapimanager.Response{Status: status}, nil
		},
		FetchJson: func(ctx context.Context, url string, _ time.Duration) (map[string]any, error) {
			b, status, err := webHTTP.Do(ctx, http.MethodGet, url, nil, nil, nil)
			if err != nil {
				return nil, err
			}
			if status != 200 {
				return nil, &webapimanager.RequestFailedError{Status: status, Msg: "fetch request failed with status " + fmt.Sprint(status)}
			}
			var out map[string]any
			if len(b) > 0 {
				if err := json.Unmarshal(b, &out); err != nil {
					return nil, err
				}
			}
			return out, nil
		},
		GetCachedHistoryId: func(_ context.Context, projectID string) (string, error) {
			v, ok, err := RED.GetCachedHistoryID(projectID)
			if err != nil {
				return "", err
			}
			if !ok {
				return "", &webapimanager.NotFoundError{Msg: "notfound"}
			}
			return v, nil
		},
		SetCachedHistoryId: func(_ context.Context, projectID, historyID string) error {
			return RED.SetCachedHistoryID(projectID, historyID)
		},
		Inc:   func(string) {},
		Sleep: time.Sleep,
	}

	// ---- history api manager (ShouldUseProjectHistory) ----------------------
	apiDeps := &historyapimanager.Deps{WebApi: wa}

	// ---- history store manager (C2, S3-native seams) -------------------------
	historystoremanager.SetHasher(hashmanager.GetBlobHash)
	hsm := &historystoremanager.Deps{
		Client: histHTTP,
		Settings: historystoremanager.Settings{
			HistoryHost:      cfg.V1HistoryURL,
			HistoryUser:      cfg.V1User,
			HistoryPass:      cfg.V1Pass,
			FilestoreURL:     cfg.FileStoreURL,
			FilestoreEnabled: cfg.FileStoreEnabled,
			RequestTimeout:   timeout,
			UploadFolder:     cfg.UploadFolder,
		},
		Range:      rangeSeam{},
		FileWriter: localFileWriter{root: cfg.UploadFolder},
		Hasher:     hashSeam{},
		FileStore:  fileStoreReader{d: histHTTP},
	}

	// ---- blob manager (C3) ----------------------------------------------------
	blob := &blobmanager.Deps{
		CreateBlob: hsm.CreateBlobForUpdate,
		ExtendLock: func(ctx context.Context) error {
			// vendor BlobManager does not extend its own lock here — the
			// caller's runner does; honor the seam with a no-error default.
			_ = ctx
			return nil
		},
		Slew:     time.Sleep,
		Attempts: cfg.SyncRetriesMax,
		Interval: time.Duration(cfg.SyncInterval) * time.Millisecond,
	}

	// ---- snapshot manager (C7) --------------------------------------------------
	snapDeps := &snapshotmanager.Deps{
		GetHistoryID:       wa.GetHistoryId,
		GetMostRecentChunk: hsm.GetMostRecentChunk,
		GetChunkAtVersion:  hsm.GetChunkAtVersion,
		GetBlob:            hsm.GetProjectBlob,
		Ranges: func(ctx context.Context, historyID string, f *snapshotmanager.File) ([]any, []any) {
			_ = ctx
			_ = historyID
			_ = f
			return nil, nil
		},
		MaxRequests: 4, // vendor MAX_REQUESTS
	}

	// ---- sync manager (C16) ------------------------------------------------------
	syncDeps := &syncmanager.Deps{
		FindOneState:      syncColl{c: mdb.ProjectHistorySyncState}.FindOneState,
		InsertOneState:    syncColl{c: mdb.ProjectHistorySyncState}.InsertOneState,
		UpdateStateDoc:    syncColl{c: mdb.ProjectHistorySyncState}.UpdateStateDoc,
		DeleteStateDoc:    syncColl{c: mdb.ProjectHistorySyncState}.DeleteStateDoc,
		UpdateProjectsDoc: syncColl{c: mdb.Projects}.UpdateProjectsDoc,
		RunWithLock: func(key string, runner func(extend func() error, release func(error, ...any) error), done func(error, ...any)) {
			syncRunWithLock(LOCK, key, runner, done)
		},
		RecordSyncStart: func(ctx context.Context, p string) error { return errrecorder.RecordSyncStart(ctx, erDeps, p) },
		RecordError: func(ctx context.Context, p string, q int, e error) error {
			_, err := errrecorder.Record(ctx, erDeps, p, q, errString(e), errString(e))
			return err
		},
		ClearFirstOpTimestamp:  func(_ context.Context, p string) error { return RED.ClearFirstOpTimestamp(p) },
		DestroyDocUpdatesQueue: func(_ context.Context, p string) error { return RED.DestroyDocUpdatesQueue(p) },
		DeleteAppliedDocUpdate: func(ctx context.Context, p string, u map[string]any) error {
			return RED.DeleteAppliedDocUpdates(p, redisRawOf(u))
		},
		RequestResync: wa.RequestResync,
		GetHistoryID:  wa.GetHistoryId,
		GetLatestSnapshotFilesForChunk: func(_ context.Context, _ string, chunk map[string]any) (map[string]*syncmanager.File, error) {
			return syncFilesFromChunk(chunk), nil
		},
		LoadFileContent: func(ctx context.Context, projectID string, f *syncmanager.File) error {
			hid, err := wa.GetHistoryId(ctx, projectID)
			if err != nil {
				return err
			}
			content, err := hsm.GetProjectBlob(ctx, hid, f.Hash)
			if err != nil {
				return err
			}
			f.Content = content
			return nil
		},
		GetBlobHashFromString: hashmanager.GetBlobHashFromString,
		DiffAsShareJsOps: func(a, b string) []any {
			ops := updatecompressor.DiffAsShareJsOps(a, b)
			out := make([]any, 0, len(ops))
			for _, o := range ops {
				for k, v := range o {
					_ = k
					_ = v
				}
				out = append(out, o)
			}
			return out
		},
		ConvertPathname: func(s string) string { return s },
		IsDataCorruption: func(err error) bool {
			type corruptable interface{ IsDataCorruption() bool }
			if c, ok := err.(corruptable); ok {
				return c.IsDataCorruption()
			}
			return err != nil && (strings.Contains(errString(err), "Invalid") || strings.Contains(errString(err), "SyntaxError") || strings.Contains(errString(err), "FileContentEmpty"))
		},
		Inc:      func(string, int, map[string]any) {},
		LogDebug: slog{"debug"}.fn(),
		LogWarn:  slog{"warn"}.fn(),
		LogErr:   slog{"error"}.fn(),
		Now:      time.Now,
	}

	// ---- updates processor (C17) --------------------------------------------------
	upDeps := &updatesprocessor.Deps{
		Sync:                    syncadapter.New(syncDeps),
		CountUnprocessedUpdates: func(ctx context.Context, p string) (int, error) { _ = ctx; return RED.CountUnprocessedUpdates(p) },
		GetRawUpdatesBatch: func(_ context.Context, p string, n int) ([]string, error) {
			b, err := RED.GetRawUpdatesBatch(p, n)
			if err != nil {
				return nil, err
			}
			return b.RAWUpdates, nil
		},
		ParseDocUpdates: redismanager.ParseDocUpdates,
		GetUpdatesInBatches: func(_ context.Context, p string, n int, runner func([]map[string]any) error) error {
			return RED.GetUpdatesInBatches(p, n, runner)
		},
		ClearDanglingFirstOpTimestamp: func(_ context.Context, p string) error { _, err := RED.ClearDanglingFirstOpTimestamp(p); return err },
		RunWithLock: func(_ context.Context, key string, runner func(extend func() error) error) error {
			return LOCK.RunWithLock(key, func(e lockmanager.Extend) error {
				return runner(func() error { return e() })
			})
		},
		Record: func(ctx context.Context, p string, q int, e error) (map[string]any, error) {
			return errrecorder.Record(ctx, erDeps, p, q, errString(e), errString(e))
		},
		ClearError: func(ctx context.Context, p string) error {
			_, err := errrecorder.ClearError(ctx, erDeps, p)
			return err
		},
		GetFailureRecord: func(ctx context.Context, p string) (map[string]any, error) {
			return errrecorder.GetFailureRecord(ctx, erDeps, p)
		},
		GetHistoryID:         wa.GetHistoryId,
		GetMostRecentChunk:   hsm.GetMostRecentChunk,
		GetMostRecentVersion: hsm.GetMostRecentVersion,
		SendChanges:          hsm.SendChanges,
		CreateBlobsForUpdates: func(ctx context.Context, p, h string, u []map[string]any) ([]map[string]any, error) {
			return blobmanager.CreateBlobsForUpdates(ctx, blob, p, h, u)
		},
		CompressRawUpdates: updatecompressor.CompressRawUpdates,
		ConvertToChanges:   upConvertToChanges(),
		Inc:                func(string) {},
		Timing:             func(string, int, int) {},
		Observe:            func(string, float64) {},
		LogWarn:            func(map[string]any, string) {},
		LogErr:             func(map[string]any, string) {},
		Now:                func() int64 { return time.Now().UnixMilli() },
	}

	// ---- diff manager (D1/D2/D3/D4) -------------------------------------------------
	diffDeps := &diffmanager.Deps{
		ProcessUpdates:    upDeps.ProcessUpdatesForProject,
		GetHistoryId:      wa.GetHistoryId,
		GetChunkAtVersion: hsm.GetChunkAtVersion,
		ToDiffUpdates: func(ctx context.Context, projectID string, chunk map[string]any, pathname string, from, to int) (map[string]any, error) {
			return ctDiffUpdates(ctx, projectID, chunk, pathname, from, to, wa, hsm)
		},
		BuildDiff:         rawBuildDiff,
		BuildFileTreeDiff: fileTreeDiffFold,
	}

	// ---- summarized updates (S1–S4) ---------------------------------------------------
	sumDeps := &summarizedupdatesmanager.Deps{
		ProcessUpdates:             upDeps.ProcessUpdatesForProject,
		GetHistoryId:               wa.GetHistoryId,
		ShouldUseProjectHistory:    apiDeps.ShouldUseProjectHistory,
		MostRecentChunk:            hsm.GetMostRecentChunk,
		ChunkAtVersion:             hsm.GetChunkAtVersion,
		ConvertToSummarizedUpdates: sumChunkConv2,
	}

	// ---- labels (C9) -------------------------------------------------------------------
	lblDeps := &labelsmanager.Deps{
		Labels:            labelsAdapter{c: mdb.ProjectHistoryLabels},
		ProcessUpdates:    upDeps.ProcessUpdatesForProject,
		GetHistoryId:      wa.GetHistoryId,
		GetChunkAtVersion: hsm.GetChunkAtVersion,
		Now:               func() any { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") },
	}
	sumDeps.GetLabels = lblDeps.GetLabels

	// ---- retry (B13) ---------------------------------------------------------------------
	retryDeps := &retrymanager.Deps{
		GetFailedProjects: func(ctx context.Context) ([]retrymanager.Failure, error) {
			raw, err := errrecorder.GetFailedProjects(ctx, erDeps)
			if err != nil {
				return nil, err
			}
			out := make([]retrymanager.Failure, 0, len(raw))
			for _, d := range raw {
				out = append(out, retryFailureFromRaw(d))
			}
			return out, nil
		},
		ClearError: func(ctx context.Context, p string) error {
			_, err := errrecorder.ClearError(ctx, erDeps, p)
			return err
		},
		GetFailureRecord: func(ctx context.Context, p string) (*retrymanager.Failure, error) {
			d, err := errrecorder.GetFailureRecord(ctx, erDeps, p)
			if err != nil {
				return nil, err
			}
			if d == nil {
				return nil, nil
			}
			f := retryFailureFromRaw(d)
			return &f, nil
		},
		GetHistoryID:             wa.GetHistoryId,
		StartHardResync:          func(ctx context.Context, p string) error { return syncDeps.StartHardResync(ctx, p, nil) },
		StartResync:              func(ctx context.Context, p string) error { return syncDeps.StartResync(ctx, p, nil) },
		CountUnprocessedUpdates:  func(ctx context.Context, p string) (int, error) { _ = ctx; return RED.CountUnprocessedUpdates(p) },
		ProcessUpdatesForProject: upDeps.ProcessUpdatesForProject,
		Sleep:                    time.Sleep,
		Now:                      time.Now,
	}

	// ---- flush (F1–F6) ---------------------------------------------------------------------
	flushDeps := &flushmanager.Deps{
		GetFirstOpTimestamp: func(ctx context.Context, p string) (int64, error) {
			t, ok, err := RED.GetFirstOpTimestamp(p)
			if err != nil {
				return 0, err
			}
			if !ok {
				return 0, nil
			}
			return t.UnixMilli(), nil
		},
		GetProjectIdsWithHistoryOps: func(ctx context.Context) ([]string, error) { _ = ctx; return RED.GetProjectIDsWithHistoryOps() },
		GetFailedProjects:           func(ctx context.Context) ([]map[string]any, error) { return errrecorder.GetFailedProjects(ctx, erDeps) },
		ProcessUpdates:              upDeps.ProcessUpdatesForProject,
		ShortHistoryQueues:          cfg.ShortHistoryQueues,
		UploadFolder:                cfg.UploadFolder,
		Inc:                         func(string, int, map[string]string) {},
		Slew:                        time.Sleep,
		Now:                         time.Now,
		WriteLocal: func(fsPath string, data []byte) (int64, error) {
			if err := os.WriteFile(fsPath, data, 0o644); err != nil {
				return 0, err
			}
			return int64(len(data)), nil
		},
		ReplaceWithStubIfNeeded: func(fsPath, fileId string, fileSize int64) (string, error) {
			return largefilemanager.ReplaceWithStubIfNeeded(largeFileDeps(cfg), fsPath, fileId, fileSize)
		},
		DeleteFile: os.Remove,
		Log:        func(error) {},
	}

	// ---- health checker (H1–H4) ------------------------------------------------------------------
	hcDeps := &healthchecker.Deps{
		Port:      fmt.Sprint(cfg.Port),
		ProjectID: cfg.HealthCheckProjectID,
		FetchNothing: func(ctx context.Context, url string, _ time.Duration) error {
			_, status, err := webHTTP.Do(ctx, http.MethodGet, url, nil, nil, nil)
			if err != nil {
				return err
			}
			if status >= 500 {
				return fmt.Errorf("health endpoint returned %d", status)
			}
			return nil
		},
		CheckLockFn: func(ctx context.Context) (bool, error) {
			_ = ctx
			return LOCK.CheckLock(cfg.HealthCheckProjectID)
		},
	}

	d := &httpcontroller.Deps{
		UP:              upDeps,
		SUM:             sumDeps,
		DIFF:            diffDeps,
		HSM:             hsm,
		WEB:             wa,
		SNAP:            snapDeps,
		HC:              hcDeps,
		SYNC:            syncDeps,
		ER:              erDeps,
		RED:             RED,
		LBL:             lblDeps,
		API:             apiDeps,
		RETRY:           retryDeps,
		FLUSH:           flushDeps,
		CloneSendUpdate: func(string) {},
		CloneFail:       func(error) bool { return false },
		Aborted:         func() bool { return false },
		FetchCallback: func(url string, headers map[string]string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			b, _, err := webHTTP.Do(ctx, http.MethodGet, url, nil, nil, nil)
			_ = b
			if err == nil {
				for k, v := range headers {
					_ = k
					_ = v
				}
			}
			return err
		},
		LogWarn:            slog{"warn"}.fn(),
		LogDebug:           func(map[string]any, string) {},
		LogErr:             slog{"error"}.fn(),
		LogErrLvl:          slog{"error"}.fn(),
		RedisReadBatchSize: 500,
	}
	_ = d

	c := httpcontroller.New(context.Background(), d)
	srv := (&server.Server{}).NewWiredServer(&server.Deps{C: c})

	return &App{
		Handler:    srv,
		Controller: c,
		Deps:       d,
		Close: func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = mclient.Disconnect(ctx)
			_ = rdLock.Close()
			_ = rdPH.Close()
		},
	}, nil
}

func requestTimeout(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.V1RequestTimeoutMs > 0 {
		return time.Duration(cfg.V1RequestTimeoutMs) * time.Millisecond
	}
	return 6 * time.Minute // vendor Router.longerTimeout
}

func errString(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

// slog — production log seam (vendor logger.<level> { info }, msg → stdout;
// the vendor writes to the service logger; the runit service captures stdout
// to /var/log/overleaf/project-history-go.log).
type slog struct{ level string }

func (l slog) fn() func(map[string]any, string) {
	return func(info map[string]any, msg string) {
		j, _ := json.Marshal(info)
		fmt.Fprintf(os.Stderr, "project-history %s: %s %s\n", l.level, msg, string(j))
	}
}

// redisRawOf — the parsed update carries its queue raw JSON (ParseDocUpdates
// sets "_raw"), which is what LREM needs (vendor update._raw).
func redisRawOf(u map[string]any) []string {
	if r, ok := u["_raw"].(string); ok && r != "" {
		return []string{r}
	}
	return nil
}

// syncFilesFromChunk — vendor SnapshotManager.getLatestSnapshotFilesForChunk:
// chunk snapshot files, lazy (no content), mapped onto the C16 File shape.
func syncFilesFromChunk(chunk map[string]any) map[string]*syncmanager.File {
	inner, _ := chunk["chunk"].(map[string]any)
	if inner == nil {
		inner = chunk
	}
	hist, _ := inner["history"].(map[string]any)
	if hist == nil {
		hist = inner
	}
	snap, _ := hist["snapshot"].(map[string]any)
	filesRaw, _ := snap["files"].(map[string]any)
	out := map[string]*syncmanager.File{}
	for p, fr := range filesRaw {
		m, ok := fr.(map[string]any)
		if !ok {
			continue
		}
		f := &syncmanager.File{Pathname: p, Content: "", Comments: []*syncmanager.Comment{}, TrackedChanges: []syncmanager.TrackedChange{}}
		if hash, ok := m["hash"].(string); ok {
			f.Hash = hash
		}
		if data, ok := m["data"].(map[string]any); ok {
			if dh, ok := data["hash"].(string); ok {
				f.DataHash = dh
			}
		}
		if mm, ok := m["metadata"].(map[string]any); ok {
			f.Metadata = mm
		}
		if e, ok := m["editable"].(bool); ok {
			f.Editable = e
		}
		out[p] = f
	}
	return out
}

// largeFileDeps — C15 (L1/L2) production wiring.
func largeFileDeps(cfg *config.Config) *largefilemanager.Deps {
	var maxBytes *int64
	if cfg.MaxFileSizeInBytes > 0 {
		v := int64(cfg.MaxFileSizeInBytes)
		maxBytes = &v
	}
	return &largefilemanager.Deps{
		UploadFolder:       cfg.UploadFolder,
		MaxFileSizeInBytes: maxBytes,
		GetBlobHash:        hashmanager.GetBlobHash,
		WriteFile: func(dir, full string, data []byte) error {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			return os.WriteFile(full, data, 0o644)
		},
		Unlink:   os.Remove,
		LogErr:   func(map[string]any, string) {},
		LogDebug: func(map[string]any, string) {},
	}
}
