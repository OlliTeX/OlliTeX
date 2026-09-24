// Package snapshot — PostbackManager + PostbackPromise
// (ports snapshot/push/PostbackManager + PostbackPromise).
package snapshot

import (
	"crypto/rand"
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"ollitex/go/services/gitbridge/giterrors"
)

// postbackTimeoutSeconds ports PostbackPromise.TIMEOUT_SECONDS = 60 * 6.
const postbackTimeoutSeconds = 60 * 6

// PostbackPromise ports push/PostbackPromise.
//
// Java: ReentrantLock + Condition, await(TIMEOUT_SECONDS) per iteration.
// Go: mutex + batch-wake channels (registered once per wait, re-registered
// per iteration — equivalent observable timing). The 360s deadline yields
// PostbackTimeoutException (Severe).
type PostbackPromise struct {
	mu          sync.Mutex
	postbackKey string
	received    bool
	versionID   int
	exception   error
	batch       []chan struct{}
}

func makePromise(key string) *PostbackPromise {
	return &PostbackPromise{postbackKey: key}
}

// WaitPostback ports waitForPostback(). Returns (versionID, error) where
// error is a SnapshotPostException (timeout or an exception posted by
// Overleaf) — mirrors Java's `throws SnapshotPostException`.
// Key returns the promise's postback key (Java PostbackPromise.postbackKey).
func (p *PostbackPromise) Key() string { return p.postbackKey }

// Snapshot is a non-blocking peek at the resolution state (unified wait
// poll: the resolution may land via the in-memory promise OR the shared
// cross-process store, so the waiter checks both).
func (p *PostbackPromise) Snapshot() (done bool, versionID int, exception error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.received {
		return false, 0, nil
	}
	if p.exception != nil {
		return true, 0, p.exception
	}
	return true, p.versionID, nil
}

func (p *PostbackPromise) WaitPostback() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	deadline := time.Now().Add(time.Duration(postbackTimeoutSeconds) * time.Second)
	for !p.received {
		rem := time.Until(deadline)
		if rem <= 0 {
			break
		}
		w := make(chan struct{})
		p.batch = append(p.batch, w)
		p.mu.Unlock()
		select {
		case <-w:
		case <-time.After(rem):
		}
		p.mu.Lock()
	}
	if !p.received {
		return 0, &giterrors.PostbackTimeoutException{TimeoutSeconds: postbackTimeoutSeconds}
	}
	if p.exception != nil {
		return 0, p.exception
	}
	return p.versionID, nil
}

// receivedPostback — success (versionID) or error postback, keyed by
// postbackKey (Java receivedVersionID/receivedException).
func (p *PostbackPromise) receivedPostback(postbackKey string, versionID int, exception error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if postbackKey != p.postbackKey {
		return
	}
	p.versionID = versionID
	p.exception = exception
	p.received = true
	p.signalLocked()
}

func (p *PostbackPromise) signalLocked() {
	for _, w := range p.batch {
		close(w)
	}
	p.batch = nil
}

// CheckPostbackKey (FileHandler).
func (p *PostbackPromise) CheckPostbackKey(postbackKey string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if postbackKey != p.postbackKey {
		return giterrors.InvalidPostbackKeyException{}
	}
	return nil
}

// PostbackManager ports push/PostbackManager.
type PostbackManager struct {
	mu    sync.Mutex
	table map[string]*PostbackPromise
	store PostbackStore // optional cross-process promise state (see below)
}

// PostbackStore is the shared (sqlite-backed) promise state across processes
// (Option 3a: the push runs in a proc-receive HOOK process, while the
// postback POST — which the web app fires at the bridge's own API port —
// lands in the SERVE process. The in-memory promise table is per-process, so
// without a shared store the hook would wait forever (and push hangs).
// *db.SqliteDBStore satisfies this.
type PostbackStore interface {
	PostbackPut(project, key, status string, versionID int, body string)
	PostbackGet(project string) (key, status string, versionID int, body string, found bool)
	PostbackDelete(project string)
}

// timeoutPostback ports PostbackPromise.TIMEOUT_SECONDS (60 * 6 = 360s) —
// Java throws PostbackTimeoutException on expiry; the Go waiter mirrors it.
const timeoutPostback = 360 * time.Second

