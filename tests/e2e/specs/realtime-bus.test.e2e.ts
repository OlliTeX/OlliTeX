/**
 * realtime-bus — the Go event-bus battery (D28a, S4 flip final leg).
 *
 * The socket.io 0.9 event bus (presence, cursors, join/leave, drain, ops)
 * is served by the Go service `go-services/realtime` on :3026 since the
 * D28a flip; the Node services/real-time is retired. This spec is the
 * live contract the IDE depends on:
 *
 *   B1 tab A boots the IDE (bus join → joinProjectResponse)
 *   B2 tab B boots the IDE (second client in the room)
 *   B3 bus /clients lists BOTH clients of this project
 *   B4 bus /count-connected-clients == 2
 *   B5 closing tab A drops the count to 1 (clientDisconnected path)
 *
 * Ops endpoints are read from inside the overleaf container (the bus
 * listens on 127.0.0.1:3026 inside it), like helpers/host.ts.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'child_process'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { ADMIN } from '../fixtures/credentials'
import { runningContainers } from '../helpers/host'

test.setTimeout(300_000)

function overleafBusCurl(path: string): string {
  const names = runningContainers()
  const ol = names.find((n) => /-e2e-overleaf-1$/.test(n))
  if (!ol) throw new Error('overleaf container not found (expected *-e2e-overleaf-1)')
  return execFileSync('docker', ['exec', '--', ol, 'curl', '-s', '-m', '5', 'http://127.0.0.1:3026' + path], {
    encoding: 'utf8',
    timeout: 30_000,
  }).trim()
}

test('event bus (Go) — 2-tab presence lifecycle', async ({ page, context }) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)
  const pid = await createBlankProject(page)
  expect(pid).toMatch(/^[0-9a-f]{24}$/)

  // B1: tab A boots (the IDE only renders after the bus join resolves —
  // joinProjectResponse over the Go bus)
  await page.goto('/project/' + pid, { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('.cm-content', { timeout: 60_000 })

  // B2: tab B boots (second bus client in the same room)
  const tab2 = await context.newPage()
  await tab2.goto('/project/' + pid, { waitUntil: 'domcontentloaded' })
  await tab2.waitForSelector('.cm-content', { timeout: 60_000 })

  // give the presence refresh a beat
  await page.waitForTimeout(1500)

  // B3: /clients lists both (Node parity: bare JSON array of client views)
  let clients: any = overleafBusCurl('/clients')
  clients = JSON.parse(clients)
  const inProject = (Array.isArray(clients) ? clients : (clients?.clients || []))
    .filter((c: any) => c.project_id === pid)
  expect(inProject.length).toBeGreaterThanOrEqual(2)

  // B4: count == 2
  const count2 = JSON.parse(overleafBusCurl(`/project/${pid}/count-connected-clients`))
  expect(count2.nConnectedClients).toBe(2)

  // B5: close tab A → count drops to 1 (disconnect broadcast + presence cleanup)
  await tab2.close()
  await page.waitForTimeout(1500)
  const count1 = JSON.parse(overleafBusCurl(`/project/${pid}/count-connected-clients`))
  expect(count1.nConnectedClients).toBe(1)

  await page.close()
})
