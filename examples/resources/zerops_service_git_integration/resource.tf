# Requires a prior console GitHub/GitLab connection saved by the SAME Zerops user
# as the provider bearer (per user, not per organization); otherwise the API
# returns githubAuthorizationRequired / gitlabAuthorizationRequired.
# No OAuth credential is accepted or stored. Configuration never triggers builds.
# Delete owns/removes the connector and its webhook.
resource "zerops_service_git_integration" "app" {
  service_id           = zerops_service.app.id
  git_provider         = "github"
  repository_full_name = "owner/repository"
  event_type           = "BRANCH"
  branch_name          = "main"
  is_active            = true
  zerops_yaml_setup    = "app"
}
