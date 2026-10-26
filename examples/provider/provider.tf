terraform {
  required_providers {
    zerops = {
      source = "zeropsio/zerops"
    }
  }
}

provider "zerops" {
  # Explicit origin; token and organization_id come from environment.
  endpoint = "https://api.dev.example.test"
}
