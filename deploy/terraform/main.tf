# Enables the Google APIs the rest of the configuration depends on. disable_on_destroy
# is false so tearing down this stack does not disable APIs other stacks may use.
locals {
  apis = [
    "compute.googleapis.com",
    "container.googleapis.com",
    "sqladmin.googleapis.com",
    "redis.googleapis.com",
    "servicenetworking.googleapis.com",
  ]

  needs_private_services = var.enable_cloud_sql || var.enable_memorystore
}

resource "google_project_service" "enabled" {
  for_each           = toset(local.apis)
  project            = var.project_id
  service            = each.value
  disable_on_destroy = false
}
