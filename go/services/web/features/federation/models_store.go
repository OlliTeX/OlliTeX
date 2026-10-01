// S2 continued — MapStore (peers/anchors/export), MongoStore production
// impl, boot-time index creation (04 §9), and the User.federation /
// ProjectInvite.federated subdoc read helpers (04 §1/§3).
package federation

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func matchesDirection(direction PeerDirection) func(peerDir string) bool {
	switch direction {
	case PeerDirectionInbound:
		return func(dir string) bool { return dir == "inbound" || dir == "both" }
	case PeerDirectionOutbound:
		return func(dir string) bool { return dir == "outbound" || dir == "both" }
	default:
		return func(string) bool { return true }
	}
}

// ---------- MapStore: peers, anchors, export ----------

func (s *MapStore) SavePeer(ctx context.Context, p *FederationPeer) error {
	if p.Origin == "" {
		return Errorf("federation: peer origin required (unique, 04 §2)")
	}
	s.mu.Lock()
	s.peers[p.Origin] = *p
	s.mu.Unlock()
	return nil
}

func (s *MapStore) PeerByOrigin(_ context.Context, origin string) (*FederationPeer, error) {
	s.mu.RLock()
	p, ok := s.peers[origin]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	cp := p
	return &cp, nil
}

func (s *MapStore) AllPeers(_ context.Context) ([]*FederationPeer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*FederationPeer
	for _, p := range s.peers {
		cp := p
		out = append(out, &cp)
	}
	return out, nil
}

