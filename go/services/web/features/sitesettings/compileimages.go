// AK-5 (owner 2026-10-08): the editor settings modal → Compiler pane was
// "missing the Sandbox features (e.g. which texlive docker image)".
//
// The surface already exists in the frontend (ImageNameSetting renders the
// per-project compile-image select when the `ol-imageNames` meta is
// non-empty) — but the meta was pinned to [] (editorpages/pinned.go), so
// the select silently disappeared for everyone. The instance's image list
// lives in the site-settings `sandboxed-compiles` section (configstore /
// Mongo doc + env seeds: ALL_TEX_LIVE_DOCKER_IMAGES[_NAMES],
// TEX_LIVE_DOCKER_IMAGE).
//
// This file resolves that section into the meta shape the frontend
// expects: [{imageName, imageDesc, allowed}] (image-name-setting.tsx).
package sitesettings

import (
	"context"

	"ollitex/go/services/web/core"
)

// CompileImageNames — the selectable sandbox compile images (texlive
// docker images) derived from the `sandboxed-compiles` section. All
// images the instance lists are allowed (the admin curates the list on
// the admin Compilation page; there is no per-image permission split).
// The instance default (defaultImage) is kept in the list even if the
// admin trimmed it from the rows, so the select always has a valid
// value.
func CompileImageNames(a *core.App, ctx context.Context) []map[string]any {
	out := []map[string]any{}
	if a == nil || a.Mongo == nil {
		return out
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return out
	}
	sections := loadAllSections(ctx, db)
	merged := getSection("sandboxed-compiles", sections, nil)

	images, _ := ObjGet(merged, "images")
	d, _ := ObjGet(merged, "defaultImage")
	def, _ := d.(string)

	seen := map[string]bool{}
	add := func(image, name string) {
		if image == "" || seen[image] {
			return
		}
		seen[image] = true
		desc := name
		if desc == "" {
			desc = image
		}
		out = append(out, map[string]any{
			"imageName": image,
			"imageDesc": desc,
			"allowed":   true,
		})
	}

	switch v := images.(type) {
	case []any:
		for i, im := range v {
			m, _ := im.(Obj)
			imVal, _ := ObjGet(m, "image")
			image, _ := imVal.(string)
			nmVal, _ := ObjGet(m, "name")
			name, _ := nmVal.(string)
			if name == "" {
				if names, ok2 := ObjGet(merged, "names"); ok2 {
					if list, ok3 := names.([]any); ok3 && i < len(list) {
						if s, ok4 := list[i].(string); ok4 {
							name = s
						}
					}
				}
			}
			add(image, name)
		}
	}
	if def != "" {
		add(def, def)
	}
	return out
}
