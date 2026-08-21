# A zonal, VPC-native GKE cluster with Workload Identity and a separately managed,
# autoscaling node pool. Zonal keeps cost down for a portfolio deployment; switch
# location to var.region for a regional (higher availability) cluster.

resource "google_container_cluster" "primary" {
  name     = "${var.name_prefix}-gke"
  location = var.zone

  network    = google_compute_network.vpc.id
  subnetwork = google_compute_subnetwork.subnet.id

  remove_default_node_pool = true
  initial_node_count       = 1

  networking_mode = "VPC_NATIVE"
  ip_allocation_policy {
    cluster_secondary_range_name  = "pods"
    services_secondary_range_name = "services"
  }

  workload_identity_config {
    workload_pool = "${var.project_id}.svc.id.goog"
  }

  release_channel {
    channel = "REGULAR"
  }

  deletion_protection = false

  depends_on = [google_project_service.enabled]
}

resource "google_container_node_pool" "primary" {
  name     = "${var.name_prefix}-pool"
  location = var.zone
  cluster  = google_container_cluster.primary.name

  initial_node_count = var.gke_node_count

  autoscaling {
    min_node_count = var.gke_min_nodes
    max_node_count = var.gke_max_nodes
  }

  node_config {
    machine_type = var.gke_node_machine_type
    disk_size_gb = 50
    oauth_scopes = ["https://www.googleapis.com/auth/cloud-platform"]

    workload_metadata_config {
      mode = "GKE_METADATA"
    }
  }

  management {
    auto_repair  = true
    auto_upgrade = true
  }
}
