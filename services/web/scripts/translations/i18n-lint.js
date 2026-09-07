#!/usr/bin/env node
/**
 * i18n linter (owner 2026-09-07, "Forgejo-style i18n linter for LibreLeaf").
 *
 * Guards the WHOLE translation chain, not just one link:
 *
 *   code (t('key') / translate('key'))
 *     -> locales/en.json                 (C1: key must exist, else raw key in UI)
 *     -> frontend/extracted-translations.json   (C2: key must be extracted, else
 *        the webpack translations-loader silently drops it from the bundle)
 *     <- frontend/extracted-translations.json   (C3: every extracted key needs an
 *        en.json value, else it is pruned from the bundle at build time)
 *     -> en.json values non-empty         (C4)
 *
 * This is exactly the pair of failures that shipped on 2026-09-07 (missing
 * mendeley_not_configured in en.json; several keys present in en.json but
 * absent from extracted-translations.json, so the runtime bundle rendered raw
 * keys).
 *
 * Usage: node scripts/translations/i18n-lint.js [--json]
 * Exit code 0 = clean, 1 = findings.
 */
import fs from 'node:fs'
import Path from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = Path.dirname(fileURLToPath(import.meta.url))
const WEB_ROOT = Path.resolve(HERE, '../..')
const EN_PATH = Path.join(WEB_ROOT, 'locales/en.json')
const EXTRACTED_PATH = Path.join(
  WEB_ROOT,
  'frontend/extracted-translations.json'
)

const SKIP_DIRS = new Set([
  'node_modules',
  '.yarn',
  '.cache',
  'public',
  'test',
  'dist',
  'coverage',
])

function walk(dir, out = []) {
  let entries
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true })
  } catch {
    return out
  }
  for (const e of entries) {
    if (e.name.startsWith('.') || SKIP_DIRS.has(e.name)) continue
    const p = Path.join(dir, e.name)
    if (e.isDirectory()) {
      walk(p, out)
    } else if (
      e.isFile() &&
      (e.name.endsWith('.ts') ||
        e.name.endsWith('.tsx') ||
        e.name.endsWith('.js') ||
        e.name.endsWith('.jsx') ||
        e.name.endsWith('.pug'))
    ) {
      out.push(p)
    }
  }
  return out
}

function scanDir(dir, files = []) {
  if (!fs.existsSync(dir)) return files
  return walk(dir)
}

/**
 * Literal i18n key usages:
 *  tsx/ts/js/jsx :  t('key'), t("key"), t(`key`)   (skip dynamic/template
 *               interpolating usages, calls with non-literal first arg, and
 *               t('key', 'literal default') — the string literal is the
 *               declared fallback, no en.json entry required)
 *  pug           :  translate('key') / translate("key")
 * Keys must look like translation keys (no whitespace).
 */
const LITERAL = /(?:\bt|translate)\(\s*(['"`])([A-Za-z0-9_.\-]+)\1(?!\s*,\s*(['"`]))/g
const SOURCE_DIRS = [
  Path.join(WEB_ROOT, 'frontend'),
  Path.join(WEB_ROOT, 'modules'),
  Path.join(WEB_ROOT, 'app'),
]

function extractUsedKeys(files) {
  const used = new Map() // key -> first file:line
  for (const file of files) {
    let text
    try {
      text = fs.readFileSync(file, 'utf8')
    } catch {
      continue
    }
    const lines = text.split('\n')
    for (let i = 0; i < lines.length; i += 1) {
      const line = lines[i]
      if (!line) continue
      for (const m of line.matchAll(LITERAL)) {
        const key = m[2]
        if (!used.has(key)) {
          const rel = Path.relative(WEB_ROOT, file)
          used.set(key, `${rel}:${i + 1}`)
        }
      }
    }
  }
  return used
}

function main() {
  const en = JSON.parse(fs.readFileSync(EN_PATH, 'utf8'))
  const extracted = JSON.parse(fs.readFileSync(EXTRACTED_PATH, 'utf8'))
  const files = SOURCE_DIRS.flatMap(d => scanDir(d))
  const used = extractUsedKeys(files)

  const errors = []
  const warnings = []

  // C1: every used key must exist in en.json (else raw key is shown)
  for (const [key, where] of [...used.entries()].sort()) {
    if (!(key in en)) {
      errors.push(`[C1] used in code but missing from locales/en.json: "${key}" (${where})`)
    }
  }

  // C2: every used key must be in extracted-translations.json (else the
  // webpack translations-loader drops it from the runtime bundle)
  for (const [key, where] of [...used.entries()].sort()) {
    if (key in en && !(key in extracted)) {
      errors.push(`[C2] used in code + en.json, but missing from frontend/extracted-translations.json (will render RAW at runtime): "${key}" (${where})`)
    }
  }

  // C3: every extracted key needs an en.json value (loader keeps the
  // intersection; extracted-but-untranslated keys silently vanish)
  for (const key of Object.keys(extracted).sort()) {
    if (!(key in en)) {
      errors.push(`[C3] in extracted-translations.json but missing from locales/en.json: "${key}"`)
    }
  }

  // C4: en.json values must be non-empty strings
  for (const [key, value] of Object.entries(en)) {
    if (typeof value !== 'string' || value.trim() === '') {
      warnings.push(`[C4] empty locale value: "${key}"`)
    }
  }

  if (JSON.stringify(process.argv).includes('--json')) {
    console.log(
      JSON.stringify(
        { usedKeys: used.size, errors, warnings },
        null,
        2
      )
    )
  } else {
    console.log(
      `i18n-lint: scanned ${files.length} files, ${used.size} literal keys`
    )
    for (const e of errors) console.log('  ' + e)
    for (const w of warnings) console.log('  ' + w)
    if (errors.length === 0 && warnings.length === 0) {
      console.log('i18n-lint: CLEAN')
    } else {
      console.log(
        `i18n-lint: ${errors.length} error(s), ${warnings.length} warning(s)`
      )
    }
  }
  process.exitCode = errors.length > 0 ? 1 : 0
}

main()
