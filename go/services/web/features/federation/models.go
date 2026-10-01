// S2 models — the four federation collections + the mirror/invite subdocs
// (Node `modules/federation/app/models/*.mjs`, plan 04 §1–§4), oracle-pinned.
//
// Collection shapes (from the Node Mongoose schemas — see overleaf-fed
// `modules/federation/app/models/README.md`, 04 §4):
//
//	FederationKey        federationKeys          02 §5 keystore (pairwise keys)
//	FederationPeer       federationPeers         04 §2 peer ledger
//	FederationTrustAnchor federationTrustAnchors 02 §3 TA pin
//	FederationExportGrant federationExportGrants 09 §3.1 export ledger
//
// Parity gotcha (oracle): FederationKey timestamps are EPOCH SECONDS
// (JS Number), NOT Date; FederationPeer timestamps ARE Date (mongoose
// default). The wire/audit payload reads the raw values — keep the Go
// types per-field, don't normalize.
//
// Store seam (Go repo convention, e.g. features/emailtemplates):
// MapStore for unit tests; NewMongoStore for production wiring.
//
// Indexes (04 §9, Node migrations 20260721*): autoIndex is off app-wide;
// indexes are created explicitly:
//
//	federationKeys:        unique {purpose, kid} + {purpose, state}
//	federationPeers:       unique {origin} + {status, direction}
//	federationTrustAnchors: unique {entityId}
//	federationExportGrants: one-phase sweep {status, expiresAt}
//	users:                 partial unique {federation.origin,
//	                       federation.localName} (partialFilter {federation:
//	                       {$exists: true}}) — created here, 04 §1
package federation

