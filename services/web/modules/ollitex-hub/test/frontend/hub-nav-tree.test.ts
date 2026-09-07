import { describe, it, expect } from 'vitest'
import {
  HUB_NAV,
  HubNode,
  visibleNav,
  indexNav,
} from '../../frontend/js/hub/nav-tree'
import { SHORTCUT_ALIASES } from '../../frontend/js/hub/navigate'
import { HUB_VALID_ICONS } from '../../frontend/js/hub/icon-names'

function leaves(nodes: HubNode[]): HubNode[] {
  const out: HubNode[] = []
  const rec = (ns: HubNode[]) => {
    for (const n of ns) {
      if (n.children?.length) rec(n.children)
      else out.push(n)
    }
  }
  rec(nodes)
  return out
}

function nodeById(id: string): HubNode | undefined {
  return indexNav(HUB_NAV).byId.get(id)
}

describe('hub nav tree (rail)', () => {
  it('members see workspace items only — no admin (site) leaves', () => {
    const memberLeaves = leaves(visibleNav(HUB_NAV, false)).map(n => n.id)
    for (const id of memberLeaves) {
      expect(id.startsWith('site.'), `member rail must not contain "${id}"`).to.be.false
    }
    // overview is admin-only (analytics/instance shortcuts)
    expect(memberLeaves).to.not.include('overview')
    for (const expected of [
      'projects.all',
      'projects.owned',
      'projects.shared',
      'projects.archived',
      'templates.all',
      'library',
      'mysettings.account',
      'mysettings.password',
      'mysettings.keybindings',
      'mysettings.sync',
      'mysettings.references',
      'mysettings.sessions',
      'mysettings.appearance',
      'mysettings.editordefaults',
      'mysettings.email',
      'mysettings.llm.general',
      'mysettings.llm.grammar',
      'mysettings.llm.compliance',
      'mysettings.llm.usage',
    ]) {
      expect(memberLeaves, `member rail is missing "${expected}"`).to.include(expected)
    }
  })

  it('admins see the full site-settings tree (owner #16 parity)', () => {
    const adminLeaves = leaves(visibleNav(HUB_NAV, true)).map(n => n.id)
    for (const expected of [
      'overview',
      'site.general.misc',
      'site.general.appearance',
      'site.general.signup',
      'site.general.managetpl',
      'site.general.projects.all',
      'site.general.projects.inactive',
      'site.general.projects.trashed',
      'site.general.projects.deleted',
      'site.general.users.all',
      'site.general.users.admins',
      'site.general.users.suspended',
      'site.general.users.inactive',
      'site.general.users.deleted',
      'site.general.activeprojects',
      'site.general.enclose',
      'site.general.messages',
      'site.general.stats',
      'site.integrations.zotero',
      'site.integrations.externalurl',
      'site.services.email',
      'site.services.services',
      'site.services.branding',
      'site.services.grammar',
      'site.compilation.sandboxed',
      'site.compilation.pandoc',
      'site.compilation.git',
      'site.compilation.github',
      'site.compilation.linkedfiletypes',
      'site.compilation.webdav',
      'site.compilation.dropbox',
      'site.llm.features',
      'site.llm.connection',
      'site.llm.models',
      'site.llm.prompt',
      'site.llm.prompts',
      'site.llm.usage',
    ]) {
      expect(adminLeaves, `admin rail is missing "${expected}"`).to.include(expected)
    }
  })

  it('every nav node icon exists in the bundled font slice', () => {
    const all = [...leaves(visibleNav(HUB_NAV, true)), ...HUB_NAV, ...HUB_NAV.flatMap(n => n.children || [])]
    const seen = new Set<string>()
    for (const n of all) {
      if (n.icon) seen.add(n.icon)
    }
    for (const name of seen) {
      expect(HUB_VALID_ICONS.has(name), `icon "${name}" not in the bundled font slice`).to.be.true
    }
  })

  it('overview shortcut aliases all resolve to real leaves', () => {
    const index = indexNav(visibleNav(HUB_NAV, true))
    for (const [alias, target] of Object.entries(SHORTCUT_ALIASES)) {
      expect(index.allLeaves.get(target), `shortcut "${alias}" → unknown leaf "${target}"`).to.exist
    }
  })

  it('rail tones: admin surfaces red, user surfaces blue', () => {
    expect(nodeById('overview')?.tone).to.equal('admin')
    expect(nodeById('site')?.tone).to.equal('admin')
    expect(nodeById('mysettings')?.tone).to.equal('user')
  })
})
