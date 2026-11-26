package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/livck/terraform-provider-livck/internal/client"
)

// The recurring maintenance acceptance tests need, besides LIVCK_ENDPOINT and
// LIVCK_API_TOKEN (abilities: maintenances.*, services.*, statuspages.*), an
// organization on a plan with API access and recurring maintenances. Two tests
// move the backend in ways the API does not offer, through optional commands:
//
//   - LIVCK_ACC_ADVANCE_CMD: runs the backend's minute job once (opens and
//     closes due windows). Unset, the instance's own scheduler is waited for.
//   - LIVCK_ACC_PLAN_LIMIT_PAUSE_CMD: pauses the maintenance named in
//     LIVCK_RECURRING_MAINTENANCE_ID the way a plan downgrade does. Unset, the
//     plan limit test is skipped.

const recurringResource = "livck_recurring_maintenance.test"

type recurringAccConfig struct {
	Schedule      string
	Extra         string
	TitleEN       string
	PlainTitle    string
	WithTargets   bool
	ServiceTarget bool
}

// testAccRecurringConfig renders a tag, a tagged service, a status page and
// the recurring maintenance. schedule and extra are spliced in verbatim.
func testAccRecurringConfig(c recurringAccConfig) string {
	title := c.TitleEN
	if title == "" {
		title = "Nightly backup"
	}

	titleBlock := fmt.Sprintf("  title_translations = {\n    en = %q\n    de = \"Nächtliche Sicherung\"\n  }", title)
	if c.PlainTitle != "" {
		titleBlock = fmt.Sprintf("  title = %q", c.PlainTitle)
	}

	targets := ""
	if c.WithTargets {
		targets = "  tag_ids = [livck_tag.test.id]\n"
		if c.ServiceTarget {
			targets = "  tag_ids        = []\n  service_ids    = [livck_service.test.id]\n  statuspage_ids = [livck_statuspage.test.id]\n"
		}
	}

	return fmt.Sprintf(`
resource "livck_tag" "test" {
  key   = "tfacc-backup"
  value = "nightly"
}

resource "livck_service" "test" {
  name       = "tfacc-recurring-db"
  check_type = "http"
  target     = "https://example.com"
  tags       = [livck_tag.test.id]
}

resource "livck_statuspage" "test" {
  name = "tfacc-recurring"
}

resource "livck_recurring_maintenance" "test" {
%s
  timezone = "Europe/Berlin"

%s
%s%s}
`, titleBlock, c.Schedule, targets, c.Extra)
}

const weeklySchedule = `  schedule {
    frequency        = "weekly"
    weekdays         = [5, 1, 3]
    anchor_date      = "2026-01-01"
    start_time       = "02:00"
    duration_minutes = 60
  }`

const monthlySchedule = `  schedule {
    frequency        = "monthly"
    anchor_date      = "2026-01-01"
    start_time       = "03:30"
    duration_minutes = 90
    until_date       = "2030-12-31"

    month {
      mode    = "nth_weekday"
      nth     = "last"
      weekday = 0
    }
  }`

const dailyEveryOtherSchedule = `  schedule {
    frequency        = "daily"
    interval         = 2
    anchor_date      = "2026-01-01"
    start_time       = "23:45"
    duration_minutes = 30
  }`

func expectEmptyPlanAfterApply() resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{
		PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
	}
}

