output "cluster_name" {
  description = "GKE cluster name."
  value       = google_container_cluster.primary.name
}

output "cluster_location" {
  description = "GKE cluster location (zone)."
  value       = google_container_cluster.primary.location
}

output "kubeconfig_command" {
  description = "Point kubectl at the new cluster."
  value       = "gcloud container clusters get-credentials ${google_container_cluster.primary.name} --zone ${var.zone} --project ${var.project_id}"
}

output "postgres_private_ip" {
  description = "Cloud SQL private IP, or null when disabled."
  value       = var.enable_cloud_sql ? google_sql_database_instance.postgres[0].private_ip_address : null
}

output "database_url" {
  description = "DATABASE_URL for the chart secret, including the generated password."
  value       = var.enable_cloud_sql ? "postgres://prism:${random_password.db[0].result}@${google_sql_database_instance.postgres[0].private_ip_address}:5432/prism?sslmode=require" : null
  sensitive   = true
}

output "redis_addr" {
  description = "REDIS_ADDR for the chart config, or null when disabled."
  value       = var.enable_memorystore ? "${google_redis_instance.cache[0].host}:6379" : null
}
