// Package data, file oauth2.go --- ports the com.google.api.client.auth.
// oauth2.Credential seam of Java's Optional<Credential>.
//
// In Java the Bridge and SnapshotApi carry an Optional<Credential> (the user's
// GitHub-style bearer token) that the Oauth2Filter validated and that the
// snapshot API request adds as `Authorization: Bearer <token>` via an
// interceptor. The Go port threads the bearer token explicitly.
package data

// Oauth2 carries the validated bearer token (Java Credential + its access
// token). A nil *Oauth2 is Java's Optional.empty().
type Oauth2 struct {
	// Token is the bearer token sent as `Authorization: Bearer <Token>`.
	Token string
}