func TestAccRecurringMaintenance_lifecycle(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRecurringMaintenanceDestroyed,
		Steps: []resource.TestStep{
			// Created with a tag as target; the schedule comes back as written,
			// weekdays as a set in another order than configured.
			{
				Config: testAccRecurringConfig(recurringAccConfig{Schedule: weeklySchedule, WithTargets: true}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(recurringResource, "id"),
					resource.TestCheckResourceAttr(recurringResource, "schedule.frequency", "weekly"),
					resource.TestCheckResourceAttr(recurringResource, "schedule.interval", "1"),
					resource.TestCheckResourceAttr(recurringResource, "schedule.weekdays.#", "3"),
					resource.TestCheckTypeSetElemAttr(recurringResource, "schedule.weekdays.*", "5"),
					resource.TestCheckResourceAttr(recurringResource, "tag_ids.#", "1"),
					resource.TestCheckResourceAttrPair(recurringResource, "tag_ids.0", "livck_tag.test", "id"),
					resource.TestCheckResourceAttr(recurringResource, "service_ids.#", "0"),
					resource.TestCheckResourceAttr(recurringResource, "notify_start", "false"),
					resource.TestCheckResourceAttr(recurringResource, "notify_end", "false"),
					resource.TestCheckResourceAttr(recurringResource, "paused", "false"),
					resource.TestCheckNoResourceAttr(recurringResource, "paused_reason"),
					resource.TestCheckResourceAttr(recurringResource, "state", "idle"),
					resource.TestCheckResourceAttrSet(recurringResource, "next_start_at"),
					resource.TestCheckResourceAttrSet(recurringResource, "next_end_at"),
					resource.TestCheckResourceAttrSet(recurringResource, "rule_text"),
					resource.TestCheckNoResourceAttr(recurringResource, "description_translations"),
				),
				ConfigPlanChecks: expectEmptyPlanAfterApply(),
			},
			// Rule, targets and switches changed in one go.
			{
				Config: testAccRecurringConfig(recurringAccConfig{
					Schedule:      monthlySchedule,
					WithTargets:   true,
					ServiceTarget: true,
					Extra: `
  description_translations = { en = "Database dump and offsite copy" }
  notify_start             = true
  notify_end               = true
`,
				}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(recurringResource, "schedule.frequency", "monthly"),
					resource.TestCheckResourceAttr(recurringResource, "schedule.month.mode", "nth_weekday"),
					resource.TestCheckResourceAttr(recurringResource, "schedule.month.nth", "last"),
					resource.TestCheckResourceAttr(recurringResource, "schedule.month.weekday", "0"),
					resource.TestCheckResourceAttr(recurringResource, "schedule.until_date", "2030-12-31"),
					resource.TestCheckNoResourceAttr(recurringResource, "schedule.weekdays"),
					resource.TestCheckResourceAttr(recurringResource, "tag_ids.#", "0"),
					resource.TestCheckResourceAttrPair(recurringResource, "service_ids.0", "livck_service.test", "id"),
					resource.TestCheckResourceAttrPair(recurringResource, "statuspage_ids.0", "livck_statuspage.test", "id"),
					resource.TestCheckResourceAttr(recurringResource, "description_translations.en", "Database dump and offsite copy"),
					resource.TestCheckResourceAttr(recurringResource, "notify_start", "true"),
					resource.TestCheckResourceAttr(recurringResource, "notify_end", "true"),
				),
				ConfigPlanChecks: expectEmptyPlanAfterApply(),
			},
			// Paused by hand: no next window.
			{
				Config: testAccRecurringConfig(recurringAccConfig{
					Schedule: monthlySchedule, WithTargets: true, ServiceTarget: true,
					Extra: "\n  description_translations = { en = \"Database dump and offsite copy\" }\n  notify_start = true\n  notify_end = true\n  paused = true\n",
				}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(recurringResource, "paused", "true"),
					resource.TestCheckResourceAttr(recurringResource, "paused_reason", "manual"),
					resource.TestCheckResourceAttr(recurringResource, "state", "paused"),
					resource.TestCheckNoResourceAttr(recurringResource, "next_start_at"),
				),
				ConfigPlanChecks: expectEmptyPlanAfterApply(),
			},
			// And resumed.
			{
				Config: testAccRecurringConfig(recurringAccConfig{
					Schedule: monthlySchedule, WithTargets: true, ServiceTarget: true,
					Extra: "\n  description_translations = { en = \"Database dump and offsite copy\" }\n  notify_start = true\n  notify_end = true\n  paused = false\n",
				}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(recurringResource, "paused", "false"),
					resource.TestCheckNoResourceAttr(recurringResource, "paused_reason"),
					resource.TestCheckResourceAttr(recurringResource, "state", "idle"),
					resource.TestCheckResourceAttrSet(recurringResource, "next_start_at"),
				),
				ConfigPlanChecks: expectEmptyPlanAfterApply(),
			},
			{
				ResourceName:      recurringResource,
				ImportState:       true,
				ImportStateVerify: true,
				// Computed from the clock and the calendar; they may move between
				// the apply and the import.
				ImportStateVerifyIgnore: []string{"next_start_at", "next_end_at", "state"},
			},
			// Description removed from the configuration: it is removed on the server.
			// Leaving the target sets out stops managing them; they stay as they are.
			{
				Config: testAccRecurringConfig(recurringAccConfig{Schedule: dailyEveryOtherSchedule, TitleEN: "Index rebuild"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(recurringResource, "schedule.frequency", "daily"),
					resource.TestCheckResourceAttr(recurringResource, "schedule.interval", "2"),
					resource.TestCheckNoResourceAttr(recurringResource, "schedule.month.mode"),
					resource.TestCheckResourceAttr(recurringResource, "title_translations.en", "Index rebuild"),
					resource.TestCheckNoResourceAttr(recurringResource, "description_translations"),
					resource.TestCheckResourceAttr(recurringResource, "notify_start", "false"),
					resource.TestCheckResourceAttrPair(recurringResource, "service_ids.0", "livck_service.test", "id"),
				),
				ConfigPlanChecks: expectEmptyPlanAfterApply(),
			},
			// From the translations to a single-language title: it reads back as
			// the string, the map is gone, and nothing drifts.
			{
				Config: testAccRecurringConfig(recurringAccConfig{Schedule: dailyEveryOtherSchedule, PlainTitle: "Index rebuild"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(recurringResource, "title", "Index rebuild"),
					resource.TestCheckNoResourceAttr(recurringResource, "title_translations"),
				),
				ConfigPlanChecks: expectEmptyPlanAfterApply(),
			},
			// And back to the translations.
			{
				Config: testAccRecurringConfig(recurringAccConfig{Schedule: dailyEveryOtherSchedule, TitleEN: "Index rebuild"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(recurringResource, "title"),
					resource.TestCheckResourceAttr(recurringResource, "title_translations.de", "Nächtliche Sicherung"),
				),
				ConfigPlanChecks: expectEmptyPlanAfterApply(),
			},
		},
	})
}

// A single-language title from the start, imported afterwards: the import
// reads it back as the string, so the configuration and the state agree.
func TestAccRecurringMaintenance_plainTitle(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRecurringMaintenanceDestroyed,
		Steps: []resource.TestStep{
			// Both shapes at once are refused at plan time. First, so the test
			// ends on a valid configuration that the destroy can use.
			{
				Config: testAccRecurringConfig(recurringAccConfig{
					Schedule: weeklySchedule, PlainTitle: "Patch day",
					Extra: "  title_translations = { en = \"Patch day\" }\n",
				}),
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			{
				Config: testAccRecurringConfig(recurringAccConfig{Schedule: weeklySchedule, PlainTitle: "Patch day"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(recurringResource, "title", "Patch day"),
					resource.TestCheckNoResourceAttr(recurringResource, "title_translations"),
				),
				ConfigPlanChecks: expectEmptyPlanAfterApply(),
			},
			// An import reads the map (it has no prior shape); the title itself is
			// the one that was written, in the organization's default language.
			{
				ResourceName:            recurringResource,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"next_start_at", "next_end_at", "state", "title", "title_translations"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected one imported state, got %d", len(states))
					}
					attrs := states[0].Attributes
					if attrs["title_translations.%"] != "1" || attrs["title"] != "" {
						return fmt.Errorf("import must read the title as a one-language map: %v", attrs)
					}
					for k, v := range attrs {
						if strings.HasPrefix(k, "title_translations.") && k != "title_translations.%" && v != "Patch day" {
							return fmt.Errorf("imported title %s = %q, want Patch day", k, v)
						}
					}
					return nil
				},
			},
		},
	})
}

