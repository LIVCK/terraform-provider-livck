# Terraform Provider for LIVCK

Manage [LIVCK](https://livck.com) monitoring, status pages, maintenance windows
and custom domains from Terraform or OpenTofu. LIVCK runs in Germany and the
provider talks to the public API, so a status page and everything behind it can
live in git.

```hcl
terraform {
  required_providers {
    livck = {
      source  = "livck/livck"
      version = "~> 0.1"
    }
  }
}

provider "livck" {} # token from LIVCK_API_TOKEN

resource "livck_service" "website" {
  name       = "Website"
  check_type = "http"
  target     = "https://example.com"

  settings = {
    interval_seconds = 60
    assigned_probes  = ["ffm", "vie"]
  }
}

resource "livck_statuspage" "main" {
  name          = "Acme Status"
  slug          = "acme"
  primary_color = "#0f172a"
  logo          = "${path.module}/assets/logo.png"
}

resource "livck_statuspage_component" "website" {
  statuspage_id = livck_statuspage.main.id
  name          = "Website"
  service_id    = livck_service.website.id
}
```

A full worked example, including one that monitors the public DNS resolvers,
lives in [livck-terraform-getting-started](https://github.com/LIVCK/livck-terraform-getting-started).

## Authentication

Create an organization API token in the console under Settings > API Tokens
(Team plan or higher) and grant it the abilities your config needs.

```sh
export LIVCK_API_TOKEN="lvk_..."
```

The provider never writes the token to a file. Point `endpoint` at something
else if you are not on LIVCK Cloud.

## What you can manage

| Resources | |
|---|---|
| `livck_service` | HTTP, TCP, DNS, ICMP, SSL and manual checks |
| `livck_statuspage` | the page, its appearance, access control and assets |
| `livck_statuspage_component` | components and groups, including tag-synced ones |
| `livck_statuspage_metric` / `livck_statuspage_metric_series` | charts |
| `livck_maintenance` | scheduled windows |
| `livck_tag` | org-wide labels for services |
| `livck_custom_domain` | your own hostname on a status page |

| Data sources | |
|---|---|
| `livck_probes` | monitoring locations and their codes |
| `livck_check_types` | field and condition catalog per check type |
| `livck_statuspage` | look up a page by id |

Every resource supports `terraform import`. Reference docs are in
[`docs/`](./docs) and on the Terraform Registry.

## Things worth knowing

**Probes are codes, not ids.** `assigned_probes = ["ffm", "vie"]`. Use the
`livck_probes` data source to see what your plan allows.

**Logos are uploaded from disk.** `logo = "./assets/logo.png"` reads the file
and uploads it. Change the file and the next apply re-uploads it; you do not
have to wire up a hash yourself.

**Custom domains hand you the DNS records.** After creating one, `cname_target`,
`txt_record_name` and `txt_record_value` are computed attributes, so you can
create the records with your DNS provider in the same plan and let LIVCK's
background verification take it from there.

**Names can be multilingual.** Either `name = "Status"` or
`name_translations = { de = "Systemstatus", en = "System Status" }`, one or the
other.

**Some fields cannot be read back**, because the API deliberately never returns
them: secrets inside `settings.config`, the status page `password`, the local
path of an uploaded asset, and the maintenance notify flags. They behave as
write-only. After importing such a resource the first plan shows a one-off
change that writes your configured values back; apply it once and you are in
sync.

**Tags on a service are opt-in.** Set `tags` and Terraform owns the assignment
(the list replaces whatever is attached, an empty list clears it). Leave it out
and tagging stays whatever the console says, with no drift.

## Limitations

- Children of a tag-synced group (`sync_tag_id`) are created server-side from
  the services carrying that tag. Do not declare them in Terraform; add and
  remove them by tagging services.
- Component nesting stops at five levels. Going deeper fails with a clear error
  from the API.
- On-call schedules, escalation policies, alert routing and SLOs are not in the
  provider yet.
- Incidents are runtime events and are not managed here.

## Development

Go 1.24 or newer, and Terraform 1.5+ or OpenTofu.

```sh
make build     # compile
make test      # unit tests, no network
make generate  # rebuild docs/ from the schema and examples/

# Acceptance tests need a live instance and a token:
TF_ACC=1 LIVCK_ENDPOINT=http://localhost:8000/api LIVCK_API_TOKEN=lvk_... make testacc
```

To try a local build, add a
[dev_overrides](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides)
block to your CLI config:

```hcl
provider_installation {
  dev_overrides {
    "livck/livck" = "/path/to/go/bin"
  }
  direct {}
}
```

With `dev_overrides` in place you skip `terraform init` and run `plan`/`apply`
directly.

## Releasing

See [PUBLISHING.md](./PUBLISHING.md).
