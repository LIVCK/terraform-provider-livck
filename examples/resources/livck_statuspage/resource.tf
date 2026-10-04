resource "livck_statuspage" "main" {
  name = "Acme Status"

  # Branding
  primary_color      = "#0F172A"
  secondary_color    = "#22C55E"
  custom_css         = ".header { border-radius: 12px; }"
  imprint_url        = "https://acme.example/imprint"
  privacy_policy_url = "https://acme.example/privacy"
  logo_size          = "large"

  # Appearance: light or dark mode. "system" follows each visitor's device,
  # and the switch lets visitors pick light or dark for themselves.
  appearance              = "system"
  allow_appearance_switch = true

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
