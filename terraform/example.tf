/**
 * Copyright 2022 Google LLC
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

variable "project_id" {
  type        = string
  description = "Project ID"
}

variable "cluster_name" {
  type        = string
  description = "Cluster name"
}

variable "location" {
  type        = string
  description = "Location for cluster"
}

data "google_client_config" "provider" {}

data "google_container_cluster" "gke-cluster" {
  project = var.project_id

  name     = var.cluster_name
  location = var.location
}

provider "kubernetes" {
  host  = format("https://%s", data.google_container_cluster.gke-cluster.endpoint)
  token = data.google_client_config.provider.access_token
  cluster_ca_certificate = base64decode(
    data.google_container_cluster.gke-cluster.master_auth[0].cluster_ca_certificate,
  )
}

module "autoneg" {
  source = "./autoneg/"

  project_id = var.project_id
}