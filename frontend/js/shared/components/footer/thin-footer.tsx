import type {
  FooterItem,
  FooterMetadata,
} from '@/shared/components/types/footer-metadata'
import OLRow from '@/shared/components/ol/ol-row'
import OLCol from '@/shared/components/ol/ol-col'
import LanguagePicker from '@/shared/components/language-picker'
import getMeta from '@/utils/meta.ts'
import React from 'react'

function FooterItemLi({
  text,
  translatedText,
  url: href,
  class: className,
  label,
}: FooterItem) {
  const textToDisplay = translatedText || text
  const hasMarkup = /<[^>]+>/.test(textToDisplay)

  if (!href) {
    return hasMarkup ? (
      <li dangerouslySetInnerHTML={{ __html: textToDisplay }} />
    ) : (
      <li>{textToDisplay}</li>
    )
  }

  const linkProps = {
    href,
    className,
    'aria-label': label,
  }

  return (
    <li>
      {hasMarkup ? (
        <a {...linkProps} dangerouslySetInnerHTML={{ __html: textToDisplay }} />
      ) : (
        <a {...linkProps}>{textToDisplay}</a>
      )}
    </li>
  )
}

function Separator() {
  return (
    <li role="separator" className="text-muted">
      <strong>|</strong>
    </li>
  )
}

function ThinFooter({
  showPoweredBy,
  subdomainLang,
  leftItems,
  rightItems,
}: FooterMetadata) {
  const { env } = getMeta('ol-ExposedSettings')
  const showLanguagePicker = Boolean(
    subdomainLang && Object.keys(subdomainLang).length > 1
  )

  const hasCustomLeftNav = Boolean(leftItems && leftItems.length > 0)

  return (
    <footer className="site-footer">
      <div className="site-footer-content d-print-none">
        <OLRow>
          <ul className="site-footer-items col-lg-9">
            {showPoweredBy ? (
              <>
                <li>
                  {/* year of initial release, static */}© 2026{' '}
                  <a
                    className="site-footer-attribution-link"
                    href="https://github.com/davrot/6.3.0_post"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    OlliTeX
                  </a>{' '}
                  — a fork of{' '}
                  <a
                    className="site-footer-attribution-link"
                    href="https://github.com/overleaf/overleaf"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    Overleaf Community Edition
                  </a>{' '}
                  (open source, AGPLv3)
                </li>
                {showLanguagePicker || hasCustomLeftNav ? <Separator /> : null}
              </>
            ) : null}
            {showLanguagePicker ? (
              <>
                <li>
                  <LanguagePicker showHeader />
                </li>
                {hasCustomLeftNav ? <Separator /> : null}
              </>
            ) : null}
            {leftItems?.map(item => (
              <FooterItemLi key={item.text} {...item} />
            ))}
          </ul>
          <ul className="site-footer-items col-lg-3 text-end">
            {rightItems?.map(item => (
              <FooterItemLi key={item.text} {...item} />
            ))}
          </ul>
        </OLRow>
        {env === 'server-ce' && (
          <OLRow>
            <OLCol lg={12}>
              <p className="copyright-notice">
                OlliTeX is a fork of Overleaf Community Edition™, an
                open-source project by Overleaf, distributed under the terms
                of the AGPLv3 license. This deployment is not operated, supported
                or endorsed by Overleaf® and is provided "AS IS" with all
                liability disclaimed (to the fullest extent lawful) and all
                rights reserved.
              </p>
            </OLCol>
          </OLRow>
        )}
      </div>
    </footer>
  )
}

export default ThinFooter