func NewPostbackManager() *PostbackManager {
	return &PostbackManager{table: map[string]*PostbackPromise{}}
}

// SetStore wires the cross-process promise store (nil-safe; unit fakes that
// don't provide one keep the pure in-memory behavior).
func (m *PostbackManager) SetStore(s PostbackStore) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = s
}

// MakeKeyForProject: key = currentTimeMillis + randomString (130-bit
// base32) — the exact Java key shape
// (`System.currentTimeMillis() + new BigInteger(130, random).toString(32)`).
func (m *PostbackManager) MakeKeyForProject(projectName string) string {
	key := strconv.FormatInt(time.Now().UnixMilli(), 10) + randomString()
	p := makePromise(key)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.table[projectName] = p
	if m.store != nil {
		m.store.PostbackPut(projectName, key, "pending", 0, "")
	}
	return key
}

// randomString ports `new BigInteger(130, random).toString(32)`:
// 130 random bits (unsigned) → base-32 lowercase digits (Java alphabet).
func randomString() string {
	ones := make([]byte, (130+7)/8) // 16 bytes
	if _, err := rand.Read(ones); err != nil {
		panic("snapshot: secure random failed: " + err.Error())
	}
	// BigInteger(bits, rnd) is unsigned; normalize (no sign bit) so Text(32)
	// matches Java's non-negative base-32 representation exactly.
	ones[0] &= 0x7F
	n := new(big.Int).SetBytes(ones)
	return n.Text(32)
}

// PostVersionIDForProject (Overleaf app postback: "up to date").
func (m *PostbackManager) PostVersionIDForProject(projectName string, versionID int, postbackKey string) error {
	m.mu.Lock()
	p, local := m.table[projectName]
	hasStore := m.store != nil
	m.mu.Unlock()
	if local {
		// Same-process push: resolve the live promise (Java 1:1). A key
		// mismatch is silently ignored (Java receivedVersionID), keeping the
		// 200 response — the store row is only cleared on a match so the
		// cross-process waiter (if any) keeps waiting.
		p.receivedPostback(postbackKey, versionID, nil)
		if hasStore && postbackKey == p.Key() {
			m.store.PostbackPut(projectName, postbackKey, "upToDate", versionID, "")
		}
		return nil
	}
	if hasStore {
		// Cross-process push (promise lives in the hook process): the serve
		// process can only resolve it via the shared store. Java semantics:
		// known project promise + matching key → 200; mismatching key →
		// ignored → 200; no promise at all → UnexpectedPostback (409).
		key, status, _, _, found := m.store.PostbackGet(projectName)
		if found {
			if status == "pending" && key == postbackKey {
				m.store.PostbackPut(projectName, key, "upToDate", versionID, "")
			}
			return nil
		}
	}
	return giterrors.UnexpectedPostbackException{}
}

// PostExceptionForProject (Overleaf app postback: error).
func (m *PostbackManager) PostExceptionForProject(projectName string, exception error, postbackKey string) error {
	m.mu.Lock()
	p, local := m.table[projectName]
	hasStore := m.store != nil
	m.mu.Unlock()
	if local {
		p.receivedPostback(postbackKey, 0, exception)
		if hasStore && postbackKey == p.Key() {
			m.store.PostbackPut(projectName, postbackKey, "err:"+exceptionCode(exception), 0, exceptionBody(exception))
		}
		return nil
	}
	if hasStore {
		key, status, _, _, found := m.store.PostbackGet(projectName)
		if found {
			if status == "pending" && key == postbackKey {
				m.store.PostbackPut(projectName, key, "err:"+exceptionCode(exception), 0, exceptionBody(exception))
			}
			return nil
		}
	}
	return giterrors.UnexpectedPostbackException{}
}

