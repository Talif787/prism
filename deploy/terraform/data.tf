# Managed data services with private IPs. Cloud SQL Postgres backs the control plane,
# relay, alerter, and metering; Memorystore Redis backs the gateway, query, alerter,
# and metering. Both are optional. ClickHouse and Redpanda are deployed in-cluster,
# since GCP has no first-party managed equivalent.

resource "random_password" "db" {
  count   = var.enable_cloud_sql ? 1 : 0
  length  = 24
  special = false
}

resource "google_sql_database_instance" "postgres" {
  count            = var.enable_cloud_sql ? 1 : 0
  name             = "${var.name_prefix}-pg"
  database_version = "POSTGRES_16"
  region           = var.region

  settings {
    tier = var.cloud_sql_tier

    ip_configuration {
      ipv4_enabled    = false
      private_network = google_compute_network.vpc.id
    }

    backup_configuration {
      enabled = true
    }
  }

  deletion_protection = false
  depends_on          = [google_service_networking_connection.private_services]
}

resource "google_sql_database" "prism" {
  count    = var.enable_cloud_sql ? 1 : 0
  name     = "prism"
  instance = google_sql_database_instance.postgres[0].name
}

resource "google_sql_user" "prism" {
  count    = var.enable_cloud_sql ? 1 : 0
  name     = "prism"
  instance = google_sql_database_instance.postgres[0].name
  password = random_password.db[0].result
}

resource "google_redis_instance" "cache" {
  count              = var.enable_memorystore ? 1 : 0
  name               = "${var.name_prefix}-redis"
  tier               = "BASIC"
  memory_size_gb     = var.memorystore_memory_gb
  region             = var.region
  authorized_network = google_compute_network.vpc.id
  connect_mode       = "PRIVATE_SERVICE_ACCESS"
  redis_version      = "REDIS_7_2"

  depends_on = [google_service_networking_connection.private_services]
}