// A pause caused by the plan limit is not a pause by hand: `paused` stays as
// configured, nothing is planned, and Terraform cannot lift it.
func TestAccRecurringMaintenance_planLimitPauseNoDiff(t *testing.T) {
	pauseCmd := os.Getenv("LIVCK_ACC_PLAN_LIMIT_PAUSE_CMD")
	if pauseCmd == "" {
		t.Skip("LIVCK_ACC_PLAN_LIMIT_PAUSE_CMD is not set")
	}

	var id string
	config := func(extra string) string {
		return testAccRecurringConfig(recurringAccConfig{Schedule: weeklySchedule, Extra: extra})
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRecurringMaintenanceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: config(""),
				Check:  captureID(recurringResource, &id),
			},
			{
				PreConfig: func() { runAccHook(t, pauseCmd, id) },
				Config:    config(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(recurringResource, "paused", "false"),
					resource.TestCheckResourceAttr(recurringResource, "paused_reason", "plan_limit"),
					resource.TestCheckResourceAttr(recurringResource, "state", "paused"),
				),
			},
			// paused = true on top of it is accepted and stays stable.
			{
				Config: config("  paused = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(recurringResource, "paused", "true"),
					resource.TestCheckResourceAttr(recurringResource, "paused_reason", "plan_limit"),
				),
				ConfigPlanChecks: expectEmptyPlanAfterApply(),
			},
			// paused = false does not lift the plan limit pause, and does not drift.
			{
				Config: config("  paused = false\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(recurringResource, "paused", "false"),
					resource.TestCheckResourceAttr(recurringResource, "paused_reason", "plan_limit"),
					resource.TestCheckResourceAttr(recurringResource, "state", "paused"),
				),
				ConfigPlanChecks: expectEmptyPlanAfterApply(),
			},
		},
	})
}

