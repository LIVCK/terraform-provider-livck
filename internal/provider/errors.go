package provider

import (
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/livck/terraform-provider-livck/internal/client"
)

// clientFromProviderData asserts the configured API client out of the
// provider data passed to resources and data sources.
func clientFromProviderData(data any, diags *diag.Diagnostics) *client.Client {
	if data == nil {
		return nil
	}
	c, ok := data.(*client.Client)
	if !ok {
		diags.AddError("Unexpected provider data", "Expected *client.Client — this is a bug in the provider.")
		return nil
	}
	return c
}

// addAPIError converts a client error into a diagnostic with the field
// errors of a validation failure spelled out.
func addAPIError(diags *diag.Diagnostics, summary string, err error) {
	var ve *client.ValidationError
	if errors.As(err, &ve) {
		diags.AddError(summary, ve.Error())
		return
	}
	diags.AddError(summary, err.Error())
}
