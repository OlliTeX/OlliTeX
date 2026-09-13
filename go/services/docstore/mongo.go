package docstore

// mongo.go — the real Store (services/docstore MongoManager 1:1) on top of
// go.mongodb.org/mongo-driver, hitting the shared `docs` collection.
//
// Multi-value ID lists in these flows are single-element filters (no $in
// needed); the chat conversion learned that bare array filter values match
// nothing on mongod 8.3.7, so equality filters are kept explicit here too.

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type mongoStore struct {
	docs        *mongo.Collection // primary read preference
	secondary   *mongo.Collection // readPreference: secondary (the Node 'secondary' readpref)
	secondaries bool
}

func NewMongoStore(ctx context.Context, uri, database string, hasSecondaries bool, logf func(string, ...any)) (*mongoStore, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}
	db := client.Database(database)
	// Node READ_PREFERENCE_SECONDARY: 'secondary' when hasSecondaries,
	// 'secondaryPreferred' otherwise.
	secMode := readpref.SecondaryPreferred()
	if hasSecondaries {
		secMode = readpref.Secondary()
	}
	return &mongoStore{
		docs:        db.Collection("docs"),
		secondary:   db.Collection("docs", options.Collection().SetReadPreference(secMode)),
		secondaries: hasSecondaries,
	}, nil
}

// col picks the handle for the requested read preference (Node applies
// READ_PREFERENCE_SECONDARY whenever useSecondary is set).
func (s *mongoStore) col(useSecondary bool) *mongo.Collection {
	if useSecondary {
		return s.secondary
	}
	return s.docs
}

// rawDoc is the flexible `docs` cursor; pointer fields preserve absent vs
// zero (views include a key only when != null, Node _buildDocView).
type rawDoc struct {
	ID             primitive.ObjectID `bson:"_id,omitempty"`
	ProjectID      primitive.ObjectID `bson:"project_id,omitempty"`
	Lines          *[]string          `bson:"lines,omitempty"`
	Rev            *int64             `bson:"rev,omitempty"`
	Version        *int64             `bson:"version,omitempty"`
	Ranges         any                `bson:"ranges,omitempty"`
	Deleted        *bool              `bson:"deleted,omitempty"`
	InS3           *bool              `bson:"inS3,omitempty"`
	Name           *string            `bson:"name,omitempty"`
	DeletedAt      *time.Time         `bson:"deletedAt,omitempty"`
	ArchivingUntil *time.Time         `bson:"archivingUntil,omitempty"`
}

func (d *rawDoc) toDoc() *Doc {
	if d == nil {
		return nil
	}
	out := &Doc{
		ID:             d.ID.Hex(),
		Ranges:         normalizeTree(d.Ranges), // driver gives primitive.D/A; normalize to the neutral tree
		ArchivingUntil: d.ArchivingUntil,
	}
	if !d.ProjectID.IsZero() {
		out.ProjectID = d.ProjectID.Hex()
	}
	if d.Lines != nil {
		out.Lines = d.Lines
	}
	if d.Rev != nil {
		out.Rev = d.Rev
	}
	if d.Version != nil {
		out.Version = d.Version
	}
	if d.Deleted != nil {
		out.Deleted = d.Deleted
	}
	if d.InS3 != nil {
		out.InS3 = d.InS3
	}
	if d.Name != nil {
		out.Name = d.Name
	}
	if d.DeletedAt != nil {
		out.DeletedAt = d.DeletedAt
	}
	return out
}

// projection builds the inclusion projection (Node projection semantics;
// Node's legacy driver keeps _id in inclusion projections — live verified).
func projection(p docProj) bson.D {
	d := bson.D{}
	if p.Lines {
		d = append(d, bson.E{Key: "lines", Value: 1})
	}
	if p.Rev {
		d = append(d, bson.E{Key: "rev", Value: 1})
	}
	if p.Version {
		d = append(d, bson.E{Key: "version", Value: 1})
	}
	if p.Ranges {
		d = append(d, bson.E{Key: "ranges", Value: 1})
	}
	if p.Deleted {
		d = append(d, bson.E{Key: "deleted", Value: 1})
	}
	if p.InS3 {
		d = append(d, bson.E{Key: "inS3", Value: 1})
	}
	if p.Name {
		d = append(d, bson.E{Key: "name", Value: 1})
	}
	if p.DeletedAt {
		d = append(d, bson.E{Key: "deletedAt", Value: 1})
	}
	return d
}

