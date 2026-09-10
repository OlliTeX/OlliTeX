/**
 * OlliTeX hub navigation tree (canonical design: nav_structure.md §2).
 *
 * Data-driven: the rail (hub/rail.tsx) renders any shape of this tree,
 * and leaf rendering is keyed by node id (hub/leaves.tsx). Admin-only
 * nodes hide their entire subtree for non-admin members.
 *
 * Leaf ids are stable deep-link keys: #/site/general/users/suspended,
 * #/projects/owned, #/mysettings/llm/grammar, ...
 */

export type HubNode = {
  id: string
  label: string
  icon: string
  /** admin-only node (subtree hides for members) */
  admin?: boolean
  /** leaf nodes map to a shared "render kind" */
  render?: 'projects' | 'projects-tags' | 'template-cat' | 'site-sec' | 'overview' | 'library'
  /** site-settings section id for render:'site-sec' (legacy NAV ids) */
  siteId?: string
  /** template category key for render:'template-cat' */
  category?: string
  /** projects view for render:'projects' */
  pview?: 'all' | 'owned' | 'shared' | 'archived' | 'trashed'
  /** dynamic children marker (template categories from API) */
  dynamicChildren?: string
  children?: HubNode[]
}

export const HUB_NAV: HubNode[] = [
  {
    id: 'overview',
    label: 'Overview & activity',
    icon: 'insights',
    admin: true,
    render: 'overview',
    tone: 'admin',
  },
  {
    id: 'projects',
    label: 'Projects',
    icon: 'folder',
    children: [
      { id: 'projects.all', label: 'All projects', icon: 'apps', render: 'projects', pview: 'all' },
      { id: 'projects.owned', label: 'My projects', icon: 'folder', render: 'projects', pview: 'owned' },
      { id: 'projects.shared', label: 'Shared with you', icon: 'groups', render: 'projects', pview: 'shared' },
      { id: 'projects.archived', label: 'Archived projects', icon: 'archive', render: 'projects', pview: 'archived' },
      // Owner #10d (2026-09-07): trashed view with restore.
      { id: 'projects.trashed', label: 'Trashed projects', icon: 'delete', render: 'projects', pview: 'trashed' },
      {
        id: 'projects.tags',
        label: 'Organize Tags',
        icon: 'tag',
        children: [
          { id: 'projects.tags.tags', label: 'All tags', icon: 'tag' },
          { id: 'projects.tags.new', label: 'New tag', icon: 'add' },
        ],
      },
    ],
  },
  {
    id: 'templates',
    label: 'Templates',
    icon: 'menu_book',
    // children are replaced at runtime with the live category list
    // (GET /api/template/categories) + 'All templates'
    dynamicChildren: 'templates',
    children: [
      { id: 'templates.all', label: 'All templates', icon: 'layers', render: 'template-cat', category: 'none' },
    ],
  },
  { id: 'library', label: 'Reference library', icon: 'menu_book', render: 'library' },
  {
    id: 'mysettings',
    label: 'My settings',
    icon: 'person',
    tone: 'user',
    children: [
      { id: 'mysettings.account', label: 'Update account info', icon: 'badge' },
      { id: 'mysettings.password', label: 'Change password', icon: 'key' },
      { id: 'mysettings.keybindings', label: 'Keybindings', icon: 'keyboard' },
      { id: 'mysettings.sync', label: 'Project synchronisation', icon: 'cloud_sync' },
      { id: 'mysettings.references', label: 'Reference managers', icon: 'menu_book' },
      { id: 'mysettings.sessions', label: 'Sessions', icon: 'computer' },
      { id: 'mysettings.appearance', label: 'Appearance', icon: 'palette' },
      { id: 'mysettings.editordefaults', label: 'Editor defaults', icon: 'tune' },
      { id: 'mysettings.email', label: 'Email preferences', icon: 'mail' },
      {
        id: 'mysettings.llm',
        label: 'My LLM settings',
        icon: 'smart_toy',
        children: [
          { id: 'mysettings.llm.general', label: 'General', icon: 'settings' },
          { id: 'mysettings.llm.grammar', label: 'Grammar Checking', icon: 'spellcheck' },
          { id: 'mysettings.llm.compliance', label: 'Compliance Review', icon: 'verified' },
          { id: 'mysettings.llm.usage', label: 'Usage', icon: 'monitoring' },
        ],
      },
    ],
  },
  {
    id: 'site',
    label: 'Site settings',
    icon: 'tune',
    admin: true,
    tone: 'admin',
    children: [
      // Owner #16/#19 (2026-09-13): “Full site settings” + “Hub health” belong
      // directly under the Site settings header, not buried inside General.
      { id: 'site.general.enclose', label: 'Full site settings', icon: 'tune' },
      { id: 'site.general.health', label: 'Hub health', icon: 'health_and_safety' },
      {
        id: 'site.general',
        label: 'General',
        icon: 'layers',
        children: [
          { id: 'site.general.misc', label: 'Miscellaneous', icon: 'tune', render: 'site-sec', siteId: 'misc' },
          { id: 'site.general.appearance', label: 'Appearance', icon: 'palette' },
          { id: 'site.general.signup', label: 'Sign-up', icon: 'person_add', render: 'site-sec', siteId: 'signup' },
          { id: 'site.general.managetpl', label: 'Manage templates', icon: 'tag' },
          {
            id: 'site.general.projects',
            label: 'Projects',
            icon: 'folder',
            children: [
              { id: 'site.general.projects.all', label: 'All projects', icon: 'apps' },
              { id: 'site.general.projects.inactive', label: 'Inactive projects', icon: 'hourglass_empty' },
              { id: 'site.general.projects.trashed', label: 'Trashed projects', icon: 'delete' },
              { id: 'site.general.projects.deleted', label: 'Deleted projects', icon: 'delete_forever' },
            ],
          },
          {
            id: 'site.general.users',
            label: 'Users & access',
            icon: 'groups',
            children: [
              { id: 'site.general.users.all', label: 'All users', icon: 'groups' },
              { id: 'site.general.users.admins', label: 'Administrators', icon: 'shield_person' },
              { id: 'site.general.users.suspended', label: 'Suspended users', icon: 'pause_circle' },
              { id: 'site.general.users.inactive', label: 'Inactive users', icon: 'hourglass_empty' },
              { id: 'site.general.users.deleted', label: 'Deleted users', icon: 'delete_forever' },
            ],
          },
          { id: 'site.general.activeprojects', label: 'Active projects', icon: 'track_changes' },
          { id: 'site.general.messages', label: 'System messages', icon: 'campaign' },
          { id: 'site.general.stats', label: 'Instance statistics', icon: 'monitoring' },
          { id: 'site.general.editor', label: 'Editor controls', icon: 'build' },
        ],
      },
      {
        id: 'site.integrations',
        label: 'Integrations',
        icon: 'link',
        children: [
          { id: 'site.integrations.zotero', label: 'Zotero', icon: 'auto_stories', render: 'site-sec', siteId: 'zotero' },
          // Owner #8 (2026-09-07): Mendeley connector credentials (CLIENT_ID/SECRET + toggle).
          { id: 'site.integrations.mendeley', label: 'Mendeley', icon: 'menu_book', render: 'site-sec', siteId: 'mendeley' },
          { id: 'site.integrations.externalurl', label: 'External URLs', icon: 'link', render: 'site-sec', siteId: 'externalUrl' },
          { id: 'site.integrations.sso-saml', label: 'SSO · SAML', icon: 'verified_user', render: 'site-sec', siteId: 'sso-saml' },
          { id: 'site.integrations.sso-oidc', label: 'SSO · OIDC', icon: 'badge', render: 'site-sec', siteId: 'sso-oidc' },
          { id: 'site.integrations.sso-ldap', label: 'SSO · LDAP', icon: 'groups', render: 'site-sec', siteId: 'sso-ldap' },
        ],
      },
      {
        id: 'site.services',
        label: 'Services',
        icon: 'dns',
        children: [
          { id: 'site.services.email', label: 'Email / SMTP', icon: 'mail', render: 'site-sec', siteId: 'email' },
          { id: 'site.services.services', label: 'Services', icon: 'dns', render: 'site-sec', siteId: 'services' },
          { id: 'site.services.branding', label: 'Branding', icon: 'palette', render: 'site-sec', siteId: 'branding' },
          { id: 'site.services.grammar', label: 'Grammar (LT)', icon: 'spellcheck', render: 'site-sec', siteId: 'languagetool' },
        ],
      },
      {
        id: 'site.compilation',
        label: 'Compilation',
        icon: 'build',
        children: [
          { id: 'site.compilation.sandboxed', label: 'Sandboxed compiles', icon: 'build', render: 'site-sec', siteId: 'sandboxed-compiles' },
          { id: 'site.compilation.pandoc', label: 'Pandoc', icon: 'swap_vert', render: 'site-sec', siteId: 'pandoc' },
          { id: 'site.compilation.git', label: 'Git integration', icon: 'commit', render: 'site-sec', siteId: 'git-integration' },
          { id: 'site.compilation.github', label: 'GitHub sync', icon: 'cloud_sync', render: 'site-sec', siteId: 'github-sync' },
          { id: 'site.compilation.linkedfiletypes', label: 'Linked file types', icon: 'attachment', render: 'site-sec', siteId: 'linked-file-types' },
          { id: 'site.compilation.webdav', label: 'WebDAV', icon: 'cloud', render: 'site-sec', siteId: 'webdav' },
          { id: 'site.compilation.dropbox', label: 'Dropbox', icon: 'cloud_done', render: 'site-sec', siteId: 'dropbox' },
        ],
      },
      {
        id: 'site.llm',
        label: 'LLM Settings',
        icon: 'psychology',
        children: [
          { id: 'site.llm.features', label: 'Features', icon: 'bolt' },
          { id: 'site.llm.connection', label: 'API Connection', icon: 'link' },
          { id: 'site.llm.models', label: 'Model Selection', icon: 'memory' },
          { id: 'site.llm.prompt', label: 'System Prompt', icon: 'chat' },
          { id: 'site.llm.prompts', label: 'AI Prompts', icon: 'auto_awesome' },
          { id: 'site.llm.usage', label: 'Usage', icon: 'monitoring' },
        ],
      },
    ],
  },
]

