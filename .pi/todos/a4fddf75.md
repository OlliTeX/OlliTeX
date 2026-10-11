{
  "id": "a4fddf75",
  "title": "Step 1: /hub/workspace Mantine-native (Projects/Library/Templates + user Settings)",
  "tags": [
    "hub",
    "workspace",
    "mantine"
  ],
  "status": "done",
  "created_at": 1788698295848,
  "last_change": 1788782387812,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

Owner 2026-09-06: all content UI on Mantine 9.6.0; integrate sub-menus of /project, /library, /templates AND user settings pages (/user/mysettings, /user/notification-preferences, /user/llm-settings) into the new menu. Fork freedom: rearrange for better UX, no old-compat needed.
Menu: Projects | Library | Templates | Settings{ My settings, Notifications, LLM }
- Projects: Mantine list (search, cards/table, actions, new-project form)
- Library: Mantine (tabs Library/Trash, add reference, entries, delete)
- Templates: Mantine gallery (search/filter, use template)
- Settings sections: Mantine forms (useForm) / switches / tabs
- reuse existing API layer (fetch-json, contexts); do not fork APIs
- GATES: e2e + build + owner visual
