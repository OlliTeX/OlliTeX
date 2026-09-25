export function Hotkey({
  combination,
  description,
}: {
  combination: string
  description: string
}) {
  return (
    <div className="hotkey" data-test-selector="hotkey">
      <span className="combination">{combination}</span>
      <span className="description">{description}</span>
    </div>
  )
}
