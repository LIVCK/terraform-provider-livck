data "livck_check_types" "catalog" {}

locals {
  http_interval_min = jsondecode(data.livck_check_types.catalog.catalog_json)
}
