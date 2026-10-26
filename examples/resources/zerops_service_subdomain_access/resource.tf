terraform {
  required_providers {
    zerops = { source = "zeropsio/zerops" }
  }
}

provider "zerops" {}

variable "service_id" {
  type        = string
  description = "Existing runtime service ID."
}

resource "zerops_service_deploy" "app" {
  service_id   = var.service_id
  archive_path = "${path.module}/app.tar.gz"
  zerops_yaml  = file("${path.module}/zerops.yml")
  setup        = "app"
}

# The deployment must declare an HTTP port (for example TCP/HTTP 3000).
# A service_id reference alone does not establish deployment ordering.
resource "zerops_service_subdomain_access" "app" {
  service_id = var.service_id
  depends_on = [zerops_service_deploy.app]
}

output "urls" {
  value = zerops_service_subdomain_access.app.urls
}
