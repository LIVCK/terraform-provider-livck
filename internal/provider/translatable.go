package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Translatable fields exist twice in a schema: a plain string attribute
// (single-language, the common case) and a `<field>_translations` map for
// {locale: value} content. Exactly one of the two is configured; the API
// accepts both shapes on the same JSON key.

// translatableInput returns the API payload for a translatable pair: the
// {locale: value} map when the map attribute is configured, the plain string
// otherwise, nil when neither is set (omitted server-side via omitempty).
func translatableInput(ctx context.Context, plain types.String, translations types.Map, diags *diag.Diagnostics) any {
	if !translations.IsNull() && !translations.IsUnknown() {
		m := map[string]string{}
		diags.Append(translations.ElementsAs(ctx, &m, false)...)
		return m
	}

	if !plain.IsNull() && !plain.IsUnknown() {
		return plain.ValueString()
	}

	return nil
}

// translatableFromAPI maps a translatable field back into state, mirroring how
// the practitioner wrote it: map-writers read the full *_translations payload
// (the plain attribute stays null), string-writers keep the locale-resolved
// string (the map stays null). Without the prior-shape pivot, one of the two
// representations would always drift.
func translatableFromAPI(ctx context.Context, resolved string, translations map[string]string, priorMap types.Map, diags *diag.Diagnostics) (types.String, types.Map) {
	if !priorMap.IsNull() && !priorMap.IsUnknown() {
		m, d := types.MapValueFrom(ctx, types.StringType, translations)
		diags.Append(d...)

		return types.StringNull(), m
	}

	return types.StringValue(resolved), types.MapNull(types.StringType)
}
