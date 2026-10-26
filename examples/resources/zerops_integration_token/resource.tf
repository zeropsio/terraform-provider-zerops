# Use a separate pre-authorized administration credential for the provider.
# The token remains sensitive plaintext in state: protect state and backups.
resource "zerops_integration_token" "example" {
  name      = "project-reader"
  role_code = "NO_ACCESS"
  projects = [{
    project_id = zerops_project.example.id
    role_code  = "READ_ONLY"
  }]
  rotation_trigger = { generation = "1" }
}
# Changing generation rotates in place and immediately invalidates the old bearer.
# Import cannot recover a secret; change rotation_trigger after import if needed.
