resource "zerops_project" "example" {
  name               = "example"
  description        = "Managed by Terraform"
  tags               = ["terraform"]
  mode               = "LIGHT"
  env_isolation      = "none"
  ssh_isolation      = "vpn"
  public_ipv4_shared = false

  # The API cannot read a configured cap. Removing this attribute retains it.
  max_credit_limit = "25"
}
