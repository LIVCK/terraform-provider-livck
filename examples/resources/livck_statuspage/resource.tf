resource "livck_statuspage" "main" {
  name = "Acme Status"

  # Appearance
  primary_color   = "#0F172A"
  secondary_color = "#22C55E"
  custom_css      = ".header { border-radius: 12px; }"
  imprint_url     = "https://acme.example/imprint"
  privacy_policy_url = "https://acme.example/privacy"

  # Access control (password is write-only — never read back)
  access_type = "password"
  password    = var.statuspage_password

  subscriber_channels = ["email", "webhook"]

  # Binary assets uploaded from local files (private repos → uploaded, not pulled).
  # Change the file's content to trigger a re-upload automatically.
  logo    = "${path.module}/assets/logo.png"
  favicon = "${path.module}/assets/favicon.png"
}

output "logo_url" { value = livck_statuspage.main.logo_url }
