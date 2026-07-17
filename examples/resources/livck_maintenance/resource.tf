resource "livck_maintenance" "db_upgrade" {
  title           = "Database upgrade"
  scheduled_start = timeadd(plantimestamp(), "168h")
  scheduled_end   = timeadd(plantimestamp(), "170h")
  service_ids     = [livck_service.website.id]
  statuspage_ids  = [livck_statuspage.main.id]
  notify_24h      = true
}

# Omitting scheduled_end opens an open-ended window: status pages announce it as
# "until further notice" instead of counting down, and it is completed manually
# (auto_complete has no end to fire on, so it cannot be combined with this).
resource "livck_maintenance" "datacenter_migration" {
  title           = "Datacenter migration"
  scheduled_start = timeadd(plantimestamp(), "24h")
  service_ids     = [livck_service.website.id]
  statuspage_ids  = [livck_statuspage.main.id]
}
