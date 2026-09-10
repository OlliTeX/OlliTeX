/**
 * Typst compile-log parser (F3.5 — fresh; the old typst fork had no parser,
 * it surfaced clsi_typst `output.log` raw in the existing "Output Logs" raw
 * tab, which we retain. This adds click-to-source parity with LaTeX).
 *
 * Lives in core `ide/log-parser/` next to `latex-log-parser.ts` +
 * `bib-log-parser.ts` (the core seam `pdf-preview/util/output-files.ts`
 * dispatches from — core→module static imports are not allowed).
 *
 * Parses the clsi_typst `output.log` line format (the typst compiler's
 * miette-style diagnostics, captured *verbatim* by clsi_typst via
 * `typst compile main.typ output.pdf >> output.log 2>&1`; live-typed corpus
 * at `services/clsi_typst/examples/errors/*.expected.txt`, typst 0.14.2)
 * into `LogEntry[]` so the PDF preview "Output Logs" panel + the existing
 * click-to-source hook (`use-handle-log-entry-click`, which reads
 * `entry.file` / `entry.line` / `entry.column`) work for `.typ` documents.
 *
 * Block shape (one per diagnostic; blocks are separated by blank lines):
 *
 *   error: <message>                         <- header: error|warning|note|info
 *     ┌─ main.typ:3:9                        <- location: file line:col (1-based)
 *     │
 *   3 │ #let x = (1 + 2                      <- snippet: raw source line (optional)
 *     │          ^                           <- caret (optional)
 *
 * A diagnostic has exactly one location; typst uses `┌─` for the *file*
 * frame and the following `┌─` for *in-file* positions (e.g. a warning
 * pointing at the exact token). For a single-error case the parser records
 * the first location. For multi-location cases (rare; e.g. "warning: ...
 * caused by ... at ...") the entry carries the first location and any
 * additional `┌─` blocks are attached to `.location` as extra context
 * (not consumed by the click-to-source hook, which only reads the entry
 * top-level `file`/`line`/`column`).
 *
 * The leading `typst 0.14.2 (<hash>)` banner is a non-matching line and is
 * silently ignored.
 */
import { ErrorLevel, LogEntry } from '@/features/pdf-preview/util/types'

// top-level diagnostic header (col 0, "note" counts as a warning)
const HEADER_RE = /^(error|warning|note|info):(.*)$/

// location line: "  ┌─ <file>:<line>:<col>" (file may be absent for the
// second (in-file) location of a diagnostic)
const LOCATION_RE = /^\s*┌─(?:\s+(\S+))?:\s*(\d+):(\d+)/

export function parseTypstLog(raw: string | null | undefined): LogEntry[] {
  const entries: LogEntry[] = []
  if (typeof raw !== 'string' || raw.length === 0) {
    return entries
  }

  const lines = raw.split('\n')
  let current: LogEntry | null = null
  let locationCount = 0

  const flush = () => {
    if (current) {
      entries.push(current)
      current = null
      locationCount = 0
    }
  }

  for (const line of lines) {
    const header = line.match(HEADER_RE)
    if (header) {
      flush()
      const kind = header[1]
      const level: ErrorLevel =
        kind === 'warning' || kind === 'note' ? 'warning' : 'error'
      current = {
        raw: line,
        level,
        key: String(entries.length),
        message: (header[2] || '').trim(),
      }
      continue
    }

    // location line attaches file/line/col to the open entry
    const loc = line.match(LOCATION_RE)
    if (loc && current) {
      const f = loc[1]
      const l = parseInt(loc[2], 10)
      const c = parseInt(loc[3], 10)
      if (locationCount === 0) {
        // first location: the entry's primary file/line/col
        current.file = f
        current.line = l
        current.column = c
        locationCount = 1
      } else {
        // subsequent locations (rare multi-frame): retain as extra info
        ;(current as LogEntry & { location?: string }).location =
          (current as LogEntry & { location?: string }).location ?? '' +
          `\n${f || ''}:${l}:${c}`
      }
    }
  }
  flush()

  return entries.filter(
    e =>
      (e.message !== undefined && e.message !== '') ||
      e.file !== undefined
  )
}

/**
 * Split a single parsed-logs `raw` into the {errors, warnings, typesetting}
 * groups the frontend `handleLogFiles` `accumulateResults` expects.
 * `typesetting` is unused for typst (clsi_typst emits no typesetting
 * diagnostics).
 */
export function parseTypstLogEntries(
  raw: string | null | undefined,
): { errors: LogEntry[]; warnings: LogEntry[]; typesetting: LogEntry[] } {
  const all = parseTypstLog(raw)
  return {
    errors: all.filter(e => e.level === 'error'),
    warnings: all.filter(e => e.level === 'warning'),
    typesetting: [],
  }
}

export default parseTypstLog
