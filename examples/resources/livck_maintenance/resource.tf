resource "livck_maintenance" "db_upgrade" {
  title           = "Database upgrade"
  scheduled_start = timeadd(plantimestamp(), "168h")
  scheduled_end   = timeadd(plantimestamp(), "170h")
  service_ids     = [livck_service.website.id]
  statuspage_ids  = [livck_statuspage.main.id]
  notify_24h      = true
}