// FindDoc = MongoManager.findDoc 1:1.
func (s *mongoStore) FindDoc(ctx context.Context, projectID, docID string, p docProj, useSecondary bool) (*Doc, error) {
	oid := oidOf(projectID)
	doid := oidOf(docID)
	filter := bson.D{{Key: "_id", Value: doid}, {Key: "project_id", Value: oid}}
	opts := options.FindOne()
	if proj := projection(p); len(proj) > 0 {
		opts.SetProjection(proj)
	}
	var raw rawDoc
	err := s.col(useSecondary).FindOne(ctx, filter, opts).Decode(&raw)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	doc := raw.toDoc()
	// Node: if (doc && projection.version && !doc.version) doc.version = 0
	if p.Version && (doc.Version == nil || *doc.Version == 0) {
		zero := int64(0)
		doc.Version = &zero
	}
	return doc, nil
}

func (s *mongoStore) idsOf(docs []*Doc) []string {
	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	return ids
}

// ProjectDocs = MongoManager.getProjectsDocs + the archive id-list queries.
func (s *mongoStore) ProjectDocs(ctx context.Context, projectID string, o ProjectDocOpts) ([]*Doc, error) {
	filter := bson.D{{Key: "project_id", Value: oidOf(projectID)}}
	if o.NonArchivedOnly {
		filter = append(filter, bson.E{Key: "inS3", Value: bson.D{{Key: "$ne", Value: true}}})
	}
	if o.ArchivedOnly {
		filter = append(filter, bson.E{Key: "inS3", Value: true})
	}
	// Node getProjectsDocs: !includeDeleted ⇒ explicit {deleted: {$ne: true}}
	if !o.IncludeDeleted {
		filter = append(filter, bson.E{Key: "deleted", Value: bson.D{{Key: "$ne", Value: true}}})
	}
	opts := options.Find()
	proj := bson.D{}
	if o.WantLines {
		proj = append(proj, bson.E{Key: "lines", Value: 1})
	}
	if o.WantRev {
		proj = append(proj, bson.E{Key: "rev", Value: 1})
	}
	if o.WantVersion {
		proj = append(proj, bson.E{Key: "version", Value: 1})
	}
	if o.WantRanges {
		proj = append(proj, bson.E{Key: "ranges", Value: 1})
	}
	if len(proj) > 0 {
		opts.SetProjection(proj)
	}
	if o.Limit > 0 {
		opts.SetLimit(int64(o.Limit))
	}
	cur, err := s.col(o.UseSecondary).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	var raws []rawDoc
	if err := cur.All(ctx, &raws); err != nil {
		return nil, err
	}
	docs := make([]*Doc, 0, len(raws))
	for i := range raws {
		docs = append(docs, raws[i].toDoc())
	}
	return docs, nil
}

// DeletedDocs = MongoManager.getProjectsDeletedDocs 1:1.
func (s *mongoStore) DeletedDocs(ctx context.Context, projectID string, limit int) ([]*Doc, error) {
	opts := options.Find().
		SetProjection(bson.D{{Key: "name", Value: 1}, {Key: "deletedAt", Value: 1}}).
		SetSort(bson.D{{Key: "deletedAt", Value: -1}}).
		SetLimit(int64(limit))
	filter := bson.D{
		{Key: "project_id", Value: oidOf(projectID)},
		{Key: "deleted", Value: true},
	}
	cur, err := s.docs.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	var raws []rawDoc
	if err := cur.All(ctx, &raws); err != nil {
		return nil, err
	}
	docs := make([]*Doc, 0, len(raws))
	for i := range raws {
		docs = append(docs, raws[i].toDoc())
	}
	return docs, nil
}

