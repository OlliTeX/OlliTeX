import express from 'express'
import logger from '@overleaf/logger'

import AuthenticationController from '../../../../app/src/Features/Authentication/AuthenticationController.mjs'
import RateLimiterMiddleware from '../../../../app/src/Features/Security/RateLimiterMiddleware.mjs'
import { RateLimiter } from '../../../../app/src/infrastructure/RateLimiter.mjs'
import TemplateGalleryController from './TemplateGalleryController.mjs'
import TemplateAuthorizationMiddleware from './TemplateAuthorizationMiddleware.mjs'
import { ensureGalleryEnabled } from './TemplateGallerySection.mjs'

const rateLimiterNewTemplate = new RateLimiter('create-template-from-project', {
  points: 20,
  duration: 60,
})
const rateLimiter = new RateLimiter('template-gallery', {
  points: 60,
  duration: 60,
})
const rateLimiterThumbnails = new RateLimiter('template-gallery-thumbnails', {
  points: 240,
  duration: 60,
})

export default {
  rateLimiter,
  apply(webRouter) {
    logger.debug({}, 'Init templates router')

    webRouter.post(
      '/template/new/:Project_id',
      // 2026-09 (publish regression): this module route mounts ahead of the
      // app-level body parsers, so req.body arrives EMPTY and the publish
      // fails with a Zod "buildId undefined" (the "recompile" 400). Parse the
      // body right here, per-route, like the upload routes do.
      express.json({ limit: '160mb' }),
      express.urlencoded({ extended: true, limit: '160mb' }),
      ensureGalleryEnabled,
      AuthenticationController.requireLogin(),
      RateLimiterMiddleware.rateLimit(rateLimiterNewTemplate),
      TemplateAuthorizationMiddleware.ensureTemplateManagementAccess,
      TemplateGalleryController.createTemplateFromProject
    )

    // 2026-09 (owner items 7+9): the legacy template PAGES are retired — the
    // gallery lives at /hub#/templates.all and management at
    // /hub#/site.general.managetpl. Old deep links/bookmarks 301-redirect
    // there instead of 404-ing.
    webRouter.get(
      '/template/:template_id',
      (req, res) => res.redirect(301, '/hub#/templates.all')
    )

    // 3b (2026-08-28): template bundle save/import (admin console).
    // Export: management access (admin, or the owning non-admin manager).
    // Import: same as create — site admin / configured template manager;
    // per-category publishable still enforced in the manager for the rest.
    // R6 (2026-08-29): admin-facing endpoints must work even while the
    // public gallery is switched OFF (ensureGalleryEnabled 404s them —
    // that's what broke the admin console's bundle card).
    webRouter.get(
      '/api/templates/admin-list',
      AuthenticationController.requireLogin(),
      RateLimiterMiddleware.rateLimit(rateLimiter),
      TemplateAuthorizationMiddleware.ensureTemplateManagementAccess,
      TemplateGalleryController.getAdminTemplateListJSON
    )

    // 8b (2026-09-16, owner): the hub gallery offers "Download bundle" to ALL
    // signed-in users (parity with the legacy detail page's open card actions),
    // so the bundle export is no longer management-only (login + rate limit stay).
    // IMPORTS stay management-only (import replaces published templates).
    webRouter.get(
      '/template/:template_id/bundle',
      AuthenticationController.requireLogin(),
      RateLimiterMiddleware.rateLimit(rateLimiterNewTemplate),
      TemplateGalleryController.downloadTemplateBundle
    )
    webRouter.post(
      '/template/bundle/import',
      AuthenticationController.requireLogin(),
      RateLimiterMiddleware.rateLimit(rateLimiterNewTemplate),
      TemplateAuthorizationMiddleware.ensureTemplateManagementAccess,
      TemplateGalleryController.importTemplateBundle
    )

    // R6 item 5 (2026-08-29): import a bundle from a URL (SSRF-guarded by
    // the External URLs site policy) — same access rules as file import
    // (admin / template gallery admin). No ensureGalleryEnabled: restoring
    // templates must work while the public gallery is switched off.
    webRouter.post(
      '/template/bundle/import-url',
      express.json({ limit: '160mb' }),
      AuthenticationController.requireLogin(),
      RateLimiterMiddleware.rateLimit(rateLimiterNewTemplate),
      TemplateAuthorizationMiddleware.ensureTemplateManagementAccess,
      TemplateGalleryController.importTemplateBundleFromUrl
    )

    // 2026-09 (owner item 7): /templates/manage retired → hub admin leaf.
    // Must stay registered before /templates/:category? below.
    webRouter.get(
      '/templates/manage',
      AuthenticationController.requireLogin(),
      (req, res) => res.redirect(301, '/hub#/site.general.managetpl')
    )

    webRouter.post(
      '/template/:template_id/edit',
      ensureGalleryEnabled,
      express.json({ limit: '160mb' }),
      AuthenticationController.requireLogin(),
      RateLimiterMiddleware.rateLimit(rateLimiter),
      TemplateAuthorizationMiddleware.ensureTemplateManagementAccess,
      TemplateGalleryController.editTemplate
    )

    webRouter.delete(
      '/template/:template_id/delete',
      ensureGalleryEnabled,
      AuthenticationController.requireLogin(),
      RateLimiterMiddleware.rateLimit(rateLimiter),
      TemplateAuthorizationMiddleware.ensureTemplateManagementAccess,
      TemplateGalleryController.deleteTemplate
    )

    // 2026-09 (owner item 9): /templates (any category) retired → hub gallery.
    webRouter.get(
      '/templates/:category?',
      (req, res) => res.redirect(301, '/hub#/templates.all')
    )

    webRouter.get(
      '/api/template',
      ensureGalleryEnabled,
      RateLimiterMiddleware.rateLimit(rateLimiter),
      TemplateGalleryController.getTemplateJSON
    )

    // New 3 (2026-08-28): enabled categories (public read; the gallery is
    // public by design) for the Templates sub-items in the nav switcher.
    webRouter.get(
      '/api/template/categories',
      ensureGalleryEnabled,
      RateLimiterMiddleware.rateLimit(rateLimiter),
      TemplateGalleryController.getCategoriesJSON
    )

    webRouter.get(
      '/api/templates',
      ensureGalleryEnabled,
      RateLimiterMiddleware.rateLimit(rateLimiter),
      TemplateGalleryController.getCategoryTemplatesJSON
    )

    webRouter.get(
      '/template/:template_id/preview',
      ensureGalleryEnabled,
      (req, res, next) => {
        const limiter = req.query.style === 'thumbnail' ? rateLimiterThumbnails : rateLimiter
        RateLimiterMiddleware.rateLimit(limiter)(req, res, next)
      },
      TemplateGalleryController.getTemplatePreview
    )
  },
}
