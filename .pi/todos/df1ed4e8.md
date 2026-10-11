{
  "id": "df1ed4e8",
  "title": "Overleaf 6.3.0 port — Round-5 theme/CSS batch + github-sync workdir (8 items)",
  "tags": [
    "done",
    "round5",
    "overleaf",
    "deployment"
  ],
  "status": "done",
  "created_at": 1788525006226,
  "last_change": 1788525006226,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

Round 5 (8 live items) COMPLETE and verified 13/13 on deployed image b8c79690f50b; commit f9ff6ce09c pushed to origin (davrot/6.3.0_post, branch ext-6.3.0-port). Fixes: /project logo off (list-react.pug hideNavLogo); /user/llm-settings + /admin/llm/settings + /user/notification-preferences rebuilt on golden /admin/site + /user/mysettings shell (new ds-settings-shell entry, scroll+tab nav models, working account menu + theme toggle); instance-stats Alert-settings padding; ask-ai + rail-chat dark-mode contrast (llm-editor-theme bridge → --content-primary-themed; llm-chat welcome h3); githubinterface workdir contract (shared host dir /data_1/docker/overleaf-ghif → /var/lib/ghif on web+sidecar, GITHUBINTERFACE_WORKDIR_ROOT on web; clone/commit chain verified live). STILL OPEN: (1) GitHub push e2e — needs test user's GitHub token; (2) ext_explain.md docs; (3) in-repo regression tests for BYO/Zotero/compile fixes.