func (s *mongoStore) GetDocRev(ctx context.Context, docID string) (int64, bool, error) {
	var raw rawDoc
	err := s.docs.FindOne(ctx, bson.D{{Key: "_id", Value: oidOf(docID)}},
		options.FindOne().SetProjection(bson.D{{Key: "rev", Value: 1}})).Decode(&raw)
	if err != nil {
		return 0, false, err
	}
	if raw.Rev == nil {
		return 0, false, nil
	}
	return *raw.Rev, true, nil
}

// treeToBSON converts a neutral tree back into BSON values for writes
// (objectIDHex → ObjectID, jsDate → Date, rest passes through).
func treeToBSON(v any) any {
	switch t := v.(type) {
	case objectIDHex:
		if id, err := primitive.ObjectIDFromHex(string(t)); err == nil {
			return id
		}
		return primitive.ObjectID{}
	case jsDate:
		return time.Time(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = treeToBSON(e)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = treeToBSON(e)
		}
		return out
	case []string:
		return t
	default:
		return v
	}
}

// UpsertDoc = MongoManager.upsertIntoDocCollection 1:1 (the $literal
// pipeline form is kept byte-for-byte: one stage per $set field in
// lines→ranges→version→[rev], then $unset inS3; insert path is
// {_id, project_id, rev:1, ...updates}).
func (s *mongoStore) UpsertDoc(ctx context.Context, projectID, docID string, previousRev int64, u WriteUpdates) error {
	oid := oidOf(projectID)
	doid := oidOf(docID)
	if previousRev > 0 {
		stages := bson.A{}
		if u.Lines != nil {
			stages = append(stages, bson.D{{Key: "$set", Value: bson.D{{Key: "lines", Value: bson.D{{Key: "$literal", Value: treeToBSON(*u.Lines)}}}}}})
		}
		if u.Ranges != nil {
			stages = append(stages, bson.D{{Key: "$set", Value: bson.D{{Key: "ranges", Value: bson.D{{Key: "$literal", Value: treeToBSON(*u.Ranges)}}}}}})
		}
		if u.Version != nil {
			stages = append(stages, bson.D{{Key: "$set", Value: bson.D{{Key: "version", Value: bson.D{{Key: "$literal", Value: *u.Version}}}}}})
		}
		if u.Lines != nil || u.Ranges != nil {
			stages = append(stages, bson.D{{Key: "$set", Value: bson.D{{Key: "rev", Value: previousRev + 1}}}})
		}
		stages = append(stages, bson.D{{Key: "$unset", Value: "inS3"}})
		filter := bson.D{
			{Key: "_id", Value: doid},
			{Key: "project_id", Value: oid},
			{Key: "rev", Value: previousRev},
		}
		res, err := s.docs.UpdateOne(ctx, filter, stages)
		if err != nil {
			return err
		}
		if res.MatchedCount != 1 {
			return ErrDocRevValue
		}
		return nil
	}
	doc := bson.D{
		{Key: "_id", Value: doid},
		{Key: "project_id", Value: oid},
		{Key: "rev", Value: int64(1)},
	}
	if u.Lines != nil {
		doc = append(doc, bson.E{Key: "lines", Value: treeToBSON(*u.Lines)})
	}
	if u.Ranges != nil {
		doc = append(doc, bson.E{Key: "ranges", Value: treeToBSON(*u.Ranges)})
	}
	if u.Version != nil {
		doc = append(doc, bson.E{Key: "version", Value: *u.Version})
	}
	_, err := s.docs.InsertOne(ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDocRevValue
		}
		return err
	}
	return nil
}