import (
	"context"
	"sort"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// ---------- collection names (04 §1 table, oracle) ----------

const (
	ColFederationKey         = "federationKeys"
	ColFederationPeer        = "federationPeers"
	ColFederationTrustAnchor = "federationTrustAnchors"
	ColFederationExportGrant = "federationExportGrants"
	ColProjectAuditLog       = "projectAuditLogEntries" // reused via util (04 §8)
)

// ---------- FederationKey (02 §5) ----------

// FederationKey — ES256 signing key row (02 §5 keystore).
//
// purpose: 'federation' (leaf EC + client assertions; public half is
// pinned by peers, TOFU) | 'oidc' (id_tokens / tokens; served at
// /federation/oidc/jwks, NOT pinned — A-side re-fetches at runtime).
//
// state (02 §5 states, oracle):
//
//	published → active → retiring → revoked
//
//	 published : public half served (leaf jwks / historical endpoint)
//	 active    : the single signing key
//	 retiring  : no longer signs; verification continues for the grace
//	              window (Settings.keyRotationGraceDays, default 14)
//	 revoked   : removed from the historical set (after grace)
//
// Private halves are stored (JWK `d`) so rotation works: the leaf must
// sign with a durable key and the key outlives everything else.
type FederationKey struct {
	ID             bson.ObjectID `bson:"_id,omitempty"`
	Purpose        string        `bson:"purpose"` // 'federation' | 'oidc'
	Kid            string        `bson:"kid"`
	Algorithm      string        `bson:"algorithm"`  // default 'ES256'
	PublicKey      *JWK          `bson:"publicKey"`  // {kty, kid, alg, use?, x, y}
	PrivateKey     *JWK          `bson:"privateKey"` // full incl. `d`
	State          string        `bson:"state"`      // default 'published'
	ExpiresAt      int64         `bson:"expiresAt,omitempty"`
	RevokedAt      int64         `bson:"revokedAt,omitempty"`
	RevokeReason   string        `bson:"revokeReason,omitempty"`
	PublishedAt    int64         `bson:"publishedAt"`    // epoch SECONDS (oracle)
	StateChangedAt int64         `bson:"stateChangedAt"` // epoch SECONDS (oracle)
}

// StateName — the four keystore states (02 §5).
type StateName string

const (
	StatePublished StateName = "published"
	StateActive    StateName = "active"
	StateRetiring  StateName = "retiring"
	StateRevoked   StateName = "revoked"
)

// defaultKeyState — mirrors the Node Mongoose `default: 'published'` on
// FederationKey.state. An empty state on Save is coerced before validation.
func defaultKeyState(k *FederationKey) {
	if k.State == "" {
		k.State = "published"
	}
	if k.Algorithm == "" {
		k.Algorithm = "ES256"
	}
}

func (k *FederationKey) validateStates() error {
	switch StateName(k.State) {
	case StatePublished, StateActive, StateRetiring, StateRevoked:
		return nil
	}
	return Errorf("federation: invalid key state %q", k.State)
}

// JWK — a JSON Web Key. `D` (EC private scalar) is present only on
// private halves; PublicJwks drops keys carrying it (06 §6).
type JWK struct {
	Kty string `json:"kty,omitempty"`
	Kid string `json:"kid,omitempty"`
	Alg string `json:"alg,omitempty"`
	Use string `json:"use,omitempty"`
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
	D   string `json:"d,omitempty"` // PRIVATE (EC scalar); never served
}

// IsPrivateJWK — true when a private field is present (redact.go mirrors
// this list).
func (k JWK) IsPrivateJWK() bool {
	return k.D != ""
}

// PublicHalf — a copy with all private fields dropped (06 §6: drop, not
// redact). Returned key serves as a public JWK (leaf / JWKS).
func (k JWK) PublicHalf() JWK {
	return JWK{Kty: k.Kty, Kid: k.Kid, Alg: k.Alg, Use: k.Use, Crv: k.Crv, X: k.X, Y: k.Y, N: k.N, E: k.E}
}

// ---------- FederationPeer (04 §2) ----------

// FederationPeer — another overleaf-cep instance our admins (partially)
// trust (04 §2). In OIDF terms the row stores the anchor (the federation
// public JWK pinned for this peer); in institutional mode also the OIDF
// explicit-registration result.
//
// Trust direction (04 §2, plan v2):
//
//	'outbound' = we pin them for calls they make to us (S2S receive +
//		grants they may mint against us when direction inbound/both)
//
// The row is the single source for:
//   - the oidc-provider clients[] reconstruction (approved, inbound/both)
//   - S2S authorize-invite/invited/revoke approvals (approved status)
//   - the trust-anchor set for the OIDC role (institutional registered
//     anchors)
type FederationPeer struct {
	ID                   bson.ObjectID     `bson:"_id,omitempty"`
	Origin               string            `bson:"origin"` // home FQDN, unique
	DisplayName          string            `bson:"displayName,omitempty"`
	EntityID             string            `bson:"entityId,omitempty"`
	Mode                 string            `bson:"mode"`                   // 'pairwise' | 'institutional'
	Registration         *PeerRegistration `bson:"registration,omitempty"` // institutional only (02 §4)
	AnchorJwks           string            `bson:"anchorJwks,omitempty"`   // JSON single JWK
	Kid                  string            `bson:"kid,omitempty"`
	Thumbprint           string            `bson:"thumbprint,omitempty"`
	Direction            string            `bson:"direction,omitempty"` // outbound|inbound|both
	Status               string            `bson:"status"`              // pending|approved|revoked (default pending)
	FederatedAt          time.Time         `bson:"federatedAt"`
	ApprovedAt           *time.Time        `bson:"approvedAt,omitempty"`
	LastTrustRefreshAt   *time.Time        `bson:"lastTrustRefreshAt,omitempty"`
	KillOutstandingCodes bool              `bson:"killOutstandingCodes"` // S2S revoke (03 §4.3)
}

// PeerRegistration — institutional mode only (02 §4): explicit-registration
// statement result. Pairwise mode has no registration statement — pinning
// IS establishment (02 §3).
type PeerRegistration struct {
	// OIDF client ID (urn:oidfed:federation:child:<entityId>)
	ChildClientID string `bson:"clientId"`
	// exp of the registration statement (epoch seconds)
	ExpiresAt int64 `bson:"expiresAt"`
	// 02 §4 explicitlyRegister result: how long the trust chain (up to the
	// trust anchor) is valid (epoch seconds)
	TrustChainExpiresAt int64 `bson:"trustChainExpiresAt"`
	// audit/replay only — the anchor pin itself is ground truth
	ChildAnchorJwks string `bson:"childAnchorJwks,omitempty"`
	ChildAnchorKid  string `bson:"childAnchorKid,omitempty"`
}

// PeerStatus — pending | approved | revoked (04 §2).
type PeerStatus string

const (
	PeerPending  PeerStatus = "pending"
	PeerApproved PeerStatus = "approved"
	PeerRevoked  PeerStatus = "revoked"
)

// PeerMode — pairwise | institutional (04 §2).
type PeerMode string

const (
	PeerModePairwise      PeerMode = "pairwise"
	PeerModeInstitutional PeerMode = "institutional"
)

// PeerDirection — outbound | inbound | both (04 §2).
type PeerDirection string

const (
	PeerDirectionOutbound PeerDirection = "outbound"
	PeerDirectionInbound  PeerDirection = "inbound"
	PeerDirectionBoth     PeerDirection = "both"
)

// ---------- FederationTrustAnchor (02 §3) ----------

// FederationTrustAnchor — an institutional trust anchor (TA) row (02 §3,
// 04 §2, 07 §P3). A row is what an admin pinned as "known-good
// institutional root":
//
//   - entityId: the TA's OIDF entity id (https://<FQDN>)
//   - jwks:     the TA's public JWK set (TOFU — the pin IS the trust
//     decision; the raw JWS is never stored, 04 §2)
//
// Used at PEER PIN TIME (institutional peers): the leaf's
// authority_hints must terminate in a TA that has a row here, and the
// subordinate statement chain is re-verified offline.
type FederationTrustAnchor struct {
	ID          bson.ObjectID  `bson:"_id,omitempty"`
	EntityID    string         `bson:"entityId"` // unique
	DisplayName string         `bson:"displayName,omitempty"`
	JWKS        *PublicJwksDoc `bson:"jwks"` // public halves only
	PinnedAt    time.Time      `bson:"pinnedAt"`
}

// PublicJwksDoc — a JWKS doc (public halves only).
type PublicJwksDoc struct {
	Keys []JWK `json:"keys"`
}

// ---------- FederationExportGrant (09 §3.1) ----------

// FederationExportGrant — B-side export grant ledger (09 §3.1), the MIRROR
// of the consent grant: one row per exported project, holding the minted
// PAT id so the sweep can remove the db.oauthAccessTokens doc when the
// grant is revoked/expired.
//
// NOT the consent itself: the consent lives in the oidc-provider Grant doc
// (30-day TTL, Redis). This row is the durable audit + sweep anchor.
type FederationExportGrant struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	Owner         bson.ObjectID `bson:"owner"`
	ProjectID     string        `bson:"projectId"`
	HomeOrigin    string        `bson:"homeOrigin"`
	Scope         string        `bson:"scope"`
	PatHashPrefix string        `bson:"patHashPrefix"`
	PatID         string        `bson:"patId"`
	ExpiresAt     time.Time     `bson:"expiresAt"`
	Status        string        `bson:"status"` // 'active' | 'revoked' | 'expired'
	CreatedAt     time.Time     `bson:"createdAt"`
}

