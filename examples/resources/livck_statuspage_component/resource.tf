resource "livck_statuspage_component" "core" {
  statuspage_id = livck_statuspage.main.id
  name          = "Core Systems"
  is_group      = true
}

resource "livck_statuspage_component" "website" {
  statuspage_id = livck_statuspage.main.id
  name          = "Website"
  parent_id     = livck_statuspage_component.core.id
  service_id    = livck_service.website.id
}
