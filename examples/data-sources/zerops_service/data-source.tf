data "zerops_service" "app" {
  project_id = zerops_project.example.id
  hostname   = "app"
}

# Alternatively, select by ID alone:
data "zerops_service" "by_id" {
  id = data.zerops_service.app.id
}
