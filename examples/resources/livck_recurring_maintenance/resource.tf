resource "livck_tag" "nightly_backup" {
  key   = "backup"
  value = "nightly"
}

# Every night from 02:00 to 03:00 Berlin time, for every service that carries
# the tag when the window starts. Times stay local across daylight saving.
resource "livck_recurring_maintenance" "nightly_backup" {
  title_translations = {
    en = "Nightly backup"
    de = "Nächtliche Sicherung"
  }
  timezone = "Europe/Berlin"

  schedule {
    frequency        = "daily"
    anchor_date      = "2026-12-01"
    start_time       = "02:00"
    duration_minutes = 60
  }

  tag_ids        = [livck_tag.nightly_backup.id]
  statuspage_ids = [livck_statuspage.main.id]
  notify_start   = true
}

# Patch day: the second Tuesday of every month, 22:00 to 23:30, until the end
# of 2027, for two services picked by hand.
resource "livck_recurring_maintenance" "patch_day" {
  title    = "Patch day"
  timezone = "Europe/Berlin"

  schedule {
    frequency        = "monthly"
    anchor_date      = "2026-12-01"
    start_time       = "22:00"
    duration_minutes = 90
    until_date       = "2027-12-31"

    month {
      mode    = "nth_weekday"
      nth     = 2
      weekday = 2
    }
  }

  service_ids = [livck_service.website.id, livck_service.api.id]
}
