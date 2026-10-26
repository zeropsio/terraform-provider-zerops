data "zerops_service_env_vars" "app" {
  service_id = zerops_service.app.id
}

# The entire vars map is sensitive and is stored in Terraform state.
# Non-sudo credentials may return masks rather than usable credentials.
output "service_environment" {
  value     = data.zerops_service_env_vars.app.vars
  sensitive = true
}
