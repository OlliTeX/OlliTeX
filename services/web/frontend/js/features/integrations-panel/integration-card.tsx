
export default function IntegrationCard({
  href,
  onClick,
  title,
  description,
  icon,
  titleBadge,
}: {
  href?: string
  onClick?: () => void
  title: string
  description: string
  icon: React.ReactNode
  titleBadge?: React.ReactNode
}) {
  const content = (
    <div className="integrations-panel-card-contents">
      <div className="integrations-panel-card-icon">{icon}</div>
      <div className="integrations-panel-card-inner">
        <div className="integrations-panel-card-header">
          <div className="integrations-panel-card-title" translate="no">
            {title}
          </div>
          {titleBadge}
        </div>
        <p className="integrations-panel-card-description">{description}</p>
      </div>
    </div>
  )

  if (href) {
    return (
      <a
        href={href}
        target="_blank"
        rel="noreferrer"
        className="integrations-panel-card-button"
      >
        {content}
      </a>
    )
  }

  return (
    <button onClick={onClick} className="integrations-panel-card-button">
      {content}
    </button>
  )
}
