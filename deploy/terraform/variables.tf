variable "project_id" {
  description = "GCP project id to deploy into."
  type        = string
}

variable "region" {
  description = "GCP region for regional resources."
  type        = string
  default     = "us-central1"
}

variable "zone" {
  description = "GCP zone for the zonal GKE cluster and node pool."
  type        = string
  default     = "us-central1-a"
}

variable "name_prefix" {
  description = "Prefix applied to resource names."
  type        = string
  default     = "prism"
}

variable "gke_node_machine_type" {
  description = "Machine type for GKE nodes."
  type        = string
  default     = "e2-standard-2"
}

variable "gke_node_count" {
  description = "Initial number of nodes in the GKE node pool."
  type        = number
  default     = 3
}

variable "gke_min_nodes" {
  description = "Node pool autoscaling minimum."
  type        = number
  default     = 2
}

variable "gke_max_nodes" {
  description = "Node pool autoscaling maximum."
  type        = number
  default     = 5
}

variable "enable_cloud_sql" {
  description = "Provision a Cloud SQL Postgres instance for the control plane, relay, alerter, and metering."
  type        = bool
  default     = true
}

variable "cloud_sql_tier" {
  description = "Cloud SQL machine tier."
  type        = string
  default     = "db-custom-1-3840"
}

variable "enable_memorystore" {
  description = "Provision a Memorystore Redis instance for the gateway, query, alerter, and metering."
  type        = bool
  default     = true
}

variable "memorystore_memory_gb" {
  description = "Memorystore Redis capacity in GB."
  type        = number
  default     = 1
}
