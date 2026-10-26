# This singleton owns ALL balancer overrides; omitted keys reset to defaults.
resource "zerops_project_l7_balancer_config" "example" {
  project_id = zerops_project.example.id
  values = {
    client_max_body_size = "64m"
    keepalive_timeout    = "35"
  }
}
