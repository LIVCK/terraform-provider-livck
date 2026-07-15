resource "livck_custom_domain" "status" {
  statuspage_id = livck_statuspage.main.id
  hostname      = "status.example.com"
}

# Wire the computed record set into ANY DNS provider — verification then
# completes automatically in the background; no second apply needed.
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
