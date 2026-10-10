// /hub → Site settings → Compilation: Python runner (owner request H,
// 2026-10-09 — "add the Python runner admin section (toggle,
// interpreter/version pinning, allowed-module policy, per-instance
// defaults)").
//
// The runner is the .py split-pane editor (frontend/modules/python-runner):
// Pyodide — CPython compiled to WebAssembly — in a dedicated web worker.
// The runtime (pyodide 0.29.3, Python 3.12) is VENDORED INTO THE OLlITex
// image (frontend build copies the npm pyodide package into
// js/libs/pyodide/ — no runtime CDN), and the toggle gates the editor
// split-test variant `overleaf-code` (Go: sitesettings.PythonRunnerEnabled
// → editorpages.SplitTestVariants — OFF hides the split-pane and .py files
// open in the standard source editor).
//
// Persistence: the `misc` site-settings section (the same store + API as
// every other site.* section):
//   - pythonRunner (bool)          — the toggle (was already a misc key;
//     this section is its canonical admin surface).
//   - allowedPipPackages (string)  — the allowed-module policy the admins
//     intend to make available as Pyodide packages (comma-separated names;
//     informational today — the worker is already offline-only, resolving
//     packages exclusively from the vendored js/libs/pyodide directory).
import React from 'react'
import { Group, Text } from '@mantine/core'
import {
  Field,
  PageLoading,
  SectionShell,
  bool0,
  str0,
  useSyncValues,
  useSiteSettings,
} from './site-core'

export function PythonRunnerSection() {
  const { data, error, flash, save, load } = useSiteSettings('misc')
  const { v, up } = useSyncValues(data, d => ({
    // Default ON — the runner has always been available in this stack
    // (overleaf-code split pinned enabled); the toggle is the rollback
    // path (Typst-section pattern).
    enabled: bool0((d as any).pythonRunner, true),
    allowed: str0((d as any).allowedPipPackages),
  }))
  if (!data && !error) return <PageLoading label="Loading Python runner settings…" />
  if (error && !data) {
    return (
      <Group>
        <Text size="sm" c="red">Python runner: {error}</Text>
        <a href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</a>
      </Group>
    )
  }
  return (
    <SectionShell
      title="Python runner (.py split editor)"
      badge="pythonrunner"
      enabled={Boolean(v.enabled)}
      onEnabled={x => up({ enabled: x })}
      description="In-browser Python execution for .py files (Pyodide — CPython on WebAssembly — in a worker). Bundled in the OlliTeX image: Pyodide 0.29.3 / Python 3.12, no external downloads at runtime."
      footerNote="The toggle takes effect the next time an editor page loads (split-test variant `overleaf-code`)."
      flash={flash}
      onSave={() => void save({
        pythonRunner: Boolean(v.enabled),
        allowedPipPackages: String(v.allowed || ''),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field
          label="Interpreter (pinned in the image)"
          value="Python 3.12 (Pyodide 0.29.3)"
          onChange={() => undefined}
          hint="Versioned at build time by the frontend bundle (yarn pyodide). Upgrading = rebuild the OlliTeX image."
          width="100%"
        />
        <Field
          label="Allowed package policy"
          value={String(v.allowed || '')}
          onChange={x => up({ allowed: x })}
          placeholder="numpy, pandas, matplotlib"
          hint="Comma-separated Pyodide packages you intend to make importable. The runner is offline-only: packages resolve exclusively from the vendored js/libs/pyodide directory — nothing is fetched from the network at runtime."
          width="100%"
        />
      </Group>
    </SectionShell>
  )
}
