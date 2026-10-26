resource "zerops_service_env" "example" {
  service_id = zerops_service.app.id
  key        = "APP_MODE"
  content    = "production"
  sensitive  = false
}
