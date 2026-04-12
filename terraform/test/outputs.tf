output "project_id" {
  description = "Google Cloud project ID"
  value       = module.project.project_id
}

output "primary_cluster" {
  description = "Primary cluster DNS endpoint"
  value       = format("https://%s", module.cluster-primary[""].dns_endpoint)
}

output "primary_cluster_name" {
  description = "Primary cluster name"
  value       = module.cluster-primary[""].name
}

output "primary_cluster_credentials" {
  description = "Command to get credentials for primary cluster"
  value       = format("gcloud container clusters get-credentials %s --project=%s --location=%s --dns-endpoint", module.cluster-primary[""].name, module.project.project_id, var.region)
}

output "secondary_cluster" {
  description = "Secondary cluster DNS endpoint"
  value       = var.secondary_region != null ? format("https://%s", module.cluster-secondary[""].dns_endpoint) : null
}

output "secondary_cluster_name" {
  description = "Secondcary cluster name"
  value       = var.secondary_region != null ? module.cluster-secondary[""].name : null
}

output "secondary_cluster_credentials" {
  description = "Command to get credentials for primary cluster"
  value       = var.secondary_region != null ? format("gcloud container clusters get-credentials %s --project=%s --location=%s --dns-endpoint", module.cluster-secondary[""].name, module.project.project_id, var.secondary_region) : null
}

output "primary_service_namespace" {
  description = "Namespace for primary service"
  value       = resource.kubernetes_service_v1.hello-workload-primary[""].metadata[0].namespace
}

output "primary_service_name" {
  description = "Name for primary service"
  value       = resource.kubernetes_service_v1.hello-workload-primary[""].metadata[0].name
}

output "secondary_service_namespace" {
  description = "Namespace for secondary service"
  value       = var.secondary_region != null ? resource.kubernetes_service_v1.hello-workload-secondary[""].metadata[0].namespace : null
}

output "secondary_service_name" {
  description = "Name for secondary service"
  value       = var.secondary_region != null ? resource.kubernetes_service_v1.hello-workload-secondary[""].metadata[0].name : null
}

output "ilb_primary_backend_name" {
  description = "Primary ILB backend service name"
  value       = module.ilb-primary[""].backend_service_ids[keys(module.ilb-primary[""].backend_service_ids)[0]]
}

output "ilb_secondary_backend_name" {
  description = "Primary ILB backend service name"
  value       = var.secondary_region != null ? module.ilb-secondary[""].backend_service_ids[keys(module.ilb-secondary[""].backend_service_ids)[0]] : null

}

output "xlb_backend_name" {
  description = "XLB backend service name"
  value       = var.create_xlb == true ? module.xlb[""].backend_service_ids[keys(module.xlb[""].backend_service_ids)[0]] : null
}

output "xlb_url" {
  description = "XLB URL for testing"
  value       = var.create_xlb == true ? format("http://%s/", module.xlb[""].address[""]) : null
}

output "service_port" {
  description = "Service port"
  value       = 80
}