export type HubIndex = {
  byId: Map<string, HubNode>
  byPrefix: Map<string, HubNode[]>
  ancestors: Map<string, string[]>
  allLeaves: Map<string, HubNode>
}

export function indexNav(nav: HubNode[]): HubIndex {
  const byId = new Map<string, HubNode>()
  const byPrefix = new Map<string, HubNode[]>()
  const ancestors = new Map<string, string[]>()
  const allLeaves = new Map<string, HubNode>()
  const walk = (n: HubNode, anc: string[]) => {
    byId.set(n.id, n)
    const children = n.children || []
    const arr = byPrefix.get(n.id) || []
    arr.push(n)
    byPrefix.set(n.id, arr)
    const kids: string[] = []
    children.forEach(c => {
      kids.push(c.id)
      const childAnc = [...anc, n.id]
      ancestors.set(c.id, childAnc)
      if (!c.children?.length) allLeaves.set(c.id, c)
      walk(c, childAnc)
    })
    if (!n.children || n.children.length === 0) allLeaves.set(n.id, n)
    return n
  }
  nav.forEach(n => {
    ancestors.set(n.id, [])
    walk(n, [])
  })
  return { byId, byPrefix, ancestors, allLeaves }
}

/** Visible tree for the given role (admin removes the admin subtree). */
export function visibleNav(nav: HubNode[], isAdmin: boolean): HubNode[] {
  if (isAdmin) return nav
  const strip = (ns: HubNode[]): HubNode[] => {
    const out: HubNode[] = []
    for (const n of ns) {
      if (n.admin) continue
      const c = { ...n }
      if (c.children?.length) c.children = strip(c.children)
      out.push(c)
    }
    return out
  }
  return strip(nav)
}
