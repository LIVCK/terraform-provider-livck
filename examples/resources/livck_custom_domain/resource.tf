resource "livck_custom_domain" "status" {
  statuspage_id = livck_statuspage.main.id
  hostname      = "status.example.com"
}

# The record set is computed, so you can create the records with whichever DNS
# provider you use. Verification then finishes on its own in the background.
resource "cloudflare_record" "status_cname" {
  zone_id = var.cloudflare_zone_id
  name    = livck_custom_domain.status.hostname
  type    = "CNAME"
  content = livck_custom_domain.status.cname_target
}

resource "cloudflare_record" "status_verify" {
  zone_id = var.cloudflare_zone_id
  name    = livck_custom_domain.status.txt_record_name
  type    = "TXT"
  content = livck_custom_domain.status.txt_record_value
}
