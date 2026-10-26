resource "zerops_service_restart" "app" {
  service_id = zerops_service.app.id
  triggers = {
    # Reference the completed env resource to order its write before restart.
    # Use a nonsecret version, never a password or secret env value.
    env_version = zerops_service_env.version.content
  }
}

# A trigger change restarts once; an unchanged apply performs no action.
# Restart starts a STOPPED service. Do not combine with desired_state = "STOPPED"
# unless you intend the resulting lifecycle drift. READY_TO_DEPLOY is rejected.
# Destroy is state-only and leaves the service running. Import is unsupported.
