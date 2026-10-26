# This resource owns the WHOLE project, including any user-added services/data.
# Changing YAML destroys/recreates the project. Do not co-manage its resources.
# YAML is sensitive and stored verbatim in Terraform state; secure your state.
# Refresh detects project existence/service IDs only, not recipe-content drift.
resource "zerops_project_import" "example" {
  yaml = <<-YAML
    project:
      name: imported-example
      corePackage: LIGHT
    services:
      - hostname: cache
        type: valkey:single@7.2
  YAML
}
