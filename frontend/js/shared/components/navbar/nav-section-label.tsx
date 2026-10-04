/**
 * NavSectionLabel — a non-interactive section heading inside the account
 * menu (functional grouping, owner UX 2026-10-06: the flat 14-row menu
 * scrolled; groups are now Workspace / Personal / Manage instance /
 * Sign out). role=presentation keeps it outside the menu-items count for
 * screen readers (same rationale as the log-out form item above it).
 */
export default function NavSectionLabel({ children }: { children: string }) {
  return (
    <li role="presentation" className="nav-section">
      <span className="nav-section-label">{children}</span>
    </li>
  )
}
