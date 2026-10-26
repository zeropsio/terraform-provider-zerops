# Only forwarding type is readable. Configuration and certificate remain in sensitive state.
resource "zerops_project_remote_logging" "example" {
  project_id    = zerops_project.example.id
  syslog_config = "destination d_discard { file(\"/dev/null\"); }; log { source(s_src); destination(d_discard); };"
  certificate   = ""
  type          = "syslog"
}
