# Terraform Provider for LIVCK

Manage [LIVCK](https://livck.com) uptime monitoring, status pages and maintenance
windows as code — GDPR-compliant monitoring, hosted in Germany.

```hcl
terraform {
  required_providers {
    livck = {
      source  = "livck/livck"
      version = "~> 0.1"
    }
  }
}

provider "livck" {} # token via LIVCK_API_TOKEN

resource "livck_service" "website" {
  name       = "Website"
  check_type = "http"
  target     = "https://example.com"
  settings = {
    interval_seconds = 60
  }
}

resource "livck_statuspage" "main" {
  name      = "Acme Status"
  slug      = "acme"
  published = true
}

resource "livck_statuspage_component" "website" {
  statuspage_id = livck_statuspage.main.id
  name          = "Website"
  service_id    = livck_service.website.id
}
```

## Authentication

Create an organization API token in the LIVCK console (**Settings → API Tokens**,
plan Team or higher) with the abilities your resources need — for the full set:
`services.*`, `statuspages.*` (incl. `statuspages.publish`), `maintenances.*`.

```sh
export LIVCK_API_TOKEN="lvk_..."
```

## Resources & Data Sources

| Type | Name |
|---|---|
| Resource | `livck_service`, `livck_statuspage`, `livck_statuspage_component`, `livck_statuspage_metric`, `livck_statuspage_metric_series`, `livck_maintenance` |
| Data Source | `livck_probes`, `livck_check_types`, `livck_statuspage` |

Full documentation lives in [`docs/`](./docs) (generated via `tfplugindocs`,
rendered on the Terraform Registry).

## Development

Requirements: Go >= 1.24, Terraform >= 1.5 (or OpenTofu).

```sh
make build     # compile
make test      # unit tests (client, no network)
make testacc   # acceptance tests against a LIVE instance:
               #   TF_ACC=1 LIVCK_ENDPOINT=http://localhost:8000/api LIVCK_API_TOKEN=lvk_... make testacc
make generate  # regenerate docs/ from schema + examples/
```

To use a locally built provider, add a
[`dev_overrides`](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides)
block to your CLI config:

```hcl
provider_installation {
  dev_overrides {
    "livck/livck" = "/path/to/go/bin"
  }
  direct {}
}
```

## Known v0 limitations

- Translatable fields (`name`, `title`, …) are managed as plain strings in the
  organization's default locale; multi-locale content stays console-managed.
- Component ordering is server-assigned (creation order); drag-reordering stays
  in the console.
- Status page access control (password / email whitelist), branding and layout
  stay console-managed.
- Secrets inside `livck_service.settings.config` (header values, auth
  credentials) are write-only; after `terraform import` they must be re-applied.
- `livck_maintenance` notify flags have no read echo: after `terraform import`
  the first apply is a harmless no-op write that pins them to the declared values.

## Releasing

See [PUBLISHING.md](./PUBLISHING.md).
