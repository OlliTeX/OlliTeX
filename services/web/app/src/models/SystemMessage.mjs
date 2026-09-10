import mongoose from '../infrastructure/Mongoose.mjs'

const { Schema } = mongoose

// Owner #17b (2026-09-13): `placements` scopes a message to specific
// app surfaces ('editor' | 'hub' | 'auth'). Missing/empty list = visible
// on ALL pages (legacy behavior, kept for every pre-existing message).
const SystemMessageSchema = new Schema(
  {
    content: { type: String, default: '' },
    placements: { type: [String], default: [] },
  },
  { minimize: false }
)

export const SystemMessage = mongoose.model(
  'SystemMessage',
  SystemMessageSchema
)