func (s *mongoStore) PatchDocMeta(ctx context.Context, projectID, docID string, deletedAt time.Time, name string) error {
	_, err := s.docs.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: oidOf(docID)}, {Key: "project_id", Value: oidOf(projectID)}},
		bson.D{
			{Key: "$set", Value: bson.D{
				{Key: "deleted", Value: true},
				{Key: "deletedAt", Value: deletedAt.UTC()},
				{Key: "name", Value: name},
			}},
		})
	return err
}

// GetDocForArchiving = MongoManager.getDocForArchiving 1:1 (archivingUntil
// lock; returns the ORIGINAL doc with {lines,ranges,rev}).
func (s *mongoStore) GetDocForArchiving(ctx context.Context, projectID, docID string, lockUntil time.Time) (*Doc, error) {
	now := time.Now().UTC()
	filter := bson.D{
		{Key: "_id", Value: oidOf(docID)},
		{Key: "project_id", Value: oidOf(projectID)},
		{Key: "inS3", Value: bson.D{{Key: "$ne", Value: true}}},
		{Key: "$or", Value: bson.A{
			bson.D{{Key: "archivingUntil", Value: nil}},
			bson.D{{Key: "archivingUntil", Value: bson.D{{Key: "$lt", Value: now}}}},
		}},
	}
	update := bson.D{{Key: "$set", Value: bson.D{{Key: "archivingUntil", Value: lockUntil.UTC()}}}}
	opts := options.FindOneAndUpdate().
		SetProjection(bson.D{{Key: "lines", Value: 1}, {Key: "ranges", Value: 1}, {Key: "rev", Value: 1}}).
		SetReturnDocument(options.Before)
	var raw rawDoc
	err := s.docs.FindOneAndUpdate(ctx, filter, update, opts).Decode(&raw)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return raw.toDoc(), nil
}

func (s *mongoStore) MarkDocAsArchived(ctx context.Context, docID string, rev int64) error {
	// Node filter: {_id, rev} (no project_id) — replicated exactly.
	_, err := s.docs.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: oidOf(docID)}, {Key: "rev", Value: rev}},
		bson.D{
			{Key: "$set", Value: bson.D{{Key: "inS3", Value: true}}},
			{Key: "$unset", Value: bson.D{
				{Key: "lines", Value: 1},
				{Key: "ranges", Value: 1},
				{Key: "archivingUntil", Value: 1},
			}},
		})
	return err
}

// RestoreArchivedDoc = MongoManager.restoreArchivedDoc 1:1 ($literal
// pipeline, rev-matched filter, ranges defaults to {}).
func (s *mongoStore) RestoreArchivedDoc(ctx context.Context, projectID, docID string, lines []string, ranges any, rev int64) error {
	filter := bson.D{
		{Key: "_id", Value: oidOf(docID)},
		{Key: "project_id", Value: oidOf(projectID)},
		{Key: "rev", Value: rev},
	}
	stages := bson.A{
		bson.D{{Key: "$set", Value: bson.D{{Key: "lines", Value: bson.D{{Key: "$literal", Value: treeToBSON(lines)}}}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "ranges", Value: bson.D{{Key: "$literal", Value: treeToBSON(ranges)}}}}}},
		bson.D{{Key: "$unset", Value: "inS3"}},
	}
	res, err := s.docs.UpdateOne(ctx, filter, stages)
	if err != nil {
		return err
	}
	if res.MatchedCount != 1 {
		return ErrDocRevValue
	}
	return nil
}

func (s *mongoStore) DestroyProjectDocs(ctx context.Context, projectID string) error {
	_, err := s.docs.DeleteMany(ctx, bson.D{{Key: "project_id", Value: oidOf(projectID)}})
	return err
}

func (s *mongoStore) DeleteDoc(ctx context.Context, projectID, docID string) error {
	_, err := s.docs.DeleteOne(ctx,
		bson.D{{Key: "_id", Value: oidOf(docID)}, {Key: "project_id", Value: oidOf(projectID)}})
	return err
}

func oidOf(hexstring string) primitive.ObjectID {
	if id, err := primitive.ObjectIDFromHex(hexstring); err == nil {
		return id
	}
	return primitive.ObjectID{}
}
