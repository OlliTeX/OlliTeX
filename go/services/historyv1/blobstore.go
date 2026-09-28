package historyv1

// BlobStores 1:1 with storage/lib/blob_store/{index,mongo}.js:
//
//	blob DATA  → S3 persistor, bucket <blobStore.projectBucket>, key
//	             <projectKey>/<h[0:2]>/<h[2:]> (makeProjectKey 1:1)
//	blob META  → Mongo:
//	             `blobs`:        { _id: <pid>, blobs: { <h[0:3]>: [{h,b,s}...] } }
//	             `shardedBlobs`: { _id: Binary(<pid>0<h[0:1]>), blobs: { <h[1:4]>: [...] } }
//	             (8-blob bucket capacity; overflow → 16 shards × 3-hex buckets)
//
// EMPTY_BLOB reads answer without any stored object (index.js EMPTY_BLOB /
// BlobStoreBase.getString empty-hash fast path). GLOBAL_BLOBS: unset in this
// stack (config.globalBlobs absent → never loaded).

import (
	"bytes"
	"ollitex/go/libraries/otpure"
	"strings"

	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"regexp"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/libraries/otc"
	persistors "ollitex/go/libraries/persistors"
)

var blobHashRe = regexp.MustCompile(`^[0-9a-f]{8,}$`) // Node assert.js blobHash (hex ≥ 8)

const maxBlobsInBucket = 8

// BlobMeta — one mongo blob record {h (hex), b byteLength, s? stringLength}.
type BlobMeta struct {
	Hash         string `bson:"h" json:"h"`
	ByteLength   int64  `bson:"b" json:"b"`
	StringLength *int64 `bson:"s"`
}

func (m *BlobMeta) toBlob() *otc.Blob { return otc.NewBlob(m.Hash, m.ByteLength, m.StringLength) }

// BlobStore — Node `new BlobStore(projectId)` (mongo meta + S3 data).
type BlobStore struct {
	db        *mongo.Database
	persistor persistors.Persistor
	bucket    string // blobStore.projectBucket (default 'projectblobs')
	projectID string
	pg        *pgxpool.Pool
}

// BlobStores — the per-project factory (Service.Blob).
type BlobStores struct {
	db   *mongo.Database
	pers persistors.Persistor
	PG   *pgxpool.Pool
}

func (s *BlobStores) ForProject(projectID string) *BlobStore {
	bs := &BlobStore{
		db:        s.db,
		persistor: s.pers,
		bucket:    "projectblobs",
		projectID: projectID,
		pg:        s.PG,
	}
	return bs
}

// makeProjectKey — index.js 1:1.
func makeProjectKey(projectID, hash string) string {
	return projectKeyFormat(projectID) + "/" + hash[0:2] + "/" + hash[2:]
}

func (b *BlobStore) key(hash string) string { return makeProjectKey(b.projectID, hash) }

type projectBlobDoc struct {
	Blobs map[string][]BlobMeta `bson:"blobs"`
}

// Initialize — mongo.initialize (insert {pid, blobs:{}}, 409 → ignore).
func (b *BlobStore) Initialize(ctx context.Context) error {
	if b.pg != nil && numericID(b.projectID) {
		return nil // Node: "Nothing to do for Postgres"
	}
	oid, err := primitive.ObjectIDFromHex(b.projectID)
	if err != nil {
		return fmt.Errorf("bad projectId: %v", err)
	}
	_, err = b.db.Collection("blobs").InsertOne(ctx, bson.D{
		{Key: "_id", Value: oid},
		{Key: "blobs", Value: bson.M{}},
	})
	if isDupKey(err) {
		return nil
	}
	return err
}

// FindBlob — mongo.findBlob → findBlobSharded → nil (EMPTY fast path);
// numeric ids route meta to the PG project_blobs table (Node index.js
// getBackend 1:1).
func (b *BlobStore) FindBlob(ctx context.Context, hash string) (*otc.Blob, error) {
	if hash == otpure.EmptyHash {
		return otc.NewBlob(otpure.EmptyHash, 0, i64(0)), nil
	}
	if b.pg != nil && numericID(b.projectID) {
		return (&pgBlobMeta{pg: b.pg}).Find(ctx, b.projectID, hash)
	}
	if !blobHashRe.MatchString(hash) {
		return nil, errors.New("bad blob hash")
	}
	oid, err := primitive.ObjectIDFromHex(b.projectID)
	if err != nil {
		return nil, errors.New("bad projectId")
	}
	if rec, err := b.lookup(b.db.Collection("blobs"), oid, hash[0:3], hash, ctx); err != nil {
		return nil, err
	} else if rec != nil {
		return rec.toBlob(), nil
	}
	if rec, err := b.lookupSharded(hash, ctx); err != nil {
		return nil, err
	} else if rec != nil {
		return rec.toBlob(), nil
	}
	return nil, nil
}

