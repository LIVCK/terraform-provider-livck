package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testAccProtoV6ProviderFactories instantiates the provider for acceptance
// tests (protocol v6).
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"livck": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck skips unless the environment points at a live instance.
// Run against the local dev stack:
//
//	TF_ACC=1 LIVCK_ENDPOINT=http://localhost:8000/api LIVCK_API_TOKEN=lvk_... make testacc
func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("LIVCK_API_TOKEN") == "" {
		t.Skip("LIVCK_API_TOKEN not set, acceptance tests need a live instance and an org token")
	}
}

func TestAccServiceResource_basic(t *testing.T) {
	name := "tfacc-service-basic"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "livck_service" "test" {
  name       = %q
  check_type = "http"
  target     = "https://example.com"

  settings = {
    interval_seconds = 60
    config = jsonencode({
      method = "GET"
      conditions = [
        { field = "status_code", operator = "gte", value = 400, status = "down" }
      ]
    })
  }
}`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("livck_service.test", "id"),
					resource.TestCheckResourceAttr("livck_service.test", "name", name),
					resource.TestCheckResourceAttr("livck_service.test", "check_type", "http"),
					resource.TestCheckResourceAttr("livck_service.test", "paused", "false"),
				),
			},
			{
				// Read-after-write: a follow-up plan must be empty.
				RefreshState:       true,
				ExpectNonEmptyPlan: false,
			},
			{
				ResourceName:      "livck_service.test",
				ImportState:       true,
				ImportStateVerify: true,
				// settings, like tags, is only tracked once it is declared: an
				// import leaves it null and the first plan writes the block back.
				ImportStateVerifyIgnore: []string{"settings"},
			},
		},
	})
}

// A service must survive settings being added, removed and added again. Each of
// those transitions used to fail the post-apply consistency check: the computed
// scalars (timeout_seconds, retries) planned as null when the settings block
// first appeared, and removing the block left its server-side values echoing
// back into a state the plan said was null.
func TestAccServiceResource_settingsLifecycle(t *testing.T) {
	name := "tfacc-settings-lifecycle"

	bare := fmt.Sprintf(`
resource "livck_service" "test" {
  name       = %q
  check_type = "http"
  target     = "https://example.com"
}`, name)

	configured := fmt.Sprintf(`
resource "livck_service" "test" {
  name       = %q
  check_type = "http"
  target     = "https://example.com"

  settings = {
    interval_seconds = 60
    assigned_probes  = ["ffm"]
  }
}`, name)

	emptyFollowUpPlan := resource.TestStep{RefreshState: true, ExpectNonEmptyPlan: false}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Created unconfigured: no settings block, and a second plan stays empty.
			{Config: bare, Check: resource.TestCheckNoResourceAttr("livck_service.test", "settings")},
			emptyFollowUpPlan,
			// Settings added later: the API fills timeout/retries without a crash.
			{Config: configured, Check: resource.TestCheckResourceAttr("livck_service.test", "settings.interval_seconds", "60")},
			emptyFollowUpPlan,
			// Settings removed: back to unconfigured, no leftover echo.
			{Config: bare, Check: resource.TestCheckNoResourceAttr("livck_service.test", "settings")},
			emptyFollowUpPlan,
			// Settings re-added on top of a previously-null state.
			{Config: configured, Check: resource.TestCheckResourceAttr("livck_service.test", "settings.interval_seconds", "60")},
			emptyFollowUpPlan,
		},
	})
}

func TestAccStatuspageWithComponents_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "livck_statuspage" "test" {
  name = "tfacc-statuspage"
}

resource "livck_statuspage_component" "group" {
  statuspage_id = livck_statuspage.test.id
  name          = "tfacc-group"
  is_group      = true
}

resource "livck_statuspage_component" "child" {
  statuspage_id = livck_statuspage.test.id
  name          = "tfacc-child"
  parent_id     = livck_statuspage_component.group.id
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("livck_statuspage.test", "slug"),
					resource.TestCheckResourceAttr("livck_statuspage_component.group", "is_group", "true"),
					resource.TestCheckResourceAttrPair(
						"livck_statuspage_component.child", "parent_id",
						"livck_statuspage_component.group", "id",
					),
				),
			},
			{
				RefreshState:       true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}
