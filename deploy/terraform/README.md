# Terraform: GCP provisioning for Prism

This provisions the substrate Prism runs on: a VPC-native GKE cluster with a managed,
autoscaling node pool, and (optionally) Cloud SQL Postgres and Memorystore Redis on
private IPs. ClickHouse, Redpanda, and Jaeger are deployed inside the cluster by the
Helm chart, since GCP has no first-party managed equivalent; for those, use the
chart's in-cluster infrastructure, a vendor cloud, or an operator.

## Cost and credentials warning

Applying this creates billable resources (a GKE node pool, and by default a Cloud SQL
instance and a Memorystore instance). Destroy them when you are done. You need
`gcloud` authenticated with rights to create these resources, and the target project
set.

## What it creates

- A VPC (`prism-vpc`), a subnet with secondary ranges for pods and services, and a
  private services access range for the managed data services.
- A zonal GKE cluster (`prism-gke`) with Workload Identity and a separately managed,
  autoscaling node pool.
- Optionally, Cloud SQL Postgres 16 (`prism-pg`) with a `prism` database and user and
  a generated password, on a private IP.
- Optionally, Memorystore Redis 7 (`prism-redis`) on a private IP.

## Usage

```bash
cd deploy/terraform
cp terraform.tfvars.example terraform.tfvars   # then edit project_id, region, zone
gcloud auth application-default login

terraform init
terraform plan
terraform apply
```

`terraform init`, `validate`, and `plan` are the real correctness gate. State defaults
to local; for anything shared, copy `backend.tf.example` to `backend.tf` and point it
at a GCS bucket.

## Wiring the outputs into the Helm chart

After apply, connect kubectl and read the outputs the chart needs:

```bash
eval "$(terraform output -raw kubeconfig_command)"

DATABASE_URL=$(terraform output -raw database_url)
REDIS_ADDR=$(terraform output -raw redis_addr)
```

Then install the chart against the managed services, leaving devInfra off for the
data services that are now managed. ClickHouse, Redpanda, and Jaeger still come from
the chart in this example:

```bash
helm upgrade --install prism ../helm/prism -n prism --create-namespace \
  --set image.registry=ghcr.io/talif787 --set image.tag=0.8.0 \
  --set-string secrets.databaseUrl="$DATABASE_URL" \
  --set endpoints.redis="$REDIS_ADDR" \
  --set secrets.internalApiToken="$INTERNAL_TOKEN" \
  --set secrets.authDevSecret="$DEV_SIGNING_SECRET"
```

## Teardown

```bash
terraform destroy
```

Cloud SQL and the network peering can take several minutes to delete. If a destroy
stalls on the private services connection, ensure no instances still reference it.

## Notes and deferrals

- The cluster is zonal to keep cost down. For availability, set the cluster and node
  pool `location` to `var.region`.
- Deletion protection is disabled on the cluster and Cloud SQL so a portfolio stack
  tears down cleanly. Enable it for anything real.
- Secrets here (the generated database password) flow through Terraform state and
  outputs. For production, source them from Secret Manager rather than state.
- A private GKE control plane, Cloud NAT for egress, and tighter firewall rules are
  natural hardening follow-ups.
