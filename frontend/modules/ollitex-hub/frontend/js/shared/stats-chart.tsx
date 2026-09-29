import React, { useMemo } from 'react'
import { Text } from '@mantine/core'

/**
 * stats-chart (2026-09-08, owner request): dependency-free SVG time-series
 * chart for the hub's instance-statistics leaf.
 *
 * Ports the legacy /admin/instance-stats Plotly contract (line default,
 * optional bar, 1–2 series with y1/y2 colors+labels, [0, max] y-range,
 * ~6 date ticks, per-point hover) into the hub bundle without the Plotly
 * dependency — the hub page does not load the classic page's vendor scripts.
 */
export interface StatsSeriesPoint {
  /** epoch ms (collector buckets by day) */
  day: number
  values: number[]
}

export interface StatsChartProps {
  points: StatsSeriesPoint[]
  /** 1 or 2 series colors (y1, optional y2) */
  colors: string[]
  /** series labels for the 2-series legend (labels.y1 / labels.y2) */
  labels?: { y1?: string; y2?: string }
  transform?: (v: number) => number
  ylabel?: string
  chartType?: 'line' | 'bar'
  height?: number
}

const W = 560
const PAD_L = 46
const PAD_R = 10
const PAD_T = 12
const PAD_B = 26

function fmtAxis(n: number): string {
  const v = Math.abs(n)
  if (v >= 1e9) return `${(n / 1e9).toFixed(v >= 1e10 ? 0 : 1)}B`
  if (v >= 1e6) return `${(n / 1e6).toFixed(v >= 1e7 ? 0 : 1)}M`
  if (v >= 1e4) return `${(n / 1e3).toFixed(0)}k`
  if (Number.isInteger(n)) return String(n)
  if (v >= 100) return n.toFixed(0)
  if (v >= 1) return n.toFixed(1)
  return n.toFixed(2)
}

function fmtDate(ms: number): string {
  const d = new Date(ms)
  return d.toLocaleDateString(undefined, { day: '2-digit', month: 'short', year: 'numeric' })
}

