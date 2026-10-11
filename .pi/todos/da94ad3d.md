{
  "id": "da94ad3d",
  "title": "Retire old UI routes + redirect stubs (owner decision 2026-10-05)",
  "tags": [
    "routes",
    "retirement",
    "frontend"
  ],
  "status": "done",
  "created_at": 1791060693621,
  "last_change": 1791507168815,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## DONE 2026-10-05 (owner decision day; commits df82bb02b8 + 356f640b5b + fe228fb6cc + 738e9edcdd + 15893deab5)

Route surface retired (404 now, were 301/302 into the hub):
- /project dashboards (owned/shared/archived/trashed/untagged/tags/:tag)
- /templates, /templates/manage, /template/:id hub-redirect rows
- /library, /library/trashed pages
- /launchpad page + register_admin|register_ldap_admin|register_saml_admin
- /user/mysettings, /user/notification-preferences redirect rows
- /admin/site, /admin/panel redirect rows
- /university + /university/* (new standalone pin: legacy-university 2/2)
- /Project/:id editor page prefix (editorpages: /editor only)

Kept (per owner binding): every API (/project/:id/*, /admin/*, /api/templates*,
/library/references*, /launchpad/send_test_email), /editor/:id, login/settings.

Live-run catches fixed on the way (each gate-fail → fix → gate-green):
- admin all-users list 500 on a 22-char non-OID owner_ref fixture row (738e9edcdd)
- GET /library/references fields:[] (bson.D vs map assertion; 15893deab5)
- POST /user/password/update Go parity gap (d8c249db31)
- launchpad first-admin → toolkit bootstrap CLI (df82bb02b8)
- spec settlement: ~24 dual-stack gates retired (post-P7), p51a/p51b reworked
  single-stack, p4d 429 backoff, auth.ts/u101-history path fixes.

Green: legacy family 57/57, hub admin-projects 8/8, hub-library 6/6, p51a/p51b,
u101-history, u103r, p0/p1-auth/p4x batteries.

