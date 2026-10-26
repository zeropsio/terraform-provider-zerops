# End-to-end smoke configuration for the explicitly guarded selected target.
# Production uses minimum resources; dev retains its original defaults.
terraform {
  required_providers {
    zerops = {
      source = "zeropsio/zerops"
    }
  }
}

variable "zerops_endpoint" {
  type        = string
  description = "Zerops API origin; tools/smoke.sh sets it from the guarded target."
}

variable "production" {
  type        = bool
  default     = false
  description = "Use minimum production resource bounds."
}

provider "zerops" {
  endpoint = var.zerops_endpoint
}

data "zerops_organization" "current" {}

resource "zerops_project" "smoke" {
  name        = "tfacc-smoke"
  description = "terraform-provider-zerops smoke run"
  tags        = ["tfacc", "smoke"]
}

resource "zerops_project_env" "mode" {
  project_id = zerops_project.smoke.id
  key        = "APP_MODE"
  content    = "smoke"
}

resource "zerops_service" "app" {
  project_id = zerops_project.smoke.id
  hostname   = "app"
  type       = "nodejs@22"

  vertical_autoscaling = var.production ? {
    cpu_mode             = "SHARED"
    min_cpu              = 1
    max_cpu              = 1
    min_ram_gb           = 0.125
    max_ram_gb           = 0.125
    min_disk_gb          = 1
    max_disk_gb          = 1
    start_cpu_core_count = 1
  } : null

  horizontal_autoscaling = {
    min_containers = 1
    max_containers = 1
  }
}

resource "zerops_service_env" "greeting" {
  service_id = zerops_service.app.id
  key        = "GREETING"
  content    = "hello"
  sensitive  = true
}

resource "zerops_object_storage" "assets" {
  project_id = zerops_project.smoke.id
  hostname   = "assets"
  quota_gb   = var.production ? 1 : 2
}

resource "zerops_service_deploy" "app" {
  service_id = zerops_service.app.id
  source_dir = "${path.module}/app"
  setup      = "app"

  zerops_yaml = <<-YAML
    zerops:
      - setup: app
        build:
          base: nodejs@22
          buildCommands:
            - echo smoke-build
          deployFiles:
            - index.js
        run:
          base: nodejs@22
          ports:
            - port: 3000
              protocol: tcp
              httpSupport: true
          start: node index.js
  YAML

  depends_on = [zerops_service_env.greeting, zerops_project_env.mode]
}

resource "zerops_service_subdomain_access" "app" {
  service_id = zerops_service.app.id
  depends_on = [zerops_service_deploy.app]
}

resource "zerops_public_http_routing" "app" {
  project_id  = zerops_project.smoke.id
  domains     = ["tfacc-smoke.example.com"]
  ssl_enabled = true
  location = [{
    path       = "/"
    port       = 3000
    service_id = zerops_service.app.id
  }]
  depends_on = [zerops_service_deploy.app]
}

resource "zerops_public_port_routing" "app" {
  service_id         = zerops_service.app.id
  public_port        = 23000
  internal_port      = 3000
  protocol           = "tcp"
  firewall_policy    = "BLACKLIST"
  firewall_ip_ranges = []
  depends_on         = [zerops_service_deploy.app, zerops_public_http_routing.app]
}

output "project_id" {
  value = zerops_project.smoke.id
}

output "organization_id" {
  value = data.zerops_organization.current.id
}

output "subdomain_urls" {
  value = zerops_service_subdomain_access.app.urls
}
