/**
 * Copyright 2021 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

terraform {
  required_providers {
    google = {
      source = "hashicorp/google"
    }
  }
}

resource "google_service_account" "autoneg" {
  project    = var.project_id
  account_id = var.service_account_id
}

resource "google_compute_subnetwork_iam_member" "shared_vpc_user" {
  count      = var.shared_vpc != null ? 1 : 0
  project    = var.shared_vpc.project_id
  region     = var.shared_vpc.subnetwork_region
  subnetwork = var.shared_vpc.subnetwork_id
  role       = "roles/compute.networkUser"
  member     = "serviceAccount:${google_service_account.autoneg.email}"
}

locals {
  zonal_permissions = [
    "compute.backendServices.get",
    "compute.backendServices.update",
    "compute.networkEndpointGroups.use",
    "compute.healthChecks.useReadOnly",
  ]
  permissions = var.regional ? concat(
    local.zonal_permissions, [
      "compute.regionBackendServices.get",
      "compute.regionBackendServices.update",
      "compute.regionHealthChecks.useReadOnly",
    ],
  ) : local.zonal_permissions
}

resource "google_project_iam_custom_role" "autoneg" {
  project     = var.project_id
  role_id     = var.regional ? "autonegRegional" : "autonegZonal"
  title       = "${var.regional ? "Regional" : "Zonal"} AutoNEG role"
  description = "Minimum viable IAM custom role to allow AutoNEG to watch and associate NEGs created by the NEG controller to backend services"
  permissions = local.permissions
}

resource "google_project_iam_member" "autoneg" {
  project = var.project_id
  role    = google_project_iam_custom_role.autoneg.id
  member  = "serviceAccount:${google_service_account.autoneg.email}"
}

resource "google_service_account_iam_member" "workload_identity" {
  count              = var.workload_identity != null ? 1 : 0
  service_account_id = google_service_account.autoneg.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[${var.workload_identity.namespace}/${var.workload_identity.service_account}]"
}