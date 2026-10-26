resource "zerops_service_backup_config" "cache" {
  service_id    = zerops_service.cache.id # A backup-capable managed service, e.g. valkey:single@7.2.
  backup_period = "15 3 * * *"

  retention_policy {
    max_total_files = 10
    max_total_gib   = 2
    protected_tags  = ["daily"]
    min_daily       = 1
    max_daily       = 3
    min_weekly      = 1
    max_weekly      = 2
    min_monthly     = 1
    max_monthly     = 2
  }
}

# Removing backup_period disables scheduling. Removing retention_policy falls
# back to the platform defaults exposed as default_retention_policy.
# Destroy resets both settings; existing backups and the service are not deleted.
