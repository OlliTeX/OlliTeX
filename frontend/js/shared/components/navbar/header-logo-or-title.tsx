import type { DefaultNavbarMetadata } from '@/shared/components/types/default-navbar-metadata'
import getMeta from '@/utils/meta'

export default function HeaderLogoOrTitle({
  overleafLogo,
  customLogo,
  title,
}: Pick<DefaultNavbarMetadata, 'customLogo' | 'title'> & {
  overleafLogo?: string
}) {
  const { appName } = getMeta('ol-ExposedSettings')
  // 2026-09-03 (owner): app/admin/user pages must NOT show the brand logo in
  // the navbar — only the public marketing pages keep it. Pages opt in via
  // the ol-navbar meta flag `hideLogo` (set by layout-react from the view
  // local `hideNavLogo`).
  const hideLogo = !!(getMeta('ol-navbar') as { hideLogo?: boolean })?.hideLogo
  if (hideLogo) {
    return null
  }
  const logoUrl = customLogo ?? overleafLogo
  return (
    <a href="/" aria-label={appName} className="navbar-brand">
      {(customLogo || !title) && (
        <div
          className="navbar-logo"
          style={logoUrl ? { backgroundImage: `url("${logoUrl}")` } : {}}
        />
      )}
      {title && (
        <div className="navbar-title">
          <span>{title}</span>
        </div>
      )}
    </a>
  )
}