// lookup — unsharded `blobs` collection: doc.blobs.<h[0:3]>[] record {h==hash}.
func (b *BlobStore) lookup(coll *mongo.Collection, oid primitive.ObjectID, bucket, hash string, ctx context.Context) (*BlobMeta, error) {
	var doc projectBlobDoc
	err := coll.FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	records := doc.Blobs[bucket]
	for i := range records {
		if records[i].Hash == hash {
			return &records[i], nil
		}
	}
	return nil, nil
}

// lookupSharded — shardedBlobs: _id = Binary(<pid>0<h[0:1]>), bucket h[1:4].
func (b *BlobStore) lookupSharded(hash string, ctx context.Context) (*BlobMeta, error) {
	idBytes, err := hex.DecodeString(b.projectID + "0" + hash[0:1])
	if err != nil {
		return nil, err
	}
	var doc projectBlobDoc
	err = b.db.Collection("shardedBlobs").FindOne(ctx, bson.D{
		{Key: "_id", Value: primitive.Binary{Subtype: 0x00, Data: idBytes}},
	}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	records := doc.Blobs[hash[1:4]]
	for i := range records {
		if records[i].Hash == hash {
			return &records[i], nil
		}
	}
	return nil, nil
}

// PutString — Node putString (hash the string, return existing, else upload +
// metadata).
func (b *BlobStore) PutString(ctx context.Context, content string) (*otc.Blob, error) {
	return b.putBuffer(ctx, []byte(content), true)
}

// PutFile — Node putFile (buffer form: the handler already read the request
// body; same semantics: existing → return it, else putBlob).
func (b *BlobStore) PutFile(ctx context.Context, data []byte) (*otc.Blob, error) {
	var sLen *int64
	if utf8.Valid(data) {
		n := int64(runeCount(string(data)))
		sLen = &n
	}
	return b.put(ctx, otpure.BlobHashFromBuffer(data), int64(len(data)), data, sLen)
}

// PutWithHash — controller path: the URL-validated hash (Node stores the
// params.hash from the URL, verified equal to the sha1 of the payload).
func (b *BlobStore) PutWithHash(ctx context.Context, hash string, data []byte) (*otc.Blob, error) {
	var sLen *int64
	if utf8.Valid(data) {
		n := int64(runeCount(string(data)))
		sLen = &n
	}
	return b.put(ctx, strings.ToLower(hash), int64(len(data)), data, sLen)
}

func (b *BlobStore) putBuffer(ctx context.Context, data []byte, isText bool) (*otc.Blob, error) {
	sLen := (*int64)(nil)
	if isText && utf8.Valid(data) {
		n := int64(runeCount(string(data)))
		sLen = &n
	}
	return b.put(ctx, otpure.BlobHashFromBuffer(data), int64(len(data)), data, sLen)
}

func (b *BlobStore) put(ctx context.Context, hash string, size int64, data []byte, sLen *int64) (*otc.Blob, error) {
	if existing, err := b.FindBlob(ctx, hash); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}
	err := b.persistor.SendStream(b.bucket, b.key(hash), bytesReader(data), persistors.Opts{
		ContentType:   "application/octet-stream",
		ContentLength: size,
	})
	if err != nil {
		return nil, err
	}
	blob := otc.NewBlob(hash, size, sLen)
	if err := b.InsertBlob(ctx, blob); err != nil {
		return nil, err
	}
	return blob, nil
}

