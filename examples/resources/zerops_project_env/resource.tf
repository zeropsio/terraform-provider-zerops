resource "zerops_project" "example" {
  name = "example"
}

resource "zerops_project_env" "example" {
  project_id = zerops_project.example.id
  key        = "APPLICATION_MODE"
  content    = "production"
  sensitive  = false
}

# Content is sensitive in Terraform state, regardless of the remote masking flag.
# Changes wait for the environment update but do not restart services.
