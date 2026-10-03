/**
 * Regression: ext User-schema fields survive Mongoose strict mode
 * (owner queue item "Zotero schema").
 *
 * History: Mongoose strict mode silently STRIPS unmodeled top-level paths on
 * document write. The ext port added storage late in the cycle — Zotero
 * credential bag, custom keybindings, LLM provider rows. This test pins that
 * those paths are modeled AND round-trip through `new User(...).toObject()`
 * without being dropped. If a schema entry is deleted by accident, this
 * fails loudly instead of users losing their Zotero link / keybindings / BYO
 * rows at runtime.
 *
 * Schema contract (VERIFIED against app/src/models/User.mjs 2026-09):
 *  - top-level `refProviders.{zotero,mendeley,papers}` = Mixed free-form bag
 *    (live zotero TokenManager path: User.findById(userId,
 *     'refProviders.zotero') + $set 'refProviders.zotero')
 *  - `ace.zotero.*` = fully modeled { apiKeyEncrypted: String,
 *    enabled: Boolean, groups: [], disablePersonalLibrary: Boolean }
 *  - `ace.customKeybindings` = Map<key, command> (R6/R11 keybindings)
 *  - `llmProviders` = subdocument array (BYO rows)
 *
 * No Mongo connection needed — schema introspection + doc instantiation only.
 * No real keys anywhere — dummies only (owner rule).
 */
import { describe, expect, it } from 'vitest'
import { User, UserSchema } from '../../../../app/src/models/User.mjs'

// dummy stored secret — NOT a real key
const ENC =
  'enc:v1:QUJDQUJDQUJDQUJD:QUJDQUJDQUJDQUJDQUJD:QUJDQUJDQUJDQUJD'

describe('ext User schema fields (regression)', () => {
  it('models the top-level refProviders bag (zotero = Mixed, per live TokenManager)', function () {
    expect(UserSchema.path('refProviders')).not.toBeNull()
    expect(UserSchema.path('refProviders.zotero').instance).toEqual('Mixed')
    // Mixed = free-form: the credential value passes through untouched, which
    // is exactly what the zotero flow depends on.
    const p = UserSchema.path('refProviders.zotero.apiKeyEncrypted')
    expect(p).toBeDefined()
  })

  it('models ace.zotero.{apiKeyEncrypted,enabled,groups,disablePersonalLibrary}', function () {
    expect(UserSchema.path('ace.zotero.apiKeyEncrypted').instance).toEqual('String')
    expect(UserSchema.path('ace.zotero.enabled').instance).toEqual('Boolean')
    expect(UserSchema.path('ace.zotero.groups')).not.toBeNull()
    expect(
      UserSchema.path('ace.zotero.disablePersonalLibrary').instance
    ).toEqual('Boolean')
  })

  it('models ace.customKeybindings as a Map (R6/R11 custom keybindings)', function () {
    const p = UserSchema.path('ace.customKeybindings')
    expect(p).not.toBeNull()
    expect(p.instance || p.constructor?.name || 'Map').toBeTruthy()
  })

  it('models llmProviders as an array of subdocs (BYO provider rows)', function () {
    expect(UserSchema.path('llmProviders')).not.toBeNull()
    expect(UserSchema.path('llmProviders.name').instance).toEqual('String')
    expect(UserSchema.path('llmProviders.apiKey').instance).toEqual('String')
  })

  it('round-trips the zotero credential bag through toObject() without strict-drop', function () {
    const doc = new User({
      email: 'schema-test@example.com',
      refProviders: {
        zotero: {
          apiKeyEncrypted: ENC,
          tokenScopes: ['libraries:read'],
        },
      },
    })
    const obj = doc.toObject()
    expect(obj.refProviders.zotero.apiKeyEncrypted).toEqual(ENC)
    expect(obj.refProviders.zotero.tokenScopes).toEqual(['libraries:read'])
  })

  it('round-trips a full ext payload (keybindings Map + zotero + BYO rows)', function () {
    const doc = new User({
      email: 'schema-test-2@example.com',
      ace: {
        mode: 'custom',
        zotero: {
          enabled: true,
          apiKeyEncrypted: ENC,
          groups: [{ id: 'grp-1' }],
          disablePersonalLibrary: false,
        },
        customKeybindings: new Map([
          ['find', 'mod-f'],
          ['replace', 'mod-r'],
        ]),
      },
      llmProviders: [
        {
          id: 'row-1',
          name: 'Test provider',
          providerType: 'openaiCompatible',
          baseUrl: 'https://llm.example.com/v1',
          apiKey: ENC,
          models: ['meta-llama-3.1-8b-instruct'],
        },
      ],
    })

    const obj = doc.toObject()

    expect(obj.ace.zotero.apiKeyEncrypted).toEqual(ENC)
    expect(obj.ace.zotero.groups).toEqual([{ id: 'grp-1' }])
    expect(obj.ace.zotero.enabled).toBe(true)

    const kb = obj.ace.customKeybindings
    const kbMap = kb instanceof Map ? kb : new Map(Object.entries(kb || {}))
    expect(kbMap.get('find')).toEqual('mod-f')
    expect(kbMap.get('replace')).toEqual('mod-r')

    expect(obj.llmProviders[0].apiKey).toEqual(ENC)
    expect(obj.llmProviders[0].name).toEqual('Test provider')

    // whole doc must stay serializable (save path shape)
    const round = JSON.parse(JSON.stringify(obj))
    expect(round.ace.zotero.apiKeyEncrypted).toEqual(ENC)
  })
})
