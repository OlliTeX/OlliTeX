import type {
  NavbarItemData,
  NavbarSessionUser,
} from '@/shared/components/types/navbar'

export type DefaultNavbarMetadata = {
  customLogo?: string
  customLogoDark?: string
  title?: string
  canDisplayAdminMenu: boolean
  canDisplayAdminRedirect: boolean
  canDisplayProjectUrlLookup: boolean
  canDisplaySplitTestMenu: boolean
  canDisplaySurveyMenu: boolean
  canDisplayScriptLogMenu: boolean
  suppressNavbarRight: boolean
  canDisplayInstanceStats?: boolean
  suppressNavContentLinks: boolean
  showCloseIcon?: boolean
  showSignUpLink: boolean
  currentUrl: string
  sessionUser?: NavbarSessionUser
  adminUrl?: string
  items: NavbarItemData[]
}
