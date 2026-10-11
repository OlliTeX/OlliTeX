{
  "id": "0d72dacf",
  "title": "Wiki (user/admin) + automated, data-safe screenshots — WRITE A PLAN",
  "tags": [
    "docs",
    "wiki",
    "screenshots",
    "data-protection",
    "plan"
  ],
  "status": "done",
  "created_at": 1789059342441,
  "last_change": 1790665047168,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

Owner queue (item 3). Build a WIKI (markdown files + (sub-)sub-folders organizing topics) that explains to USERS and ADMINS how to use OlliTeX. Owner wants a PLAN first ("Please write me a plan for that"), so deliver the plan before writing the wiki content.

Two hard requirements drive the plan:
a.) Screenshots are made AUTOMATICALLY — regeneratable/updatable when the project changes (a Playwright-driven screenshot pipeline keyed off the running stack, committed to the wiki's assets dir, with a stable shot-name -> route mapping so docs reference stable IDs).
b.) PROTECT REAL DATA from appearing in screenshots — redact/mask real user names & email addresses, and credentials / API keys / API secrets. Strategy: screenshot against a SEEDED/FIXTURE stack (the e2e env with synthetic users/projects, never the prod DB); plus a redaction pass (pixel/overlay or DOM text replacement of email/secret patterns, fixed synthetic keys); plus avoid pages that print real emails/secrets (or seed innocuous values first).

Plan should cover: wiki IA (folder tree: Getting started, User workflow, Editor, Hub/My settings, Admin/site, LLM, Modules, Deployment/ops, Troubleshooting), which topics need which screenshots, the automated screenshot generator design (scripts + storage + regeneration trigger + CI/optional), the data-protection approach (fixture stack + redaction + secret scrubbing), and a delivery sequence. Keep it a PLAN for owner approval — do not start writing content until approved.
