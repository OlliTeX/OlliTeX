import {
  createContext,
  Dispatch,
  FC,
  SetStateAction,
  useEffect,
  useState,
} from 'react'

export type NestableDropdownContextType = {
  selected: string | null
  setSelected: Dispatch<SetStateAction<string | null>>
  menuId: string
  /** 2026-10-09 (owner item AC): when true, nested targets expand their
   *  children INLINE inside the parent dropdown (accordion) instead of the
   *  Bootstrap flyout. The flyout misbehaves inside Mantine Menu portals
   *  (z/position/hover gaps) — inline expansion is deterministic in both
   *  the legacy menu bar and the v2 rail menus. Defaults false (legacy). */
  inline?: boolean
}

export const NestableDropdownContext = createContext<
  NestableDropdownContextType | undefined
>(undefined)

export const NestableDropdownContextProvider: FC<
  React.PropsWithChildren<{ id: string; inline?: boolean }>
> = ({ id, inline = false, children }) => {
  const [selected, setSelected] = useState<string | null>(null)

  useEffect(() => {
    return () => {
      // unset selection on unmount
      setSelected(null)
    }
  }, [])

  return (
    <NestableDropdownContext.Provider
      value={{ selected, setSelected, menuId: id, inline }}
    >
      {children}
    </NestableDropdownContext.Provider>
  )
}
