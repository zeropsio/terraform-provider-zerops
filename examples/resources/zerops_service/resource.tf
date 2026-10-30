resource "zerops_service" "app" {
  project_id         = zerops_project.example.id
  hostname           = "app"
  type               = "nodejs@22"
  start_without_code = true
  desired_state      = "RUNNING"

  vertical_autoscaling = {
    min_cpu      = 1
    max_cpu      = 2
    min_ram_gb   = 0.25
    max_ram_gb   = 2
    swap_enabled = false
  }
  horizontal_autoscaling = {
    min_containers = 1
    max_containers = 2
  }

  timeouts {
    create = "30m"
    update = "20m"
    delete = "20m"
  }
}

# Deploying later starts the service. If desired_state is STOPPED, the next
# service apply stops it again; do not depend on graph ordering to stop a deploy.

# Managed services offering both single-node and highly available modes need an
# explicit mode: "postgresql:single@16" or "postgresql:ha@16". The shorthand
# "postgresql@16" is rejected during planning.
resource "zerops_service" "db" {
  project_id = zerops_project.example.id
  hostname   = "db"
  type       = "postgresql:single@16"
}
