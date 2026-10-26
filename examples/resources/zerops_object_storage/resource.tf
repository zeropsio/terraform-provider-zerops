# Native website JSON round-trips through the API; arbitrary AWS policy fields do not.
# Destroying this resource destroys the bucket data.
resource "zerops_object_storage" "example" {
  project_id = zerops_project.example.id
  hostname   = "assets"
  quota_gb   = 10
  policy     = "custom"
  # CDN enable is asynchronous and may fail in some regions. A failure retains
  # the bucket identity and reports the actual remote flag, never false success.
  cdn_enabled = false
  raw_policy = jsonencode({
    websiteAccess = true
    indexDocument = "index.html"
    errorDocument = "404.html"
  })
}
