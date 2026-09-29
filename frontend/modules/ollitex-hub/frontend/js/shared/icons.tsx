import React from 'react'

/**
 * Material Symbols icon — OL's own icon font (.material-symbols class from
 * frontend/fonts/material-symbols/material-symbols.css, imported by the page
 * entries). The ligature text (e.g. 'folder') renders as a glyph once the
 * font is loaded; kept as visible text as a no-font fallback.
 */
export default function Icon({
  name,
  size = 20,
  style,
}: {
  name: string
  size?: number
  style?: React.CSSProperties
}) {
  return (
    <span
      className="material-symbols"
      style={{
        fontSize: size,
        lineHeight: 1,
        display: 'inline-block',
        verticalAlign: 'middle',
        flexShrink: 0,
        ...style,
      }}
      aria-hidden="true"
    >
      {name}
    </span>
  )
}
