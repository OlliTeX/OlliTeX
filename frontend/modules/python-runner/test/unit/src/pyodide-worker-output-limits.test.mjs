import { describe, it, expect } from 'vitest'
import {
  checkOutputCount,
  checkOutputLimits,
  MAX_OUTPUT_FILES,
  MAX_OUTPUT_TOTAL_BYTES,
  MAX_OUTPUT_FILE_BYTES,
} from '../../../frontend/js/components/editor/python/pyodide-worker-output-limits'

// python-runner (ported from ayakaleaf-pro): the output-limit guards are the
// only hard caps on worker side-effects (files written back into the
// project). They are pure functions — cover the boundaries exactly.

const MB = 1024 * 1024

describe('pyodide-worker-output-limits: file count', () => {
  it('allows exactly MAX_OUTPUT_FILES files', () => {
    expect(checkOutputCount(MAX_OUTPUT_FILES)).toBeNull()
  })

  it('rejects MAX_OUTPUT_FILES + 1 with kind=count', () => {
    const v = checkOutputCount(MAX_OUTPUT_FILES + 1)
    expect(v).not.toBeNull()
    expect(v.kind).toBe('count')
    expect(v.message).toContain(String(MAX_OUTPUT_FILES))
  })

  it('limits are the documented 50-file / 50MB / 100MB caps', () => {
    expect(MAX_OUTPUT_FILES).toBe(50)
    expect(MAX_OUTPUT_FILE_BYTES).toBe(50 * MB)
    expect(MAX_OUTPUT_TOTAL_BYTES).toBe(100 * MB)
  })
})

describe('pyodide-worker-output-limits: sizes', () => {
  it('allows a file exactly at the per-file cap', () => {
    expect(checkOutputLimits([{ path: 'a.bin', size: MAX_OUTPUT_FILE_BYTES }])).toBeNull()
  })

  it('rejects a single oversized file with kind=single-file-size', () => {
    const v = checkOutputLimits([{ path: 'big.bin', size: 51 * MB }])
    expect(v).not.toBeNull()
    expect(v.kind).toBe('single-file-size')
    expect(v.message).toContain('big.bin')
  })

  it('rejects the total once it crosses 100MB', () => {
    // three files: each under the 50MB cap, total 102MB
    const v = checkOutputLimits([
      { path: 'a.bin', size: 40 * MB },
      { path: 'b.bin', size: 34 * MB },
      { path: 'c.bin', size: 28 * MB },
    ])
    expect(v).not.toBeNull()
    expect(v.kind).toBe('total-output-size')
    expect(v.message).toContain('102MB')
  })

  it('allows many small files that fit under the total cap', () => {
    const files = Array.from({ length: 50 }, (_, i) => ({
      path: `f${i}.txt`,
      size: 1 * MB,
    }))
    // 50 * 1MB = 50MB total, 50 files — both under their caps
    expect(checkOutputLimits(files)).toBeNull()
  })

  it('reports the count violation before size violations', () => {
    const files = Array.from({ length: 51 }, () => ({ path: 'x.bin', size: 1 * MB }))
    const v = checkOutputLimits(files)
    expect(v.kind).toBe('count')
  })
})
