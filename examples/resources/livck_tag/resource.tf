# A key/value tag and a bare label.
resource "livck_tag" "env_prod" {
  key   = "env"
  value = "prod"
  color = "#16a34a"
}

resource "livck_tag" "critical" {
  key = "critical"
}

# Setting tags hands the assignment to Terraform: the list replaces whatever is
# attached on every apply.
resource "livck_service" "api" {
  name       = "API"
  check_type = "http"
  target     = "https://api.example.com/health"

  tags = [livck_tag.env_prod.id, livck_tag.critical.id]
}

# A tag-synced group. Every service carrying env:prod shows up as a child on
# its own, so do not declare those children here.
resource "livck_statuspage_component" "production" {
  statuspage_id = livck_statuspage.main.id
  name          = "Production"
  is_group      = true
  sync_tag_id   = livck_tag.env_prod.id
}
