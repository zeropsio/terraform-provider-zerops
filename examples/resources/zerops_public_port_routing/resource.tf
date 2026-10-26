# Deploy declared TCP/UDP backend ports before enabling routing.
# IP_V4 is also supported, but requires an existing dedicated project IPv4
# allocation (zerops_project_dedicated_ipv4); the shared IPv4 flag is insufficient.
resource "zerops_public_port_routing" "app" {
  service_id         = zerops_service.app.id
  public_port        = 20000
  internal_port      = 3000
  protocol           = "tcp"
  public_ip_type     = "IP_V6"
  firewall_policy    = "WHITELIST"
  firewall_ip_ranges = ["2001:db8::/32"]
  force_sync         = false
  depends_on         = [zerops_service_deploy.app]
}
