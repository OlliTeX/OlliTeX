package otc

import (
	oerror "ollitex/go/libraries/oerror"
)

// ChunkResponse mirrors lib/chunk_response.js: allows additional data to be sent
// back with the chunk (currently there is no extra data).
type ChunkResponse struct {
	Chunk *Chunk
}

// NewChunkResponse mirrors `new ChunkResponse(chunk)`.
func NewChunkResponse(chunk *Chunk) *ChunkResponse {
	return &ChunkResponse{Chunk: chunk}
}

// ToRaw (Node: toRaw).
func (r *ChunkResponse) ToRaw() map[string]any {
	return map[string]any{"chunk": r.Chunk.ToRaw()}
}

// ChunkResponseFromRaw mirrors `ChunkResponse.fromRaw`.
//
//	Node:
//	  if (!raw) return null
//	  return new ChunkResponse(Chunk.fromRaw(raw.chunk))
//
// Go: a nil (or empty) raw returns a nil *ChunkResponse (Node: null).
func ChunkResponseFromRaw(raw map[string]any) (*ChunkResponse, error) {
	if raw == nil {
		return nil, nil
	}
	chRaw, ok := raw["chunk"].(map[string]any)
	if !ok {
		return nil, oerror.New("bad raw.chunk", nil)
	}
	ch, err := ChunkFromRaw(chRaw)
	if err != nil {
		return nil, err
	}
	return NewChunkResponse(ch), nil
}

// GetChunk (Node: getChunk).
func (r *ChunkResponse) GetChunk() *Chunk { return r.Chunk }