// WaitProjectForVersionIdOrThrow: wait on the promise; ALWAYS remove the
// project from the table afterwards (Java: `finally { postbackContentsTable
// .remove(projectName) }`). Unknown project: Java
// `Preconditions.checkNotNull` -> we return UnexpectedPostbackException.
//
// The promise must STAY in the table for the whole wait so a postback
// arriving over HTTP (PostbackHandler -> table lookup) can reach the live
// promise; removal happens only after the wait returns (defer), mirroring
// Java.
func (m *PostbackManager) WaitProjectForVersionIdOrThrow(projectName string) (int, error) {
	m.mu.Lock()
	p, local := m.table[projectName]
	hasStore := m.store != nil
	m.mu.Unlock()
	if !local && !hasStore {
		return 0, giterrors.UnexpectedPostbackException{}
	}
	// Unified wait (Java bound: PostbackPromise TIMEOUT_SECONDS = 360s).
	// The resolution can arrive EITHER via this process's in-memory promise
	// OR via the shared store (cross-process: the postback POST lands in the
	// serve process, the waiter runs in the hook process). Java is one JVM so
	// it only needs the promise; both polls are needed here.
	deadline := time.Now().Add(timeoutPostback)
	cleanupLocal := func() {
		if local {
			m.mu.Lock()
			delete(m.table, projectName)
			m.mu.Unlock()
		}
	}
	for {
		if hasStore {
			if _, status, versionID, body, found := m.store.PostbackGet(projectName); found && status != "pending" {
				m.store.PostbackDelete(projectName)
				cleanupLocal()
				if status == "upToDate" {
					return versionID, nil
				}
				return 0, ExceptionFromCode(strings.TrimPrefix(status, "err:"), []byte(body), true)
			}
		}
		if local {
			if done, versionID, exception := p.Snapshot(); done {
				cleanupLocal() // Java: finally { postbackContentsTable.remove(project) }
				if hasStore {
					if _, _, _, _, found := m.store.PostbackGet(projectName); found {
						m.store.PostbackDelete(projectName)
					}
				}
				if exception != nil {
					return 0, exception
				}
				return versionID, nil
			}
		}
		if time.Now().After(deadline) {
			cleanupLocal()
			if hasStore {
				m.store.PostbackDelete(projectName)
			}
			return 0, &giterrors.PostbackTimeoutException{TimeoutSeconds: int(timeoutPostback / time.Second)}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// CheckPostbackKey (FileHandler).
func (m *PostbackManager) CheckPostbackKey(projectName, postbackKey string) error {
	m.mu.Lock()
	p, local := m.table[projectName]
	hasStore := m.store != nil
	m.mu.Unlock()
	if local {
		return p.CheckPostbackKey(postbackKey)
	}
	if hasStore {
		// Cross-process: the promise lives in the hook process, but the file
		// fetch lands here — the shared pending row with a matching key is a
		// valid postback key (Java: live promise, same check).
		key, status, _, _, found := m.store.PostbackGet(projectName)
		if found && status == "pending" && key == postbackKey {
			return nil
		}
	}
	return giterrors.InvalidPostbackKeyException{}
}

// ---------------------------------------------------------------------------
// exception code mapping (shared store <-> exception types)
// ---------------------------------------------------------------------------

// exceptionCode maps a postback error to its wire code (the value a Java
// PostbackContents would carry: outOfDate | invalidFiles | invalidProject |
// error | <unknown>).
func exceptionCode(e error) string {
	switch e.(type) {
	case *giterrors.OutOfDateException:
		return "outOfDate"
	case *giterrors.InvalidFilesException:
		return "invalidFiles"
	case *giterrors.InvalidProjectException:
		return "invalidProject"
	case *giterrors.UnexpectedErrorException:
		return "error"
	default:
		return "error"
	}
}

// exceptionBody extracts the reconstructable body payload (invalid-files /
// invalid-project error lists) for the shared store; "" otherwise.
func exceptionBody(e error) string {
	if ef, ok := e.(*giterrors.InvalidFilesException); ok {
		b, _ := json.Marshal(map[string]interface{}{"errors": ef.Errors})
		return string(b)
	}
	if ep, ok := e.(*giterrors.InvalidProjectException); ok {
		b, _ := json.Marshal(map[string]interface{}{"errors": ep.Errors})
		return string(b)
	}
	return ""
}

// ExceptionFromCode rebuilds the exception for a stored postback code
// (waiter side; inverse of the handler's code->exception mapping).
func ExceptionFromCode(code string, body []byte, severe bool) error {
	switch code {
	case "outOfDate":
		return &giterrors.OutOfDateException{}
	case "invalidFiles":
		return giterrors.NewInvalidFilesException(body)
	case "invalidProject":
		return giterrors.NewInvalidProjectException(body)
	case "error":
		return &giterrors.UnexpectedErrorException{}
	default:
		return &giterrors.UnexpectedPostbackException{}
	}
}