func (s *MapStore) ApprovedPeers(_ context.Context, direction PeerDirection) ([]*FederationPeer, error) {
	match := matchesDirection(direction)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*FederationPeer
	for _, p := range s.peers {
		if p.Status != "approved" {
			continue
		}
		if match(p.Direction) {
			cp := p
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *MapStore) RevokePeer(_ context.Context, origin, _, _ string, _ bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.peers[origin]
	if !ok {
		return ErrNotFound
	}
	p.Status = "revoked"
	s.peers[origin] = p
	return nil
}

func (s *MapStore) SaveExportGrant(_ context.Context, g *FederationExportGrant) error {
	id := g.ID
	if id.IsZero() {
		id = bson.NewObjectID()
		g.ID = id
	}
	s.mu.Lock()
	s.exportGrants[id.Hex()] = *g
	s.mu.Unlock()
	return nil
}

func (s *MapStore) ExportGrantsForProject(_ context.Context, projectID string) ([]*FederationExportGrant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*FederationExportGrant
	for _, g := range s.exportGrants {
		if g.ProjectID == projectID {
			cp := g
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *MapStore) ActiveExportGrants(_ context.Context) ([]*FederationExportGrant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*FederationExportGrant
	for _, g := range s.exportGrants {
		if g.Status == "active" {
			cp := g
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *MapStore) SaveTrustAnchor(_ context.Context, a *FederationTrustAnchor) error {
	if a.EntityID == "" {
		return Errorf("federation: trust anchor entityId required (02 §3)")
	}
	s.mu.Lock()
	s.anchors[a.EntityID] = *a
	s.mu.Unlock()
	return nil
}

func (s *MapStore) TrustAnchorByEntityId(_ context.Context, entityID string) (*FederationTrustAnchor, error) {
	s.mu.RLock()
	a, ok := s.anchors[entityID]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	cp := a
	return &cp, nil
}

func (s *MapStore) AllTrustAnchors(_ context.Context) ([]*FederationTrustAnchor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*FederationTrustAnchor
	for _, a := range s.anchors {
		cp := a
		out = append(out, &cp)
	}
	return out, nil
}

func (s *MapStore) DeleteTrustAnchor(_ context.Context, entityID string) error {
	s.mu.Lock()
	delete(s.anchors, entityID)
	s.mu.Unlock()
	return nil
}

// ---------- MongoStore (production) ----------

// MongoStore — production Mongo-backed store.
type MongoStore struct{ DB *mongo.Database }

var _ Store = (*MongoStore)(nil)

func NewMongoStore(db *mongo.Database) *MongoStore { return &MongoStore{DB: db} }

func (s *MongoStore) SaveKey(ctx context.Context, k *FederationKey) error {
	defaultKeyState(k)
	if err := k.validateStates(); err != nil {
		return err
	}
	if k.ID.IsZero() {
		k.ID = bson.NewObjectID()
	}
	_, err := s.DB.Collection(ColFederationKey).UpdateOne(ctx,
		bson.D{{Key: "purpose", Value: k.Purpose}, {Key: "kid", Value: k.Kid}},
		bson.D{{Key: "$set", Value: k}},
		options.UpdateOne().SetUpsert(true))
	return err
}

func (s *MongoStore) KeyByPurposeAndKid(ctx context.Context, purpose, kid string) (*FederationKey, error) {
	var k FederationKey
	err := s.DB.Collection(ColFederationKey).FindOne(ctx,
		bson.D{{Key: "purpose", Value: purpose}, {Key: "kid", Value: kid}}).Decode(&k)
	if IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (s *MongoStore) ActiveKey(ctx context.Context, purpose string) (*FederationKey, error) {
	var k FederationKey
	err := s.DB.Collection(ColFederationKey).FindOne(ctx,
		bson.D{{Key: "purpose", Value: purpose}, {Key: "state", Value: "active"}}).Decode(&k)
	if IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (s *MongoStore) RetiringKeys(ctx context.Context, purpose string) ([]*FederationKey, error) {
	c := s.DB.Collection(ColFederationKey)
	cur, err := c.Find(ctx, bson.D{{Key: "purpose", Value: purpose}, {Key: "state", Value: "retiring"}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []*FederationKey
	for cur.Next(ctx) {
		k := FederationKey{}
		if err := cur.Decode(&k); err != nil {
			return nil, err
		}
		out = append(out, &k)
	}
	return out, cur.Err()
}

func (s *MongoStore) AllKeys(ctx context.Context, purpose string) ([]*FederationKey, error) {
	c := s.DB.Collection(ColFederationKey)
	cur, err := c.Find(ctx, bson.D{{Key: "purpose", Value: purpose}},
		options.Find().SetSort(bson.D{{Key: "publishedAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []*FederationKey
	for cur.Next(ctx) {
		k := FederationKey{}
		if err := cur.Decode(&k); err != nil {
			return nil, err
		}
		out = append(out, &k)
	}
	return out, cur.Err()
}

func (s *MongoStore) RotateKey(ctx context.Context, oldKid, newKid string, purpose string, now time.Time) error {
	nowSec := now.Unix()
	if _, err := s.DB.Collection(ColFederationKey).UpdateOne(ctx,
		bson.D{{Key: "purpose", Value: purpose}, {Key: "kid", Value: oldKid}, {Key: "state", Value: "active"}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "state", Value: "retiring"}, {Key: "stateChangedAt", Value: nowSec}}}}); err != nil {
		return err
	}
	if _, err := s.DB.Collection(ColFederationKey).UpdateOne(ctx,
		bson.D{{Key: "purpose", Value: purpose}, {Key: "kid", Value: newKid}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "state", Value: "active"}, {Key: "stateChangedAt", Value: nowSec}}}}); err != nil {
		return err
	}
	return nil
}

func (s *MongoStore) SavePeer(ctx context.Context, p *FederationPeer) error {
	if p.Origin == "" {
		return Errorf("federation: peer origin required (unique, 04 §2)")
	}
	if p.ID.IsZero() {
		p.ID = bson.NewObjectID()
	}
	_, err := s.DB.Collection(ColFederationPeer).UpdateOne(ctx,
		bson.D{{Key: "origin", Value: p.Origin}},
		bson.D{{Key: "$set", Value: p}},
		options.UpdateOne().SetUpsert(true))
	return err
}

func (s *MongoStore) PeerByOrigin(ctx context.Context, origin string) (*FederationPeer, error) {
	var p FederationPeer
	err := s.DB.Collection(ColFederationPeer).FindOne(ctx,
		bson.D{{Key: "origin", Value: origin}}).Decode(&p)
	if IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *MongoStore) AllPeers(ctx context.Context) ([]*FederationPeer, error) {
	c := s.DB.Collection(ColFederationPeer)
	cur, err := c.Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []*FederationPeer
	for cur.Next(ctx) {
		p := FederationPeer{}
		if err := cur.Decode(&p); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, cur.Err()
}

func (s *MongoStore) ApprovedPeers(ctx context.Context, direction PeerDirection) ([]*FederationPeer, error) {
	match := matchesDirection(direction)
	c := s.DB.Collection(ColFederationPeer)
	cur, err := c.Find(ctx, bson.D{{Key: "status", Value: "approved"}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []*FederationPeer
	for cur.Next(ctx) {
		p := FederationPeer{}
		if err := cur.Decode(&p); err != nil {
			return nil, err
		}
		if match(p.Direction) {
			out = append(out, &p)
		}
	}
	return out, cur.Err()
}

func (s *MongoStore) RevokePeer(ctx context.Context, origin, reason, revokedAt string, kill bool) error {
	_ = reason
	_ = revokedAt
	_, err := s.DB.Collection(ColFederationPeer).UpdateOne(ctx,
		bson.D{{Key: "origin", Value: origin}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "status", Value: "revoked"},
			{Key: "killOutstandingCodes", Value: kill},
		}}},
	)
	return err
}

func (s *MongoStore) SaveTrustAnchor(ctx context.Context, a *FederationTrustAnchor) error {
	if a.EntityID == "" {
		return Errorf("federation: trust anchor entityId required (02 §3)")
	}
	if a.ID.IsZero() {
		a.ID = bson.NewObjectID()
	}
	_, err := s.DB.Collection(ColFederationTrustAnchor).UpdateOne(ctx,
		bson.D{{Key: "entityId", Value: a.EntityID}},
		bson.D{{Key: "$set", Value: a}},
		options.UpdateOne().SetUpsert(true))
	return err
}

func (s *MongoStore) TrustAnchorByEntityId(ctx context.Context, entityID string) (*FederationTrustAnchor, error) {
	var a FederationTrustAnchor
	err := s.DB.Collection(ColFederationTrustAnchor).FindOne(ctx,
		bson.D{{Key: "entityId", Value: entityID}}).Decode(&a)
	if IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *MongoStore) AllTrustAnchors(ctx context.Context) ([]*FederationTrustAnchor, error) {
	c := s.DB.Collection(ColFederationTrustAnchor)
	cur, err := c.Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []*FederationTrustAnchor
	for cur.Next(ctx) {
		a := FederationTrustAnchor{}
		if err := cur.Decode(&a); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, cur.Err()
}

func (s *MongoStore) DeleteTrustAnchor(ctx context.Context, entityID string) error {
	_, err := s.DB.Collection(ColFederationTrustAnchor).DeleteOne(ctx,
		bson.D{{Key: "entityId", Value: entityID}})
	return err
}

func (s *MongoStore) SaveExportGrant(ctx context.Context, g *FederationExportGrant) error {
	if g.ID.IsZero() {
		g.ID = bson.NewObjectID()
	}
	_, err := s.DB.Collection(ColFederationExportGrant).UpdateOne(ctx,
		bson.D{{Key: "_id", Value: g.ID}},
		bson.D{{Key: "$set", Value: g}},
		options.UpdateOne().SetUpsert(true))
	return err
}

func (s *MongoStore) ExportGrantsForProject(ctx context.Context, projectID string) ([]*FederationExportGrant, error) {
	c := s.DB.Collection(ColFederationExportGrant)
	cur, err := c.Find(ctx, bson.D{{Key: "projectId", Value: projectID}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []*FederationExportGrant
	for cur.Next(ctx) {
		g := FederationExportGrant{}
		if err := cur.Decode(&g); err != nil {
			return nil, err
		}
		out = append(out, &g)
	}
	return out, cur.Err()
}

func (s *MongoStore) ActiveExportGrants(ctx context.Context) ([]*FederationExportGrant, error) {
	c := s.DB.Collection(ColFederationExportGrant)
	cur, err := c.Find(ctx, bson.D{{Key: "status", Value: "active"}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []*FederationExportGrant
	for cur.Next(ctx) {
		g := FederationExportGrant{}
		if err := cur.Decode(&g); err != nil {
			return nil, err
		}
		out = append(out, &g)
	}
	return out, cur.Err()
}
