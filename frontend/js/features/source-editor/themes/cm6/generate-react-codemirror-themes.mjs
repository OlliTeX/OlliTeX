#!/usr/bin/env node
/**
 * J (owner 2026-10-09): vendored "Editor themes" — the color themes from the
 * reference repo /data_1/image_mining/the_diff/react-codemirror/themes
 * (credited in CREDITS.md).
 *
 * Converts each theme's `defaultSettings*` palette + highlight styles into
 * the cm6 registry format used by js/features/source-editor/extensions/
 * theme.ts (`{ theme, highlightStyle, dark }`). Text-parses the two uniform
 * shapes in that repo (settings palette + [{tag, color}] arrays), resolving
 * `c.*` references into the theme's color config (color.ts).
 *
 * Lezer tag → the CM6 token classes emitted by extensions/class-highlighter.
 * Run:  node frontend/js/features/source-editor/themes/cm6/generate-react-codemirror-themes.mjs
 * (SOURCE_ROOT overridable; defaults to the sibling react-codemirror repo.)
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
const __dirname = path.dirname(fileURLToPath(import.meta.url))

const SOURCE_ROOT =
  process.env.SOURCE_ROOT ||
  '/data_1/image_mining/the_diff/react-codemirror/themes'
const OUT_DIR = __dirname

// lezer base tag name → CM6 tok-* class (class-highlighter.ts contract)
const TAG2TOK = {
  keyword: 'tok-keyword',
  operatorKeyword: 'tok-keyword',
  string: 'tok-string',
  number: 'tok-number',
  typeName: 'tok-type',
  className: 'tok-type',
  comment: 'tok-comment',
  functionName: 'tok-function',
  variableName: 'tok-variableName',
  definition: 'tok-variableName',
  name: 'tok-variableName',
  constant: 'tok-literal',
  atom: 'tok-literal',
  attributeName: 'tok-attributeValue',
  attributeValue: 'tok-attributeValue',
  propertyName: 'tok-propertyName',
  heading: 'tok-heading',
  invalid: 'tok-invalid',
  namespace: 'tok-namespace',
  labelName: 'tok-label',
  label: 'tok-label',
}

function hexLuma(hex) {
  try {
    let h = String(hex).replace('#', '')
    if (h.length === 3) h = h.split('').map(c => c + c).join('')
    if (h.length < 6) return null
    const n = parseInt(h.slice(0, 6), 16)
    const r = (n >> 16) & 255
    const g = (n >> 8) & 255
    const b = n & 255
    return (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255
  } catch {
    return null
  }
}

function parseColorConfig(src) {
  const out = {}
  const blocks = src.matchAll(
    /(?:export\s+)?const\s+(?:config|colors?)[\w]*\s*=\s*\{([\s\S]*?)\n\}/g,
  )
  for (const b of blocks) {
    const pat = /(\w+)\s*:\s*(['"])([\s\S]*?)\2/g
    let mm
    while ((mm = pat.exec(b[1])) !== null) out[mm[1]] = mm[3]
  }
  return out
}

function resolveVal(v, cfg) {
  if (typeof v !== 'string') return v
  const m = v.match(/^c\.?(\w+)?$/)
  if (m && m[1] && cfg[m[1]]) return cfg[m[1]]
  return v
}

function parseKV(body, cfg) {
  const out = {}
  const pat = /(\w+)\s*:\s*(?:(["'])((?:\\.|[^'"])*?)\2|([A-Za-z_][\w.$]*(?:\([^()\s]*\)?)*))/g
  let mm
  while ((mm = pat.exec(body)) !== null) {
    if (mm[2] !== undefined) out[mm[1]] = mm[3]
    else out[mm[1]] = resolveVal(mm[4].trim(), cfg)
  }
  return out
}

function parseSettings(src, cfg) {
  const m = src.match(
    /defaultSettings[\w]*\s*[:=]\s*(?:CreateThemeOptions\['settings'\]\s*=\s*)?\{([\s\S]*?)\n\}/,
  )
  if (!m) return null
  const out = parseKV(m[1], cfg)
  return out.background ? out : null
}

function parseStyles(src, cfg) {
  const entries = []
  const seen = new Set()
  const re =
    /\{\s*tag\s*:\s*([0-9a-zA-Z_,.\s\[\]()$]+?)\s*,\s*color\s*:\s*(?:(["'])((?:\\.|[^'"])*?)\2|([A-Za-z_][\w.$]*(?:\([^()\s]*\)?)*))/g
  let mm
  while ((mm = re.exec(src)) !== null) {
    let color = mm[3]
    if (color === undefined) color = mm[4]
    color = resolveVal(color, cfg)
    if (!color) continue
    const tags = []
    const trefs = mm[1].matchAll(/t\.(\w+)(?:\(([^()]+)\))?/g)
    for (const tr of trefs) {
      const head = tr[1]
      if (tr[2] && (head === 'special' || head === 'function' || head === 'definition')) {
        const inner = tr[2].match(/t\.(\w+)/)
        if (inner) tags.push(inner[1])
        if (head === 'function') tags.push('functionName')
      } else if (head !== 'special') {
        tags.push(head)
      }
    }
    for (const base of tags) {
      const cls = TAG2TOK[base]
      if (cls && !seen.has(cls)) {
        seen.add(cls)
        entries.push({ cls, color })
      }
    }
  }
  return entries
}

function buildTheme(name, dir) {
  const srcDir = path.join(dir, 'src')
  if (!fs.existsSync(srcDir)) return null
  let all = ''
  for (const f of fs.readdirSync(srcDir)) {
    if (/\.tsx?$/.test(f)) all += fs.readFileSync(path.join(srcDir, f), 'utf8') + '\n'
  }
  const cfg = parseColorConfig(all)
  const settings = parseSettings(all, cfg)
  if (!settings) return null
  const styles = parseStyles(all, cfg)
  // settings-only themes (e.g. 'console') are valid: base palette
  // + the editor's default syntax colors.
  if (!Object.keys(settings).length) return null

  const luma = hexLuma(settings.background)
  const dark = luma === null ? true : luma < 0.5
  const bg = settings.background
  const fg = settings.foreground || '#ddd'

  const theme = {
    '&': { backgroundColor: bg, color: fg },
    '.cm-gutters': {
      background: settings.gutterBackground || bg,
      color: settings.gutterForeground || fg,
      borderRightColor: 'transparent',
    },
    '.cm-cursor, .cm-dropCursor': { color: settings.caret || fg },
    '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': {
      backgroundColor: settings.selection || (dark ? 'rgba(160,160,200,0.35)' : 'rgba(255,255,255,0.6)'),
    },
    '.cm-activeLine': {
      backgroundColor: settings.lineHighlight || (dark ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.05)'),
    },
  }
  if (settings.dropdownBackground || settings.gutterBackground) {
    theme['.cm-tooltip'] = {
      backgroundColor: settings.dropdownBackground || settings.gutterBackground,
      color: fg,
    }
  }
  if (settings.matchingBracket) {
    theme['.cm-matchingBracket'] = { backgroundColor: settings.matchingBracket }
  }

  const highlightStyle = {}
  for (const s of styles) highlightStyle['.' + s.cls] = { color: s.color }

  return { theme, highlightStyle, dark }
}

function main() {
  const names = fs
    .readdirSync(SOURCE_ROOT)
    .filter(n => fs.statSync(path.join(SOURCE_ROOT, n)).isDirectory())
    .sort()

  const LEGACY = new Set([
    'ambiance','chaos','chrome','clouds','clouds_midnight','cobalt',
    'crimson_editor','dawn','dracula','dreamweaver','eclipse','github','gob',
    'gruvbox','idle_fingers','iplastic','katzenmilch','kr_theme','kuroir',
    'merbivore','merbivore_soft','mono_industrial','monokai','nord_dark',
    'one_dark','overleaf','overleaf_dark','pastel_on_dark','solarized_dark',
    'solarized_light','sqlserver','terminal','textmate','tomorrow',
    'tomorrow_night','tomorrow_night_blue','tomorrow_night_bright',
    'tomorrow_night_eighties','twilight','vibrant_ink','xcode',
  ])

  const index = []
  const skipped = []
  for (const name of names) {
    if (name === '_scripts' || name === 'all') continue
    if (!/^[a-z0-9][a-z0-9-]*$/i.test(name)) continue
    if (LEGACY.has(name)) {
      skipped.push(`${name} (name collides with an existing cm6 theme — existing wins)`)
      continue
    }
    const built = buildTheme(name, path.join(SOURCE_ROOT, name))
    if (!built) {
      skipped.push(`${name} (could not parse)`)
      continue
    }
    fs.writeFileSync(path.join(OUT_DIR, name + '.json'), JSON.stringify(built, null, 2) + '\n')
    index.push({ name, dark: built.dark })
  }

  // index.json = the vendored additions currently present in the registry
  // (stable across idempotent rebuilds; legacy themes keep their own options).
  const allVendored = fs
    .readdirSync(OUT_DIR)
    .filter(f => f.endsWith('.json'))
    .map(f => f.slice(0, -5))
    .filter(n => !LEGACY.has(n) && n !== 'index')
  const themes = allVendored
    .sort((a, b) => a.localeCompare(b))
    .map(n => {
      const data = JSON.parse(fs.readFileSync(path.join(OUT_DIR, n + '.json'), 'utf8'))
      return {
        name: n,
        dark: !!data.dark,
        label: n
          .split(/[-_]/)
          .filter(Boolean)
          .map(p => p[0].toUpperCase() + p.slice(1))
          .join(' '),
      }
    })
  fs.writeFileSync(
    path.join(OUT_DIR, 'index.json'),
    JSON.stringify({
      // static descriptor list for the UI (theme-toggle options +
      // overall-theme dark/light resolution).
      themes,
    }, null, 2) + '\n',
  )
  console.log(`generated ${index.length} themes -> ${OUT_DIR}`)
  console.log('skipped: ' + (skipped.join(', ') || '(none)'))
}

main()
