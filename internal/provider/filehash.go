package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// fileHashModifier stamps a Computed hash attribute from the content of a local
// file named by a sibling path attribute. This is what makes a binary asset
// (logo/favicon) diff correctly: Terraform compares attribute VALUES, and a raw
// file path never changes when the file's CONTENT does. By re-hashing the file
// on every plan, a changed image produces a changed hash → a diff → a re-upload,
// with zero boilerplate for the practitioner (no manual `filesha256(...)`).
type fileHashModifier struct {
	pathAttr string
}

func fileHashFrom(pathAttr string) planmodifier.String {
	return fileHashModifier{pathAttr: pathAttr}
}

func (m fileHashModifier) Description(_ context.Context) string {
	return fmt.Sprintf("Sets the value to the sha256 of the file at %q (drives content-change detection).", m.pathAttr)
}

func (m fileHashModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m fileHashModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	var filePath types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root(m.pathAttr), &filePath)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// No file configured → no hash (the asset is unmanaged / to be removed).
	if filePath.IsNull() {
		resp.PlanValue = types.StringNull()
		return
	}

	// Path still unknown at plan (e.g. interpolated from another unknown) → leave
	// unknown; the value settles at apply.
	if filePath.IsUnknown() {
		resp.PlanValue = types.StringUnknown()
		return
	}

	hash, err := hashFile(filePath.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root(m.pathAttr),
			"Cannot read the asset file",
			fmt.Sprintf("Reading %q to compute its content hash failed: %s", filePath.ValueString(), err),
		)
		return
	}

	resp.PlanValue = types.StringValue(hash)
}

func hashFile(p string) (string, error) {
	content, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

// urlFollowsHashModifier keeps a Computed served-URL stable across plans UNLESS
// the local asset file's content is changing — a re-uploaded asset gets a
// brand-new URL server-side, so a plain UseStateForUnknown would promise "no
// change" and then trip Terraform's post-apply consistency check.
//
// CRITICAL: it must NOT read the sibling *_hash attribute from req.Plan. During
// SchemaModifyPlan the plan is a snapshot and attribute modifiers do not observe
// each other's just-computed values, so the sibling hash there is still the OLD
// (state) value — comparing it to state always says "unchanged", the URL is kept,
// and the apply then re-uploads to a new URL → "inconsistent result after apply".
// Instead we hash the LOCAL FILE directly (the same source fileHashModifier uses)
// and compare to the state's stored hash. Self-contained, order-independent.
type urlFollowsHashModifier struct {
	pathAttr string
	hashAttr string
}

func urlFollowsHash(pathAttr, hashAttr string) planmodifier.String {
	return urlFollowsHashModifier{pathAttr: pathAttr, hashAttr: hashAttr}
}

func (m urlFollowsHashModifier) Description(_ context.Context) string {
	return fmt.Sprintf("Marks the URL unknown when the file at %q changes; keeps the prior value otherwise.", m.pathAttr)
}

func (m urlFollowsHashModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m urlFollowsHashModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Create (no prior state): leave as the framework's unknown.
	if req.State.Raw.IsNull() {
		return
	}
	// Destroy: nothing to plan.
	if req.Plan.Raw.IsNull() {
		return
	}

	var filePath types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root(m.pathAttr), &filePath)...)
	var stateHash types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root(m.hashAttr), &stateHash)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// No file configured now: if there was one before (state hash set), the asset
	// is being removed and the URL will change → unknown; otherwise nothing.
	if filePath.IsNull() {
		if stateHash.IsNull() {
			resp.PlanValue = req.StateValue
		} else {
			resp.PlanValue = types.StringUnknown()
		}
		return
	}
	// Path interpolated from something still unknown → settle at apply.
	if filePath.IsUnknown() {
		resp.PlanValue = types.StringUnknown()
		return
	}

	hash, err := hashFile(filePath.ValueString())
	if err != nil {
		// Let fileHashModifier surface the read error; keep the URL unknown.
		resp.PlanValue = types.StringUnknown()
		return
	}

	// Unchanged content → keep the stable URL; changed → re-upload → unknown.
	if stateHash.ValueString() == hash {
		resp.PlanValue = req.StateValue
		return
	}
	resp.PlanValue = types.StringUnknown()
}
