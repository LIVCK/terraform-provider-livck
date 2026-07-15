data "livck_probes" "all" {}

output "probe_codes" {
  value = [for p in data.livck_probes.all.probes : p.code]
}
