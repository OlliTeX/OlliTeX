package historyv1

// Postgres seam (PG18, owner-pinned image "postgres:18-alpine").
// 1:1 with storage/lib/chunk_store/postgres.js + blob_store/postgres.js.
//
// Schema (migrations 20220228163642 + chunk_start_version + unique +
// add_chunks_closed), replicated by e2e compose / the Node service:
//
//	chunks          (id SERIAL, doc_id int, start_version int, end_version int,
//	                   end_timestamp timestamptz, closed bool)
//	pending_chunks  (id SERIAL, doc_id int, start_version int, end_version int,
//	                   end_timestamp timestamptz)
//	old_chunks      (chunk_id int PK, doc_id, end_version, end_timestamp, deleted_at)
//	project_blobs   (project_id int, hash_bytes bytea, byte_length int, string_length int)

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"ollitex/go/libraries/otc"
)

const pgUniqueViolation = "23505"

func pgErrCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func pgID(projectID string) (int64, error) {
	if !numericID(projectID) {
		return 0, errors.New("bad postgres project id: " + projectID)
	}
	n, err := strconv.ParseInt(projectID, 10, 64)
	if err != nil {
		return 0, errors.New("bad postgres project id: " + projectID)
	}
	return n, nil
}

type pgChunkBackend struct {
	pg *pgxpool.Pool
}

func (p *pgChunkBackend) GetLatestChunk(ctx context.Context, projectID string, _ bool) (*ChunkMeta, error) {
	did, err := pgID(projectID)
	if err != nil {
		return nil, err
	}
	var rec struct {
		ID           int64
		StartVersion *int64
		EndVersion   int64
		EndTimestamp *time.Time
	}
	err = p.pg.QueryRow(ctx,
		`SELECT id, start_version, end_version, end_timestamp FROM chunks WHERE doc_id=$1 ORDER BY end_version DESC LIMIT 1`, did,
	).Scan(&rec.ID, &rec.StartVersion, &rec.EndVersion, &rec.EndTimestamp)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sv int
	if rec.StartVersion != nil {
		sv = int(*rec.StartVersion)
	}
	return &ChunkMeta{ID: strconv.FormatInt(rec.ID, 10), StartVersion: sv, EndVersion: int(rec.EndVersion), EndTimestamp: rec.EndTimestamp}, nil
}

func (p *pgChunkBackend) GetChunkForVersion(ctx context.Context, projectID string, version int, preferNewer bool) (*ChunkMeta, error) {
	did, err := pgID(projectID)
	if err != nil {
		return nil, err
	}
	dir := "ASC"
	if preferNewer {
		dir = "DESC"
	}
	var rec struct {
		ID           int64
		StartVersion *int64
		EndVersion   int64
		EndTimestamp *time.Time
	}
	err = p.pg.QueryRow(ctx,
		`SELECT id, start_version, end_version, end_timestamp FROM chunks
		 WHERE doc_id=$1 AND start_version <= $2 AND end_version >= $2
		 ORDER BY end_version `+dir+` LIMIT 1`, did, int32(version),
	).Scan(&rec.ID, &rec.StartVersion, &rec.EndVersion, &rec.EndTimestamp)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, otcNewVersionNotFound(projectID, version)
	}
	if err != nil {
		return nil, err
	}
	var sv int
	if rec.StartVersion != nil {
		sv = int(*rec.StartVersion)
	}
	return &ChunkMeta{ID: strconv.FormatInt(rec.ID, 10), StartVersion: sv, EndVersion: int(rec.EndVersion), EndTimestamp: rec.EndTimestamp}, nil
}

func (p *pgChunkBackend) GetChunkForTimestamp(ctx context.Context, projectID string, ts time.Time) (*ChunkMeta, error) {
	did, err := pgID(projectID)
	if err != nil {
		return nil, err
	}
	var rec struct {
		ID           int64
		StartVersion *int64
		EndVersion   int64
		EndTimestamp *time.Time
	}
	err = p.pg.QueryRow(ctx,
		`SELECT id, start_version, end_version, end_timestamp FROM chunks
		 WHERE doc_id=$1 AND (end_timestamp >= $2
			or id = (SELECT id FROM chunks WHERE doc_id=$1 ORDER BY end_version DESC LIMIT 1))
		 ORDER BY end_version LIMIT 1`, did, ts,
	).Scan(&rec.ID, &rec.StartVersion, &rec.EndVersion, &rec.EndTimestamp)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, otcNewBeforeTimestampNotFound(projectID, ts)
	}
	if err != nil {
		return nil, err
	}
	var sv int
	if rec.StartVersion != nil {
		sv = int(*rec.StartVersion)
	}
	return &ChunkMeta{ID: strconv.FormatInt(rec.ID, 10), StartVersion: sv, EndVersion: int(rec.EndVersion), EndTimestamp: rec.EndTimestamp}, nil
}

