package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// serverDefaultInt64 keeps an omitted, server-computed scalar stable across
// plans without tripping Terraform's post-apply consistency check.
//
// The stock UseStateForUnknown modifier is not enough for these attributes,
// because they live inside an Optional (non-Computed) settings object. When
// that object first appears in config, the framework null-fills the computed
// children the user did not mention. UseStateForUnknown then does nothing (the
// planned value is null, not unknown), and the value the API returns for the
// child (a timeout, a retry count) clashes with the null the plan promised,
// which Terraform rejects as an inconsistent result after apply.
//
// Deciding the plan from config and prior state avoids that:
//
//   - value set in config         -> keep it
//   - omitted, prior value known  -> reuse it (steady-state plans stay clean)
//   - omitted, no prior value yet  -> unknown (the API fills it on apply)
//
// When the whole settings block is absent from config the modifier stays out of
// the way, so an unconfigured service does not sprout a phantom settings object.
type serverDefaultInt64Modifier struct{}

func serverDefaultInt64() planmodifier.Int64 {
	return serverDefaultInt64Modifier{}
}

func (m serverDefaultInt64Modifier) Description(_ context.Context) string {
	return "Reuses the prior value when omitted, or defers to the API the first time settings are configured."
}

func (m serverDefaultInt64Modifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m serverDefaultInt64Modifier) PlanModifyInt64(ctx context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	// The practitioner gave an explicit value: keep it.
	if !req.ConfigValue.IsNull() {
		return
	}

	// The settings block is absent entirely: leave the attribute null so an
	// unconfigured service stays unconfigured.
	var parent types.Object
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, req.Path.ParentPath(), &parent)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if parent.IsNull() {
		return
	}

	// Settings are configured but this child was omitted: reuse a known prior
	// value, otherwise defer to the value the API computes on apply.
	if !req.StateValue.IsNull() && !req.StateValue.IsUnknown() {
		resp.PlanValue = req.StateValue
		return
	}
	resp.PlanValue = types.Int64Unknown()
}
