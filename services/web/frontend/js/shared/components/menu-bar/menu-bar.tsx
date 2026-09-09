import { NestableDropdownContextProvider } from '@/shared/context/nestable-dropdown-context'
import { FC, HTMLProps } from 'react'

export const MenuBar: FC<
  React.PropsWithChildren<HTMLProps<HTMLDivElement> & { id: string }>
> = ({ children, id, ...props }) => {
  return (
    // No role="menubar": the menu bar holds dropdown triggers (buttons),
    // not ARIA menuitems — the menubar role requires menuitem/group
    // children (axe: aria-required-children) which this surface does not
    // provide. A plain named region is the honest role here.
    <div {...props} role="group">
      <NestableDropdownContextProvider id={id}>
        {children}
      </NestableDropdownContextProvider>
    </div>
  )
}
