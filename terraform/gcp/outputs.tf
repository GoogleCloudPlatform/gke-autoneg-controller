output "autoneg_custom_role" {
  value = google_project_iam_custom_role.autoneg
}

output "service_account" {
  value = var.workload_identity != null ? google_service_account.autoneg[0] : null
}

output "service_account_email" {
  value = var.workload_identity != null ? google_service_account.autoneg[0].email : null
}
