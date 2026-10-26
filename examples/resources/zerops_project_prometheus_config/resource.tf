resource "zerops_project_prometheus_config" "example" {
  project_id = zerops_project.example.id
  # This singleton replaces every custom discovery group on the primary core.
  # Destroy clears discovery; it does not delete the SYSTEM user-data key.
  configs = [{
    targets = ["127.0.0.1:9100"]
    labels  = { job = "node", environment = "sandbox" }
  }]
}