// InsertBlob — mongo.insertBlob (upsert record; overflow > 8 → sharded copy).
func (b *BlobStore) InsertBlob(ctx context.Context, blob *otc.Blob) error {
	if b.pg != nil && numericID(b.projectID) {
		return (&pgBlobMeta{pg: b.pg}).Insert(ctx, b.projectID, blob)
	}
	rec := BlobMeta{Hash: blob.Hash, ByteLength: blob.ByteLength, StringLength: blob.StringLength}
	oid, err := primitive.ObjectIDFromHex(b.projectID)
	if err != nil {
		return errors.New("bad projectId")
	}
	coll := b.db.Collection("blobs")
	bucket := blob.Hash[0:3]

	var doc projectBlobDoc
	err = coll.FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		doc = projectBlobDoc{Blobs: map[string][]BlobMeta{}}
		if _, err := coll.InsertOne(ctx, bson.D{
			{Key: "_id", Value: oid},
			{Key: "blobs", Value: doc.Blobs},
		}); err != nil && !isDupKey(err) {
			return err
		}
		err = nil
	} else if err != nil {
		return err
	}
	records := doc.Blobs[bucket]
	for i := range records {
		if records[i].Hash == blob.Hash {
			return nil // already recorded
		}
	}
	records = append(records, rec)
	doc.Blobs[bucket] = records
	if _, err := coll.UpdateOne(ctx, bson.D{{Key: "_id", Value: oid}}, bson.D{
		{Key: "$set", Value: bson.D{{Key: "blobs", Value: doc.Blobs}}},
	}); err != nil {
		return err
	}
	if len(records) > maxBlobsInBucket {
		if err := b.insertSharded(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}

func (b *BlobStore) insertSharded(ctx context.Context, rec BlobMeta) error {
	shard := rec.Hash[0:1]
	bucket := rec.Hash[1:4]
	idBytes, err := hex.DecodeString(b.projectID + "0" + shard)
	if err != nil {
		return err
	}
	coll := b.db.Collection("shardedBlobs")
	id := primitive.Binary{Subtype: 0x00, Data: idBytes}
	var doc projectBlobDoc
	err = coll.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		doc = projectBlobDoc{Blobs: map[string][]BlobMeta{}}
		if _, err := coll.InsertOne(ctx, bson.D{
			{Key: "_id", Value: id},
			{Key: "blobs", Value: doc.Blobs},
		}); err != nil && !isDupKey(err) {
			return err
		}
		err = nil
	} else if err != nil {
		return err
	}
	for i := range doc.Blobs[bucket] {
		if doc.Blobs[bucket][i].Hash == rec.Hash {
			return nil
		}
	}
	doc.Blobs[bucket] = append(doc.Blobs[bucket], rec)
	_, err = coll.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}}, bson.D{
		{Key: "$set", Value: bson.D{{Key: "blobs", Value: doc.Blobs}}},
	})
	return err
}

// GetStream — BlobStoreBase.getStream (persistor.GetObjectStream; range via
// Opts.Start/End; NotFoundError → otc Chunk.NotPersistedError).
func (b *BlobStore) GetStream(ctx context.Context, hash string, start, end *int64) (io.ReadCloser, error) {
	rc, err := b.persistor.GetObjectStream(b.bucket, b.key(hash), persistors.Opts{Start: start, End: end})
	if err != nil {
		var nf *persistors.NotFoundError
		if errors.As(err, &nf) {
			return nil, otc.NewChunkNotPersistedError(b.projectID)
		}
		return nil, err
	}
	return rc, nil
}

// GetString — BlobStoreBase.getString (empty fast path + read all).
func (b *BlobStore) GetString(ctx context.Context, hash string) (string, error) {
	if hash == otpure.EmptyHash {
		return "", nil
	}
	rc, err := b.GetStream(ctx, hash, nil, nil)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	buf, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

// GetObject — BlobStoreBase.getObject (JSON parse; ” → error, Node 1:1).
func (b *BlobStore) GetObject(ctx context.Context, hash string) (map[string]any, error) {
	s, err := b.GetString(ctx, hash)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if s == "" {
		return nil, errors.New("JSON parse of empty string")
	}
	if err := jsonUnmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CopyBlob — Node copyBlob (persistor copy + insertBlob into target meta).
func (b *BlobStore) CopyBlob(ctx context.Context, blob *otc.Blob, targetProjectID string) error {
	dst := b.ForTarget(targetProjectID)
	if err := b.persistor.CopyObject(b.bucket, b.key(blob.Hash), dst.key(blob.Hash), persistors.Opts{}); err != nil {
		return err
	}
	return dst.InsertBlob(ctx, blob)
}

func (b *BlobStore) ForTarget(projectID string) *BlobStore {
	return &BlobStore{db: b.db, persistor: b.persistor, bucket: b.bucket, projectID: projectID, pg: b.pg}
}

// DeleteBlobs — mongo.deleteBlobs (project doc + its 16 shards).
func (b *BlobStore) DeleteBlobs(ctx context.Context) error {
	if b.pg != nil && numericID(b.projectID) {
		return (&pgBlobMeta{pg: b.pg}).Delete(ctx, b.projectID)
	}
	oid, err := primitive.ObjectIDFromHex(b.projectID)
	if err != nil {
		return errors.New("bad projectId")
	}
	if _, err := b.db.Collection("blobs").DeleteOne(ctx, bson.D{{Key: "_id", Value: oid}}); err != nil {
		return err
	}
	minBytes, _ := hex.DecodeString(b.projectID + "00")
	maxBytes, _ := hex.DecodeString(b.projectID + "0f")
	_, err = b.db.Collection("shardedBlobs").DeleteMany(ctx, bson.D{
		{Key: "_id", Value: bson.D{
			{Key: "$gte", Value: primitive.Binary{Subtype: 0x00, Data: minBytes}},
			{Key: "$lte", Value: primitive.Binary{Subtype: 0x00, Data: maxBytes}},
		}},
	})
	return err
}

// GetProjectBlobs — mongo.getProjectBlobs (non-sharded + all 16 shards).
func (b *BlobStore) GetProjectBlobs(ctx context.Context) ([]BlobMeta, error) {
	if b.pg != nil && numericID(b.projectID) {
		return (&pgBlobMeta{pg: b.pg}).GetProjectBlobs(ctx, b.projectID)
	}
	oid, err := primitive.ObjectIDFromHex(b.projectID)
	if err != nil {
		return nil, errors.New("bad projectId")
	}
	out := []BlobMeta{}
	var doc projectBlobDoc
	if err := b.db.Collection("blobs").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&doc); err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}
	for _, recs := range doc.Blobs {
		out = append(out, recs...)
	}
	for i := 0; i < 16; i++ {
		shard := string(byte('0' + i))
		idBytes, err := hex.DecodeString(b.projectID + "0" + shard)
		if err != nil {
			return nil, err
		}
		var sdoc projectBlobDoc
		err = b.db.Collection("shardedBlobs").FindOne(ctx, bson.D{
			{Key: "_id", Value: primitive.Binary{Subtype: 0x00, Data: idBytes}},
		}).Decode(&sdoc)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				continue
			}
			return nil, err
		}
		for _, recs := range sdoc.Blobs {
			out = append(out, recs...)
		}
	}
	return out, nil
}

