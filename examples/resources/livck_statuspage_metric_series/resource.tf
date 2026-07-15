resource "livck_statuspage_metric_series" "website" {
  statuspage_id = livck_statuspage.main.id
  metric_id     = livck_statuspage_metric.latency.id
  name          = "Website"
  service_id    = livck_service.website.id
  metric_type   = "response_time_avg"
  color         = "#3B82F6"
}
