// overleaf-lab (M2.5): Appearance — instance-wide hub theme.
//
// A SINGLE small document (collection `hubthemes`, id 'default') holds the
// admin's custom theme for /hub: per-light/dark mode colors, fonts, radius.
// Scope is the HUB only (nav_structure.md §8.8: "affects the hub in both modes").
// 2026-09-13 owner wave (#14/#15): the editor page also renders this theme
// (ProjectController.mjs + project/editor/_meta.pug) so the editor chrome and
// module modals share one instance design language. Document contract unchanged.
// All reads/writes are safe (failures logged, flow continues) so a Mongo
// hiccup can never break /hub rendering.
import logger from '@overleaf/logger'
import mongoose from 'mongoose'

const { Schema } = mongoose

const HubThemeSchema = new Schema({
  documentId: { type: String, required: true, unique: true },
  version: { type: Number, default: 1 },
  light: { type: Object, default: null },
  dark: { type: Object, default: null },
  updatedAt: { type: Date },
})

let themeModel = null
function getModel() {
  if (!themeModel) {
    themeModel = mongoose.model('HubTheme', HubThemeSchema)
  }
  return themeModel
}

const COLOR_RE = /^#([0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/
const COLOR_KEYS = [
  'primary',
  'background',
  'surface',
  'text',
  'dimmed',
  'border',
  'button',
  'buttonText',
]

function validateMode(mode, where) {
  if (!mode || typeof mode !== 'object') {
    throw new Error(`${where}: must be an object`)
  }
  const out = {}
  for (const key of COLOR_KEYS) {
    const v = mode[key]
    if (typeof v !== 'string' || !COLOR_RE.test(v)) {
      throw new Error(`${where}.${key}: must be a hex color (#rrggbb)`)
    }
    out[key] = v
  }
  if (
    typeof mode.fontFamily !== 'string' ||
    !mode.fontFamily.trim() ||
    mode.fontFamily.length > 300
  ) {
    throw new Error(`${where}.fontFamily: must be a non-empty css font stack`)
  }
  out.fontFamily = mode.fontFamily
  if (
    typeof mode.fontSize !== 'number' ||
    mode.fontSize < 12 ||
    mode.fontSize > 22
  ) {
    throw new Error(`${where}.fontSize: must be a number between 12 and 22`)
  }
  out.fontSize = mode.fontSize
  if (
    typeof mode.radius !== 'number' ||
    mode.radius < 2 ||
    mode.radius > 24
  ) {
    throw new Error(`${where}.radius: must be a number between 2 and 24`)
  }
  out.radius = mode.radius
  return out
}

export async function getHubTheme() {
  try {
    const doc = await getModel()
      .findOne({ documentId: 'default' })
      .lean()
      .exec()
    if (!doc) return null
    return { version: Number(doc.version) || 1, light: doc.light, dark: doc.dark }
  } catch (err) {
    logger.error({ err, context: 'hubTheme.get' }, 'hub theme read failed')
    return null
  }
}

export async function saveHubTheme(input) {
  const light = validateMode(input && input.light, 'light')
  const dark = validateMode(input && input.dark, 'dark')
  const value = { version: 1, light, dark, updatedAt: new Date() }
  await getModel().updateOne(
    { documentId: 'default' },
    { $set: value },
    { upsert: true }
  )
  logger.info({ context: 'hubTheme.save' }, 'hub theme saved')
  return { version: 1, light, dark }
}

export async function clearHubTheme() {
  await getModel().deleteOne({ documentId: 'default' })
  logger.info({ context: 'hubTheme.clear' }, 'hub theme reset to defaults')
  return { ok: true }
}
