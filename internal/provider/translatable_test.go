package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func mustMap(t *testing.T, m map[string]string) types.Map {
	t.Helper()
	v, d := types.MapValueFrom(context.Background(), types.StringType, m)
	if d.HasError() {
		t.Fatalf("building map: %v", d)
	}
	return v
}

// translatableInput picks the map when set, the plain string otherwise, nil when neither.
func TestTranslatableInput(t *testing.T) {
	ctx := context.Background()

	t.Run("map wins over plain", func(t *testing.T) {
		var d diag.Diagnostics
		got := translatableInput(ctx, types.StringValue("ignored"), mustMap(t, map[string]string{"de": "Hallo", "en": "Hi"}), &d)
		m, ok := got.(map[string]string)
		if !ok || m["de"] != "Hallo" || m["en"] != "Hi" {
			t.Fatalf("expected the locale map, got %#v", got)
		}
	})

	t.Run("plain string when map null", func(t *testing.T) {
		var d diag.Diagnostics
		got := translatableInput(ctx, types.StringValue("Plain"), types.MapNull(types.StringType), &d)
		if got != "Plain" {
			t.Fatalf("expected plain string, got %#v", got)
		}
	})

	t.Run("nil when both unset (omitempty on the wire)", func(t *testing.T) {
		var d diag.Diagnostics
		got := translatableInput(ctx, types.StringNull(), types.MapNull(types.StringType), &d)
		if got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})
}

// translatableFromAPI must mirror the shape the practitioner wrote so both
// representations round-trip without a perpetual plan diff.
func TestTranslatableFromAPI(t *testing.T) {
	ctx := context.Background()
	remote := map[string]string{"de": "Hallo", "en": "Hi"}

	t.Run("map-writer reads the full map, plain stays null", func(t *testing.T) {
		var d diag.Diagnostics
		priorMap := mustMap(t, map[string]string{"de": "x"}) // prior wrote a map
		plain, m := translatableFromAPI(ctx, "Hallo", remote, priorMap, &d)
		if !plain.IsNull() {
			t.Fatalf("plain must stay null for a map-writer, got %q", plain.ValueString())
		}
		var out map[string]string
		m.ElementsAs(ctx, &out, false)
		if out["en"] != "Hi" {
			t.Fatalf("expected full translations echoed, got %#v", out)
		}
	})

	t.Run("string-writer keeps the resolved string, map stays null", func(t *testing.T) {
		var d diag.Diagnostics
		plain, m := translatableFromAPI(ctx, "Hallo", remote, types.MapNull(types.StringType), &d)
		if plain.ValueString() != "Hallo" {
			t.Fatalf("expected resolved string, got %q", plain.ValueString())
		}
		if !m.IsNull() {
			t.Fatalf("map must stay null for a string-writer")
		}
	})
}

func TestColorPtrAndTagKeyPattern(t *testing.T) {
	if colorPtr(types.StringNull()) != nil || colorPtr(types.StringUnknown()) != nil {
		t.Fatal("colorPtr must be nil for null/unknown")
	}
	if p := colorPtr(types.StringValue("#abcdef")); p == nil || *p != "#abcdef" {
		t.Fatalf("colorPtr should pass through a concrete value")
	}

	// The provider key rule must mirror TagInput::KEY_PATTERN exactly, or a
	// config that passes here 422s late server-side.
	accept := []string{"env", "critical", "my.team_web-1", "a"}
	reject := []string{"Env", "team/web", "env-", "env_", "env.", "-env", "", "env:prod"}
	for _, k := range accept {
		if !tagKeyPattern.MatchString(k) {
			t.Errorf("expected %q to be accepted", k)
		}
	}
	for _, k := range reject {
		if tagKeyPattern.MatchString(k) {
			t.Errorf("expected %q to be rejected", k)
		}
	}
}
