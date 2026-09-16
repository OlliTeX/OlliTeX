package registrationpage

import (
	"go.mongodb.org/mongo-driver/bson"
)

// newUserDoc returns the Node-parity user document for a freshly registered
// account (registration-page leaf, P3.4). Static defaults captured from the
// live Node user (int32 numerics per the wire). The dynamic fields (_id,
// email, first_name, last_name, analyticsId, hashedPassword, signUpDate,
// emails, thirdPartyIdentifiers) are set by the caller.

func newUserDoc() bson.M {
	d := bson.M{
		"enrollment": bson.M{
			"sso": bson.A{},
		},
		"flags": bson.M{
			"canManageTemplates": false,
		},
		"ace": bson.M{
			"zotero": bson.M{
				"enabled":                true,
				"disablePersonalLibrary": false,
				"groups":                 bson.A{},
			},
			"mendeley": bson.M{
				"enabled":                true,
				"disablePersonalLibrary": false,
				"groups":                 bson.A{},
			},
			"papers": bson.M{
				"enabled":                true,
				"disablePersonalLibrary": false,
				"groups":                 bson.A{},
			},
			"mode":                 "none",
			"theme":                "textmate",
			"lightTheme":           "textmate",
			"darkTheme":            "overleaf_dark",
			"fontSize":             int32(12),
			"autoComplete":         true,
			"autoPairDelimiters":   true,
			"spellCheckLanguage":   "en",
			"pdfViewer":            "pdfjs",
			"previewTabs":          false,
			"mathPreview":          true,
			"breadcrumbs":          false,
			"editorTabs":           true,
			"nonBlinkingCursor":    false,
			"referencesSearchMode": "advanced",
			"darkModePdf":          false,
			"floatingMenu":         true,
			"customKeybindings":    bson.M{},
			"syntaxValidation":     true,
		},
		"features": bson.M{
			"collaborators":  int32(-1),
			"versioning":     true,
			"dropbox":        true,
			"github":         true,
			"gitBridge":      true,
			"compileTimeout": int32(180),
			"compileGroup":   "standard",
			"references":     true,
			"trackChanges":   true,
			"aiUsageQuota":   "basic",
			"offlineMode":    false,
		},
		"refProviders": bson.M{},
		"writefull": bson.M{
			"initialized":        false,
			"autoCreatedAccount": false,
			"isPremium":          false,
			"premiumSource":      nil,
		},
		"aiFeatures": bson.M{
			"enabled": true,
		},
		"overleaf":                bson.M{},
		"twoFactorAuthentication": bson.M{},
		"dsMobileApp":             bson.M{},
		"stripeCustomerIds":       bson.M{},
		"grammar": bson.M{
			"mode":         "default",
			"llmModel":     "",
			"language":     "auto",
			"blockedRules": bson.A{},
		},
		"role":                "",
		"institution":         "",
		"isAdmin":             false,
		"adminRoles":          bson.A{},
		"lastLoginIp":         "",
		"loginCount":          int32(0),
		"holdingAccount":      false,
		"must_reconfirm":      false,
		"refered_users":       bson.A{},
		"refered_user_count":  int32(0),
		"alphaProgram":        false,
		"betaProgram":         false,
		"labsProgram":         false,
		"labsExperiments":     bson.A{},
		"awareOfV2":           false,
		"samlIdentifiers":     bson.A{},
		"useOwnLLMSettings":   false,
		"llmApiKey":           "",
		"llmModelName":        "",
		"llmApiUrl":           "",
		"llmCompletionModel":  "",
		"llmApiType":          "",
		"llmModels":           bson.A{},
		"llmModelNames":       bson.A{},
		"llmCompletionModels": bson.A{},
		"llmSelectedModel":    "",
		"featuresOverrides":   bson.A{},
		"referal_id":          "ZfqyRp4nKZ9W9mNz",
		"llmProviders":        bson.A{},
		"__v":                 int32(0),
	}
	return d
}

// NewUserDoc exposes the Node-parity default user document for other
// features (P6.3b admin user create reuses the same baseline doc).
func NewUserDoc() bson.M { return newUserDoc() }