// A window that opened and closed moves state, next_start_at and next_end_at.
// None of them may show up in a plan.
func TestAccRecurringMaintenance_noDiffAfterWindow(t *testing.T) {
	// The first window starts one to two minutes from now and lasts five.
	start := time.Now().UTC().Truncate(time.Minute).Add(2 * time.Minute)
	config := testAccRecurringConfig(recurringAccConfig{
		Schedule: fmt.Sprintf(`  schedule {
    frequency        = "daily"
    anchor_date      = %q
    start_time       = %q
    duration_minutes = 5
  }`, start.Format("2006-01-02"), start.Format("15:04")),
		Extra: "",
	})
	// UTC keeps the arithmetic above simple.
	config = strings.Replace(config, `timezone = "Europe/Berlin"`, `timezone = "UTC"`, 1)

	var id string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckRecurringMaintenanceDestroyed,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					captureID(recurringResource, &id),
					resource.TestCheckResourceAttr(recurringResource, "state", "idle"),
				),
			},
			// The window runs.
			{
				PreConfig: func() { waitForRecurringState(t, id, start, "running") },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.TestCheckResourceAttr(recurringResource, "state", "running"),
			},
			// The window is over (ended early through the API); the next one is tomorrow.
			{
				PreConfig: func() {
					endRecurringWindow(t, id)
					waitForRecurringState(t, id, start, "idle")
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(recurringResource, "state", "idle"),
					resource.TestCheckResourceAttr(recurringResource, "next_start_at", start.Add(24*time.Hour).Format("2006-01-02T15:04:05+00:00")),
				),
			},
		},
	})
}

func testAccClient() *client.Client {
	return client.New(os.Getenv("LIVCK_ENDPOINT"), os.Getenv("LIVCK_API_TOKEN"))
}

func testAccCheckRecurringMaintenanceDestroyed(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "livck_recurring_maintenance" {
			continue
		}
		_, err := testAccClient().GetRecurringMaintenance(context.Background(), rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("recurring maintenance %s still exists", rs.Primary.ID)
		}
		if !errors.Is(err, client.ErrNotFound) {
			return err
		}
	}
	return nil
}

func captureID(name string, into *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("%s not in state", name)
		}
		*into = rs.Primary.ID
		return nil
	}
}

// runAccHook runs an optional backend command with the maintenance id in its
// environment.
func runAccHook(t *testing.T, command, id string) {
	t.Helper()
	if command == "" {
		return
	}

	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), "LIVCK_RECURRING_MAINTENANCE_ID="+id)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("command %q failed: %v\n%s", command, err, out)
	}
}

// waitForRecurringState waits until the window has started and the
// maintenance reads the wanted state, running LIVCK_ACC_ADVANCE_CMD on every
// round when it is set.
func waitForRecurringState(t *testing.T, id string, start time.Time, want string) {
	t.Helper()

	if wait := time.Until(start.Add(2 * time.Second)); wait > 0 {
		time.Sleep(wait)
	}

	deadline := time.Now().Add(3 * time.Minute)
	for {
		runAccHook(t, os.Getenv("LIVCK_ACC_ADVANCE_CMD"), id)

		remote, err := testAccClient().GetRecurringMaintenance(context.Background(), id)
		if err != nil {
			t.Fatalf("reading %s: %v", id, err)
		}
		if remote.State == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("recurring maintenance %s is %q, expected %q (is the backend's scheduler running?)", id, remote.State, want)
		}
		time.Sleep(5 * time.Second)
	}
}

// endRecurringWindow ends the running window through the API, named by its
// regular start as the API requires.
func endRecurringWindow(t *testing.T, id string) {
	t.Helper()

	remote, err := testAccClient().GetRecurringMaintenance(context.Background(), id)
	if err != nil || remote.WindowStart == nil {
		t.Fatalf("reading the running window of %s: %v", id, err)
	}

	body, _ := json.Marshal(map[string]string{"window_start": *remote.WindowStart})
	endpoint := strings.TrimRight(os.Getenv("LIVCK_ENDPOINT"), "/") + "/v1/recurring-maintenances/" + id + "/end-window"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("LIVCK_API_TOKEN"))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ending the window of %s: HTTP %d", id, resp.StatusCode)
	}
}
