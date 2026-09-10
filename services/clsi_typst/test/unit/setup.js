// Minimal unit-test setup for clsi_typst.
//
// We deliberately do NOT replicate clsi's chai.should()/sinon-chai/
// chai-as-promised wiring: clsi's unit suite is RED in this environment with
// "[Function functionStub] is not a spy or a call to a spy!" (a chai-plugin
// interaction under PnP), and clsi's test files assert through that plugin
// chain. clsi_typst's unit tests instead assert with plain vitest `expect`
// and vi.fn() spies, which avoids the fragile layer entirely.
//
// Note: globals are NOT enabled (see vitest.config.unit.cjs); tests import
// describe/it/expect/vi explicitly from 'vitest'.
import { afterEach, vi } from 'vitest'

afterEach(() => {
  // restore any vi.restoreAllMocks'd spies and clear the module registry so
  // each test re-imports fresh modules with fresh vi.doMock registrations.
  vi.restoreAllMocks()
  vi.resetModules()
})
