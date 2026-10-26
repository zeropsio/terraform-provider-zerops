# This allocation is billed. Destroying this resource only forgets ownership;
# the address remains allocated and billed until the owning project is deleted.
# An existing allocation is adopted without requesting another address.
resource "zerops_project_dedicated_ipv4" "example" {
  project_id = zerops_project.example.id

  timeouts {
    create = "30m"
  }
}
