/**
 * Typst module (TYPST_PHASES F2.9): "Typst project" entry for the
 * New-Project dropdown on the project list.
 *
 * Rendered INTO `new-project-button.tsx` from
 * `Settings.overleafModuleImports['typstNewProjectMenu']` via the
 * build-time `importOverleafModules` macro (same pattern as the
 * github-import menu). The old fork (/home/davrot/typst_test/typst)
 * registered this menu unconditionally; this repo deploys flag on/off
 * from ONE release image (plan §12), so the visibility gate lives at
 * RUNTIME on the host (`ExposedSettings.typstEnabled`, set from
 * `Settings.typst.enabled` / `COMPILE_TYPEST_ENABLED`) — not at build
 * time.
 */
import { useTranslation } from 'react-i18next'
import { OLDropdownItem } from '@/shared/components/ol/ol-dropdown-menu'

export default function TypstNewProjectMenu({
  onClick,
}: {
  onClick: (e: React.MouseEvent) => void
}) {
  const { t } = useTranslation()

  return <OLDropdownItem onClick={onClick}>{t('blank_typst_project')}</OLDropdownItem>
}
