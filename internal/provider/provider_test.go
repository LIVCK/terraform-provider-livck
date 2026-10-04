package provider

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// errNoAccEndpoint stops an acceptance run that does not name its target. The
// provider falls back to the production API when LIVCK_ENDPOINT is unset, and
// these tests create and delete real resources.
var errNoAccEndpoint = errors.New("LIVCK_ENDPOINT must be set for acceptance tests. They create and " +
	"delete real resources, so they never fall back to the production API. For the local dev stack: " +
	"LIVCK_ENDPOINT=http://localhost:15800/api")

// testAccProtoV6ProviderFactories instantiates the provider for acceptance
// tests (protocol v6). It refuses to start without an explicit LIVCK_ENDPOINT,
// so a test that skips testAccPreCheck still cannot reach production.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"livck": func() (tfprotov6.ProviderServer, error) {
		if os.Getenv("LIVCK_ENDPOINT") == "" {
			return nil, errNoAccEndpoint
		}
		return providerserver.NewProtocol6WithError(New("test")())()
	},
}

// testAccPreCheck fails fast unless the environment explicitly points at a live
// instance. Run against the local dev stack:
//
//	TF_ACC=1 LIVCK_ENDPOINT=http://localhost:15800/api LIVCK_API_TOKEN=lvk_... make testacc
func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("LIVCK_ENDPOINT") == "" {
		t.Fatal(errNoAccEndpoint)
	}
	if os.Getenv("LIVCK_API_TOKEN") == "" {
		t.Fatal("LIVCK_API_TOKEN must be set for acceptance tests: an organization API token (lvk_...) " +
			"for the instance at LIVCK_ENDPOINT.")
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

// testAccStatuspageConfig renders a page with a group and a child component.
// pageAttrs is spliced into the livck_statuspage block.
func testAccStatuspageConfig(pageAttrs string) string {
	return fmt.Sprintf(`
resource "livck_statuspage" "test" {
  name = "tfacc-statuspage"
%s
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
}`, pageAttrs)
}

func TestAccStatuspageWithComponents_basic(t *testing.T) {
	const page = "livck_statuspage.test"
	emptyFollowUpPlan := resource.TestStep{RefreshState: true, ExpectNonEmptyPlan: false}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccStatuspageConfig(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(page, "slug"),
					// Not configured: the server defaults land in the state.
					resource.TestCheckResourceAttr(page, "appearance", "system"),
					resource.TestCheckResourceAttr(page, "allow_appearance_switch", "true"),
					resource.TestCheckResourceAttr(page, "logo_size", "medium"),
					resource.TestCheckResourceAttr("livck_statuspage_component.group", "is_group", "true"),
					resource.TestCheckResourceAttrPair(
						"livck_statuspage_component.child", "parent_id",
						"livck_statuspage_component.group", "id",
					),
				),
			},
			emptyFollowUpPlan,
			{
				Config: testAccStatuspageConfig(`
  appearance              = "dark"
  allow_appearance_switch = false
  logo_size               = "large"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(page, "appearance", "dark"),
					resource.TestCheckResourceAttr(page, "allow_appearance_switch", "false"),
					resource.TestCheckResourceAttr(page, "logo_size", "large"),
				),
			},
			emptyFollowUpPlan,
			{
				// Removed from the config, the values stay as they are, so
				// there is nothing to plan.
				Config:   testAccStatuspageConfig(""),
				PlanOnly: true,
			},
			{
				ResourceName:      page,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
