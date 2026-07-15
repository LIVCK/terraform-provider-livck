# Standalone chart on the page
resource "livck_statuspage_metric" "latency" {
  statuspage_id = livck_statuspage.main.id
  name          = "Response Times"
  display_type  = "line_chart"
  suffix        = "ms"
}

# Chart under a component
resource "livck_statuspage_metric" "uptime" {
  statuspage_id = livck_statuspage.main.id
  component_id  = livck_statuspage_component.website.id
  name          = "Website Uptime"
  display_type  = "uptime_bars"
}