// ExportGrantStatus — active | revoked | expired (09 §3.1).
type ExportGrantStatus string

const (
	ExportActive  ExportGrantStatus = "active"
	ExportRevoked ExportGrantStatus = "revoked"
	ExportExpired ExportGrantStatus = "expired"
)

// ---------- Store seam (Go repo convention) ----------

// Store — read/write surface over the four collections.
type Store interface {
	// Keys (federationKeys)
	SaveKey(ctx context.Context, key *FederationKey) error
	KeyByPurposeAndKid(ctx context.Context, purpose, kid string) (*FederationKey, error)
	ActiveKey(ctx context.Context, purpose string) (*FederationKey, error)
	RetiringKeys(ctx context.Context, purpose string) ([]*FederationKey, error)
	AllKeys(ctx context.Context, purpose string) ([]*FederationKey, error)
	RotateKey(ctx context.Context, oldKid, newKid string, purpose string, now time.Time) error

	// Peers (federationPeers)
	SavePeer(ctx context.Context, peer *FederationPeer) error
	PeerByOrigin(ctx context.Context, origin string) (*FederationPeer, error)
	AllPeers(ctx context.Context) ([]*FederationPeer, error)
	ApprovedPeers(ctx context.Context, direction PeerDirection) ([]*FederationPeer, error)
	RevokePeer(ctx context.Context, origin string, reason, revokedAt string, killOutstandingCodes bool) error

	// Trust anchors (federationTrustAnchors)
	SaveTrustAnchor(ctx context.Context, anchor *FederationTrustAnchor) error
	TrustAnchorByEntityId(ctx context.Context, entityID string) (*FederationTrustAnchor, error)
	AllTrustAnchors(ctx context.Context) ([]*FederationTrustAnchor, error)
	DeleteTrustAnchor(ctx context.Context, entityID string) error

	// Export grants (federationExportGrants)
	SaveExportGrant(ctx context.Context, grant *FederationExportGrant) error
	ExportGrantsForProject(ctx context.Context, projectID string) ([]*FederationExportGrant, error)
	ActiveExportGrants(ctx context.Context) ([]*FederationExportGrant, error)
}

