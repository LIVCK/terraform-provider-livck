resource "livck_service" "website" {
  name       = "Website"
  check_type = "http"
  target     = "https://example.com"

  settings = {
    interval_seconds = 60
    config = jsonencode({
      method = "GET"
      conditions = [
        { field = "status_code", operator = "gte", value = 400, status = "down" }
      ]
    })
  }
}
