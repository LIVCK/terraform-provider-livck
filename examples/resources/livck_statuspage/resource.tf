resource "livck_statuspage" "main" {
  name = "Acme Status"

  # Appearance
  primary_color      = "#0F172A"
  secondary_color    = "#22C55E"
  custom_css         = ".header { border-radius: 12px; }"
  imprint_url        = "https://acme.example/imprint"
  privacy_policy_url = "https://acme.example/privacy"

  # Access control. The password is write-only and never read back.
  access_type = "password"
  password    = var.statuspage_password

  subscriber_channels = ["email", "webhook"]

  # Assets are uploaded from disk rather than fetched from a URL, so this works
  # with a private repo. Change the file and the next apply re-uploads it.
  logo    = "${path.module}/assets/logo.png"
  favicon = "${path.module}/assets/favicon.png"
}

output "logo_url" { value = livck_statuspage.main.logo_url }
