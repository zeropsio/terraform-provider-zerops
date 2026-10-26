# Supply only a public WireGuard key generated outside Terraform. No private key enters state.
resource "zerops_project_vpn_peer" "example" {
  project_id = zerops_project.example.id
  public_key = var.wireguard_public_key
}