var _ Store = (*MapStore)(nil)

// ---------- MapStore (unit tests / S11–S13 logic) ----------

type MapStore struct {
	mu           sync.RWMutex
	keys         map[mapKey]FederationKey
	peers        map[string]FederationPeer
	anchors      map[string]FederationTrustAnchor
	exportGrants map[string]FederationExportGrant
}

type mapKey struct{ purpose, kid string }

func NewMapStore() *MapStore {
	return &MapStore{
		keys:         map[mapKey]FederationKey{},
		peers:        map[string]FederationPeer{},
		anchors:      map[string]FederationTrustAnchor{},
		exportGrants: map[string]FederationExportGrant{},
	}
}

func (s *MapStore) SaveKey(_ context.Context, k *FederationKey) error {
	defaultKeyState(k)
	if err := k.validateStates(); err != nil {
		return err
	}
	s.mu.Lock()
	s.keys[mapKey{k.Purpose, k.Kid}] = *k
	s.mu.Unlock()
	return nil
}

func (s *MapStore) KeyByPurposeAndKid(_ context.Context, purpose, kid string) (*FederationKey, error) {
	s.mu.RLock()
	k, ok := s.keys[mapKey{purpose, kid}]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	cp := k
	return &cp, nil
}

func (s *MapStore) ActiveKey(_ context.Context, purpose string) (*FederationKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.keys {
		if k.Purpose == purpose && k.State == "active" {
			cp := k
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (s *MapStore) RetiringKeys(_ context.Context, purpose string) ([]*FederationKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*FederationKey
	for _, k := range s.keys {
		if k.Purpose == purpose && k.State == "retiring" {
			cp := k
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *MapStore) AllKeys(_ context.Context, purpose string) ([]*FederationKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*FederationKey
	for _, k := range s.keys {
		if k.Purpose == purpose {
			cp := k
			out = append(out, &cp)
		}
	}
	// oracle 02 §5 / keystore.mjs `.sort({ publishedAt: 1 })`.
	sort.SliceStable(out, func(i, j int) bool { return out[i].PublishedAt < out[j].PublishedAt })
	return out, nil
}

func (s *MapStore) RotateKey(_ context.Context, oldKid, newKid string, purpose string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.keys[mapKey{purpose, oldKid}]
	if !ok {
		return ErrNotFound
	}
	old.State = "retiring"
	s.keys[mapKey{purpose, oldKid}] = old
	new, ok := s.keys[mapKey{purpose, newKid}]
	if !ok {
		return Errorf("federation: rotation target %s/%s not found", purpose, newKid)
	}
	new.State = "active"
	s.keys[mapKey{purpose, newKid}] = new
	return nil
}
