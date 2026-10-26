data "zerops_projects" "example" {
  name_contains = "app"
  statuses      = ["ACTIVE"]
}
