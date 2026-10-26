# Configure external DNS before opting into wait_for_ssl. The provider never manages DNS.
# SSL acceptance fixtures use ZEROPS_ACC_DNS=1 and tools/cloudflare-dns.sh credentials.
resource "zerops_public_http_routing" "app" {
  project_id   = zerops_project.app.id
  domains      = ["app.example.com", "*.app.example.com"]
  ssl_enabled  = true
  cdn_enabled  = false
  wait_for_ssl = false
  ssl_timeout  = "20m"
  location = [{
    path = "/", port = 3000, service_id = zerops_service.app.id
    config = {
      rate_limiting = { enabled = true, rate = 10, burst = 10 }
      # basic_auth passwords, when configured, are Sensitive but remain in state.
    }
  }]
  depends_on = [zerops_service_deploy.app]
  # Null inherits provider routing_force_sync. Explicit false refuses foreign staged changes.
  force_sync = false
}
