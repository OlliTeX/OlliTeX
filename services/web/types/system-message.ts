/** Surfaces a system message can be scoped to (#17b, owner 2026-09-13). */
export type SystemMessagePlacement = 'editor' | 'hub' | 'auth'

export type SystemMessage = {
  _id: string
  content: string
  /**
   * Where this message appears ('editor' | 'hub' | 'auth').
   * Missing or empty list → visible on ALL pages (legacy behavior).
   */
  placements?: SystemMessagePlacement[]
}

export type SuggestedLanguage = {
  url: string
  imgUrl: string
  lngCode: string
  lngName: string
}