// Clone — mongo.clone (copy meta doc + shards to the target; returns hashes).
func (b *BlobStore) Clone(ctx context.Context, sourceProjectID string) ([]string, error) {
	if b.pg != nil && numericID(b.projectID) && numericID(sourceProjectID) {
		return (&pgBlobMeta{pg: b.pg}).Clone(ctx, sourceProjectID, b.projectID)
	}
	oidSrc, err := primitive.ObjectIDFromHex(sourceProjectID)
	if err != nil {
		return nil, errors.New("bad source projectId")
	}
	oidDst, err := primitive.ObjectIDFromHex(b.projectID)
	if err != nil {
		return nil, errors.New("bad target projectId")
	}
	var src projectBlobDoc
	if err := b.db.Collection("blobs").FindOne(ctx, bson.D{{Key: "_id", Value: oidSrc}}).Decode(&src); err != nil {
		return nil, errors.New("missing blobs for source project")
	}
	hashes := []string{}
	for _, recs := range src.Blobs {
		for i := range recs {
			hashes = append(hashes, recs[i].Hash)
		}
	}
	if _, err := b.db.Collection("blobs").UpdateOne(ctx, bson.D{{Key: "_id", Value: oidDst}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "blobs", Value: src.Blobs}}}}); err != nil {
		return nil, err
	}
	for i := 0; i < 16; i++ {
		shard := string(byte('0' + i))
		srcBytes, _ := hex.DecodeString(sourceProjectID + "0" + shard)
		dstBytes, _ := hex.DecodeString(b.projectID + "0" + shard)
		var sdoc projectBlobDoc
		err := b.db.Collection("shardedBlobs").FindOne(ctx, bson.D{
			{Key: "_id", Value: primitive.Binary{Subtype: 0x00, Data: srcBytes}},
		}).Decode(&sdoc)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				continue
			}
			return nil, err
		}
		for _, recs := range sdoc.Blobs {
			for j := range recs {
				hashes = append(hashes, recs[j].Hash)
			}
		}
		if len(sdoc.Blobs) > 0 {
			if _, err := b.db.Collection("shardedBlobs").UpdateOne(ctx,
				bson.D{{Key: "_id", Value: primitive.Binary{Subtype: 0x00, Data: dstBytes}}},
				bson.D{{Key: "$set", Value: bson.D{{Key: "blobs", Value: sdoc.Blobs}}}},
				options.Update().SetUpsert(true)); err != nil {
				return nil, err
			}
		}
	}
	return hashes, nil
}

// ---------- small helpers (shared with chunk store) ----------

func isDupKey(err error) bool { return mongo.IsDuplicateKeyError(err) }

func i64(n int) *int64 { v := int64(n); return &v }

func runeCount(s string) int { return utf8.RuneCountInString(s) }

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
