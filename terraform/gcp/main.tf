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
  project = var.project_id

  account_id   = "gitlab-autoneg"
  display_name = "GitLab Autoneg service account"
}

resource "google_project_iam_custom_role" "autoneg_role" {
  project = var.project_id

  role_id     = "autoneg"
  title       = "Autoneg role"
  description = "Autoneg role"
  permissions = ["compute.backendServices.get", "compute.backendServices.update", "compute.regionBackendServices.get", "compute.regionBackendServices.update", "compute.networkEndpointGroups.use", "compute.healthChecks.useReadOnly", "compute.regionHealthChecks.useReadOnly"]
}

data "google_iam_policy" "autoneg_iam_policy" {
  binding {
    role = "roles/iam.workloadIdentityUser"

    members = [
      format("serviceAccount:%s.svc.id.goog[autoneg-system/autoneg]", var.project_id)
    ]
  }
}

resource "google_service_account_iam_policy" "autoneg_sa_iam" {
  service_account_id = google_service_account.autoneg.name
  policy_data        = data.google_iam_policy.autoneg_iam_policy.policy_data
}

resource "google_project_iam_member" "autoneg_iam" {
  project = var.project_id

  role   = google_project_iam_custom_role.autoneg_role.id
  member = format("serviceAccount:%s", google_service_account.autoneg.email)
}
