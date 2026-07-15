package provider

import "regexp"

// hexColorPattern mirrors the server-side color rule (`#RRGGBB`), shared by the
// statuspage appearance colors so an invalid color fails fast at plan instead of
// as a late server 422.
var hexColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