func (p *pgChunkBackend) GetProjectChunkIDs(ctx context.Context, projectID string) ([]string, error) {
	did, err := pgID(projectID)
	if err != nil {
		return nil, err
	}
	rows, err := p.pg.Query(ctx, `SELECT id FROM chunks WHERE doc_id=$1`, did)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, strconv.FormatInt(id, 10))
	}
	return out, rows.Err()
}

func (p *pgChunkBackend) GetProjectChunks(ctx context.Context, projectID string) ([]ChunkMeta, error) {
	did, err := pgID(projectID)
	if err != nil {
		return nil, err
	}
	rows, err := p.pg.Query(ctx, `SELECT id, start_version, end_version, end_timestamp FROM chunks WHERE doc_id=$1 ORDER BY end_version`, did)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChunkMeta{}
	for rows.Next() {
		var id int64
		var sv *int64
		var ev int64
		var ets *time.Time
		if err := rows.Scan(&id, &sv, &ev, &ets); err != nil {
			return nil, err
		}
		meta := ChunkMeta{ID: strconv.FormatInt(id, 10), EndVersion: int(ev), EndTimestamp: ets}
		if sv != nil {
			meta.StartVersion = int(*sv)
		}
		out = append(out, meta)
	}
	return out, rows.Err()
}

func (p *pgChunkBackend) Clone(ctx context.Context, srcID, dstID string) (map[string]string, error) {
	return map[string]string{}, errors.New("pg clone: not yet ported (DU-era path)")
}

func (p *pgChunkBackend) InsertPendingChunk(ctx context.Context, projectID string, c *otc.Chunk) (string, error) {
	did, err := pgID(projectID)
	if err != nil {
		return "", err
	}
	// Node: nextval('chunks_id_seq') shared sequence for chunk ids
	var chunkID int64
	if err := p.pg.QueryRow(ctx, `SELECT nextval('chunks_id_seq')`).Scan(&chunkID); err != nil {
		return "", err
	}
	var endTS interface{}
	if t := c.GetEndTimestamp(); t != nil {
		endTS = *t
	}
	_, err = p.pg.Exec(ctx,
		`INSERT INTO pending_chunks (id, doc_id, end_version, start_version, end_timestamp)
		 VALUES ($1,$2,$3,$4,$5)`,
		chunkID, did, int32(c.GetEndVersion()), int32(c.GetStartVersion()), endTS)
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(chunkID, 10), nil
}

