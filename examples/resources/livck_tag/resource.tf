# A key/value tag and a bare label.
resource "livck_tag" "env_prod" {
  key   = "env"
  value = "prod"
  color = "#16a34a"
}

resource "livck_tag" "critical" {
  key = "critical"
}

# Tag services declaratively — the set REPLACES the assignment on every apply.
resource "livck_service" "api" {
  name       = "API"
  check_type = "http"
  target     = "https://api.example.com/health"

  tags = [livck_tag.env_prod.id, livck_tag.critical.id]
}

# A tag-synced statuspage group: every service tagged env:prod materializes
# as a machine-managed child automatically — do not declare those children.
resource "livck_statuspage_component" "production" {
  statuspage_id = livck_statuspage.main.id
  name          = "Production"
  is_group      = true
  sync_tag_id   = livck_tag.env_prod.id
}
