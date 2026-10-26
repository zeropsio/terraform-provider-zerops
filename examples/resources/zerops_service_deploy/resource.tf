# Invocation inputs are immutable: changing source contents, YAML or triggers
# creates a new app version. Destroy only forgets this invocation, and import
# is unsupported because the remote API cannot recover source/YAML inputs.
resource "zerops_service_deploy" "app" {
  service_id = zerops_service.app.id
  source_dir = "${path.module}/app"
  setup      = "app"
  build      = true

  zerops_yaml = <<-YAML
    zerops:
      - setup: app
        build:
          base: nodejs@22
          buildCommands:
            - echo build
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

  timeouts {
    create = "30m"
  }
}

# Alternative sources (replace source_dir, never combine sources):
# archive_path = "${path.module}/app.tar.gz"
# git = {
#   url  = "https://github.com/zeropsio/recipe-nodejs"
#   ref  = "4a156a716a3087ea4b4f0b00814b5ea1d3f21590"
#   mode = "provider" # exact commit fetched and archived locally
# }
# For private HTTPS repositories in provider mode, supply ZEROPS_GIT_TOKEN
# separately to BOTH plan and apply processes (including saved-plan applies).
# It is read only at runtime and never stored in Terraform plan/state or sent
# to Zerops. Do not embed credentials in the URL. Existing git.auth_token wins
# when explicitly supplied, but unlike the environment token it IS stored in state.
# Server mode ignores ZEROPS_GIT_TOKEN and only supports public repositories.
# Server mode only accepts public branches, requires repository zerops.yml,
# disallows auth_token and build=false, and builds HEAD at build time rather
# than pinning the planned branch-head commit. Inline YAML must match the repo.