func (p *pgChunkBackend) ConfirmCreate(ctx context.Context, projectID string, c *otc.Chunk, chunkID string, oldChunkID *string, earliest *time.Time) error {
	did, err := pgID(projectID)
	if err != nil {
		return err
	}
	cid, err := strconv.ParseInt(chunkID, 10, 64)
	if err != nil {
		return err
	}
	tx, err := p.pg.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if oldChunkID != nil {
		oid, err := strconv.ParseInt(*oldChunkID, 10, 64)
		if err != nil {
			return err
		}
		if err := p.assertChunkNotClosed(ctx, tx, did, oid); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE chunks SET closed=true WHERE doc_id=$1 AND id=$2`, did, oid); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM pending_chunks WHERE doc_id=$1 AND id=$2`, did, cid); err != nil {
		return err
	}
	var endTS interface{}
	if t := c.GetEndTimestamp(); t != nil {
		endTS = *t
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO chunks (id, doc_id, start_version, end_version, end_timestamp)
		 VALUES ($1,$2,$3,$4,$5)`,
		cid, did, int32(c.GetStartVersion()), int32(c.GetEndVersion()), endTS)
	if err != nil {
		if pgErrCode(err) == pgUniqueViolation {
			return newChunkConflict("chunk start or end version is not unique", map[string]any{"projectId": projectID, "chunkId": chunkID})
		}
		return err
	}
	return tx.Commit(ctx)
}

func (p *pgChunkBackend) ConfirmUpdate(ctx context.Context, projectID, oldChunkID string, newChunk *otc.Chunk, newChunkID string, earliest *time.Time) error {
	did, err := pgID(projectID)
	if err != nil {
		return err
	}
	oid, err := strconv.ParseInt(oldChunkID, 10, 64)
	if err != nil {
		return err
	}
	cid, err := strconv.ParseInt(newChunkID, 10, 64)
	if err != nil {
		return err
	}
	tx, err := p.pg.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := p.assertChunkNotClosed(ctx, tx, did, oid); err != nil {
		return err
	}
	if err := p.deleteChunksToOld(ctx, tx, did, &oid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM pending_chunks WHERE doc_id=$1 AND id=$2`, did, cid); err != nil {
		return err
	}
	var endTS interface{}
	if t := newChunk.GetEndTimestamp(); t != nil {
		endTS = *t
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO chunks (id, doc_id, start_version, end_version, end_timestamp)
		 VALUES ($1,$2,$3,$4,$5)`,
		cid, did, int32(newChunk.GetStartVersion()), int32(newChunk.GetEndVersion()), endTS)
	if err != nil {
		if pgErrCode(err) == pgUniqueViolation {
			return newChunkConflict("chunk start or end version is not unique", map[string]any{"projectId": projectID, "chunkId": newChunkID})
		}
		return err
	}
	return tx.Commit(ctx)
}

func (p *pgChunkBackend) assertChunkNotClosed(ctx context.Context, tx pgx.Tx, did, chunkID int64) error {
	var closed bool
	err := tx.QueryRow(ctx, `SELECT closed FROM chunks WHERE doc_id=$1 AND id=$2 FOR UPDATE`, did, chunkID).Scan(&closed)
	if errors.Is(err, pgx.ErrNoRows) {
		return newChunkConflict("unable to close chunk: not found", map[string]any{})
	}
	if err != nil {
		return err
	}
	if closed {
		return newChunkConflict("unable to close chunk: already closed", map[string]any{})
	}
	return nil
}

func (p *pgChunkBackend) deleteChunk(ctx context.Context, tx pgx.Tx, did, chunkID int64) error {
	return p.oldChunks(ctx, tx, `DELETE FROM chunks WHERE doc_id=$1 AND id=$2 RETURNING id, doc_id, start_version, end_version, end_timestamp`, did, chunkID)
}

func (p *pgChunkBackend) deleteChunksToOld(ctx context.Context, tx pgx.Tx, did int64, chunkID *int64) error {
	if chunkID != nil {
		return p.deleteChunk(ctx, tx, did, *chunkID)
	}
	return p.oldChunks(ctx, tx, `DELETE FROM chunks WHERE doc_id=$1 RETURNING id, doc_id, start_version, end_version, end_timestamp`, did)
}

func (p *pgChunkBackend) DeleteChunk(ctx context.Context, projectID, chunkID string) error {
	did, err := pgID(projectID)
	if err != nil {
		return err
	}
	cid, err := strconv.ParseInt(chunkID, 10, 64)
	if err != nil {
		return err
	}
	return p.deleteCtx(ctx, did, cid)
}

func (p *pgChunkBackend) deleteCtx(ctx context.Context, did, cid int64) error {
	tx, err := p.pg.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := p.deleteChunk(ctx, tx, did, cid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *pgChunkBackend) DeleteProjectChunks(ctx context.Context, projectID string) error {
	did, err := pgID(projectID)
	if err != nil {
		return err
	}
	tx, err := p.pg.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := p.deleteChunksToOld(ctx, tx, did, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GenerateProjectID — Node postgres.generateProjectId (nextval docs_id_seq).
func (p *pgChunkBackend) GenerateProjectID(ctx context.Context) (string, error) {
	var id int64
	if err := p.pg.QueryRow(ctx, `SELECT nextval('docs_id_seq')`).Scan(&id); err != nil {
		return "", err
	}
	return strconv.FormatInt(id, 10), nil
}

// ---------- PG blob meta (blob_store/postgres.js 1:1) ----------

type pgBlobMeta struct {
	pg *pgxpool.Pool
}

func (b *pgBlobMeta) Find(ctx context.Context, projectID, hash string) (*otc.Blob, error) {
	did, err := pgID(projectID)
	if err != nil {
		return nil, err
	}
	var byteLen int64
	var strLen *int64
	var hashBytes []byte
	err = b.pg.QueryRow(ctx,
		`SELECT hash_bytes, byte_length, string_length FROM project_blobs WHERE project_id=$1 AND hash_bytes=$2`,
		did, hashHexToBytes(hash),
	).Scan(&hashBytes, &byteLen, &strLen)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return otc.NewBlob(hash, byteLen, strLen), nil
}

func (b *pgBlobMeta) Insert(ctx context.Context, projectID string, blob *otc.Blob) error {
	did, err := pgID(projectID)
	if err != nil {
		return err
	}
	_, err = b.pg.Exec(ctx,
		`INSERT INTO project_blobs (project_id, hash_bytes, byte_length, string_length) VALUES ($1,$2,$3,$4)
		 ON CONFLICT DO NOTHING`,
		did, hashHexToBytes(blob.Hash), int32(blob.ByteLength), blob.StringLength)
	return err
}

func (b *pgBlobMeta) GetProjectBlobs(ctx context.Context, projectID string) ([]BlobMeta, error) {
	did, err := pgID(projectID)
	if err != nil {
		return nil, err
	}
	rows, err := b.pg.Query(ctx, `SELECT hash_bytes, byte_length, string_length FROM project_blobs WHERE project_id=$1`, did)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlobMeta{}
	for rows.Next() {
		var h []byte
		var byt int64
		var sl *int64
		if err := rows.Scan(&h, &byt, &sl); err != nil {
			return nil, err
		}
		out = append(out, BlobMeta{Hash: bytesToHex(h), ByteLength: byt, StringLength: sl})
	}
	return out, rows.Err()
}

func (b *pgBlobMeta) Delete(ctx context.Context, projectID string) error {
	did, err := pgID(projectID)
	if err != nil {
		return err
	}
	_, err = b.pg.Exec(ctx, `DELETE FROM project_blobs WHERE project_id=$1`, did)
	return err
}

func (b *pgBlobMeta) Clone(ctx context.Context, srcID, dstID string) ([]string, error) {
	s, err := pgID(srcID)
	if err != nil {
		return nil, err
	}
	d, err := pgID(dstID)
	if err != nil {
		return nil, err
	}
	rows, err := b.pg.Query(ctx,
		`INSERT INTO project_blobs (project_id, hash_bytes, byte_length, string_length)
		 SELECT $1, hash_bytes, byte_length, string_length FROM project_blobs WHERE project_id=$2
		 RETURNING hash_bytes`, d, s)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var h []byte
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, bytesToHex(h))
	}
	return out, rows.Err()
}

// hex helpers

func hashHexToBytes(hexHash string) []byte {
	out := make([]byte, len(hexHash)/2)
	for i := 0; i+1 < len(hexHash); i += 2 {
		v := hexDigit(hexHash[i])<<4 | hexDigit(hexHash[i+1])
		out[i/2] = v
	}
	return out
}

func hexDigit(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

func bytesToHex(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = digits[c>>4]
		out[i*2+1] = digits[c&0xf]
	}
	return string(out)
}

var _ ChunkBackend = (*pgChunkBackend)(nil)

func otcNewVersionNotFound(projectID string, version int) error {
	return otc.NewChunkVersionNotFoundError(projectID, itoaVersion(version))
}

func otcNewBeforeTimestampNotFound(projectID string, ts time.Time) error {
	return &otc.ChunkBeforeTimestampNotFoundError{ProjectID: projectID, Timestamp: ts}
}

func (p *pgChunkBackend) oldChunks(ctx context.Context, tx pgx.Tx, q string, args ...any) error {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return err
	}
	var old []struct {
		ChunkID      int64
		DocID        int64
		StartVersion *int64
		EndVersion   int64
		EndTimestamp *time.Time
	}
	for rows.Next() {
		var r struct {
			ChunkID      int64
			DocID        int64
			StartVersion *int64
			EndVersion   int64
			EndTimestamp *time.Time
		}
		if err := rows.Scan(&r.ChunkID, &r.DocID, &r.StartVersion, &r.EndVersion, &r.EndTimestamp); err != nil {
			return err
		}
		old = append(old, r)
	}
	for i := range old {
		if _, err := tx.Exec(ctx, `INSERT INTO old_chunks (chunk_id, doc_id, end_version, end_timestamp, deleted_at) VALUES ($1,$2,$3,$4, now())`,
			old[i].ChunkID, old[i].DocID, old[i].EndVersion, old[i].EndTimestamp); err != nil {
			return err
		}
	}
	return nil
}