export default function StatsChart({
  points,
  colors,
  labels,
  transform = v => v,
  ylabel,
  chartType = 'line',
  height = 150,
}: StatsChartProps) {
  const model = useMemo(() => {
    const pts = (points || []).filter(p => p && Array.isArray(p.values) && p.values.length > 0)
    if (pts.length === 0) return null

    const seriesCount = pts.reduce((m, p) => Math.max(m, p.values.length), 0) >= 2 ? 2 : 1
    const indexes = seriesCount === 2 ? [0, 1] : [0]

    const seriesVals = indexes.map(i => pts.map(p => transform(p.values[i] ?? 0)))
    const maxV = chartType === 'bar'
      ? Math.max(0, ...pts.map(p => indexes.reduce((s, i) => s + (p.values[i] ?? 0), 0)))
      : Math.max(0, ...seriesVals.flat())
    const yMax = maxV > 0 ? maxV : 1

    const xs = pts.map(p => p.day)
    const minT = Math.min(...xs)
    const maxT = Math.max(...xs)
    const span = Math.max(1, maxT - minT)

    const innerW = W - PAD_L - PAD_R
    const innerH = height - PAD_T - PAD_B

    const x = (t: number) => PAD_L + ((t - minT) / span) * innerW
    const y = (v: number) => PAD_T + innerH - (v / yMax) * innerH

    // ~5 axis ticks on both axes (nice rounded numbers)
    const yTicks: number[] = []
    for (let k = 0; k <= 4; k++) yTicks.push((yMax / 4) * k)
    const xTickCount = Math.min(6, pts.length)
    const xTicks = Array.from({ length: xTickCount }, (_, k) =>
      xTickCount === 1 ? minT : minT + (span * k) / (xTickCount - 1),
    )

    return { pts, indexes, seriesVals, yMax, x, y, yTicks, xTicks, innerH }
  }, [points, transform, chartType, height])

  if (!model) {
    return (
      <Text size="sm" c="dimmed" style={{ padding: '12px 4px' }}>
        No data in this window yet (the collector samples once per day).
      </Text>
    )
  }

  const { pts, indexes, seriesVals, x, y, yTicks, xTicks } = model
  const axisColor = 'var(--mantine-color-dimmed)'
  const gridColor = 'color-mix(in srgb, var(--mantine-color-dimmed) 25%, transparent)'

  const barW = Math.max(
    2,
    Math.min(18, ((W - PAD_L - PAD_R) / Math.max(1, pts.length)) * 0.8 / indexes.length),
  )

  return (
    <div>
      <svg
        viewBox={`0 0 ${W} ${height}`}
        width="100%"
        height={height}
        role="img"
        aria-label={ylabel ? `chart, ${ylabel}` : 'time series chart'}
        style={{ display: 'block', fontFamily: 'inherit' }}
      >
        {/* grid + y axis */}
        {yTicks.map((t, i) => (
          <g key={`yt${i}`}>
            <line x1={PAD_L} x2={W - PAD_R} y1={y(t)} y2={y(t)} stroke={gridColor} strokeWidth={1} />
            <text
              x={PAD_L - 6}
              y={y(t) + 3.5}
              textAnchor="end"
              fontSize={10}
              fill={axisColor}
            >
              {fmtAxis(t)}
            </text>
          </g>
        ))}
        {/* x ticks */}
        {xTicks.map((t, i) => (
          <text
            key={`xt${i}`}
            x={x(t)}
            y={height - 8}
            textAnchor={i === 0 ? 'start' : i === xTicks.length - 1 ? 'end' : 'middle'}
            fontSize={10}
            fill={axisColor}
          >
            {fmtDate(t)}
          </text>
        ))}
        {ylabel ? (
          <text
            x={8}
            y={PAD_T + 2}
            fontSize={10}
            fill={axisColor}
          >
            {ylabel}
          </text>
        ) : null}

        {indexes.map((idx, si) => {
          const color = colors[si] || colors[0]
          const seriesLabel = indexes.length === 2 ? (labels?.[`y${si + 1}`] || `series ${si + 1}`) : undefined
          if (chartType === 'bar' && pts.length > 1) {
            // grouped bars (legacy used stacked; grouped reads better at small sizes)
            return (
              <g key={`s${si}`}>
                {pts.map((p, k) => {
                  const v = transform(p.values[idx] ?? 0)
                  const cx = x(p.day)
                  const bx = cx - (indexes.length * barW) / 2 + si * barW
                  const by = y(v)
                  return (
                    <rect
                      key={`b${k}`}
                      x={bx}
                      y={by}
                      width={barW}
                      height={Math.max(0, height - PAD_B - by)}
                      fill={color}
                      opacity={0.85}
                    >
                      <title>{`${fmtDate(p.day)}${seriesLabel ? ` · ${seriesLabel}` : ''}: ${fmtAxis(v)}${ylabel ? ` ${ylabel}` : ''}`}</title>
                    </rect>
                  )
                })}
              </g>
            )
          }
          return (
            <g key={`s${si}`}>
              <polyline
                fill="none"
                stroke={color}
                strokeWidth={2}
                strokeLinejoin="round"
                strokeLinecap="round"
                points={seriesVals[si].map((v, k) => `${x(pts[k].day)},${y(v)}`).join(' ')}
              />
              {pts.length <= 40
                ? pts.map((p, k) => (
                    <circle key={`c${k}`} cx={x(p.day)} cy={y(seriesVals[si][k])} r={2.6} fill={color}>
                      <title>{`${fmtDate(p.day)}${seriesLabel ? ` · ${seriesLabel}` : ''}: ${fmtAxis(seriesVals[si][k])}${ylabel ? ` ${ylabel}` : ''}`}</title>
                    </circle>
                  ))
                : null}
            </g>
          )
        })}

        {/* legend (2 series only, mirrors the legacy plot legend) */}
        {indexes.length === 2 ? (
          <g>
            {indexes.map((idx, si) => {
              const lx = PAD_L + si * 120
              return (
                <g key={`lg${si}`}>
                  <rect x={lx} y={4} width={10} height={10} fill={colors[si] || colors[0]} rx={2} />
                  <text x={lx + 14} y={13} fontSize={11} fill={axisColor}>
                    {labels?.[`y${si + 1}`] || `series ${si + 1}`}
                  </text>
                </g>
              )
            })}
          </g>
        ) : null}
      </svg>
    </div>
  )
}
