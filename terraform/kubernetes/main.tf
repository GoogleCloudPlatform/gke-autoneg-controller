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
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.9.0"
    }
  }
}

resource "kubernetes_namespace" "namespace_autoneg_system" {
  metadata {
    labels = {
      app           = "autoneg"
      control-plane = "controller-manager"
    }

    name = var.workload_identity != null ? var.workload_identity.namespace : var.namespace
  }
}

resource "kubernetes_service_account" "service_account" {
  metadata {
    namespace = kubernetes_namespace.namespace_autoneg_system.metadata[0].name
    name      = var.workload_identity != null ? var.workload_identity.service_account : var.service_account_id
    labels = {
      app = "autoneg"
    }

    annotations = var.workload_identity != null ? {
      "iam.gke.io/gcp-service-account" = var.service_account_email
    } : {}
  }
}

resource "kubernetes_role" "role_autoneg_leader_election_role" {
  metadata {
    namespace = kubernetes_namespace.namespace_autoneg_system.metadata[0].name
    name      = "autoneg-leader-election-role"
    labels = {
      app = "autoneg"
    }
    annotations = {}
  }

  rule {
    api_groups = [""]
    resources  = ["configmaps"]
    verbs      = ["get", "list", "watch", "create", "update", "patch", "delete"]
  }

  rule {
    api_groups = ["coordination.k8s.io"]
    resources  = ["leases"]
    verbs      = ["get", "list", "watch", "create", "update", "patch", "delete"]
  }

  rule {
    api_groups = [""]
    resources  = ["events"]
    verbs      = ["create"]
  }
}

resource "kubernetes_cluster_role" "clusterrole_autoneg_manager_role" {
  metadata {
    name = "autoneg-manager-role"

    labels = {
      app = "autoneg"
    }
  }

  rule {
    api_groups = [""]
    resources  = ["events"]
    verbs      = ["create", "patch"]
  }

  rule {
    api_groups = [""]
    resources  = ["services"]
    verbs      = ["get", "list", "patch", "update", "watch"]
  }

  rule {
    api_groups = [""]
    resources  = ["services/finalizers"]
    verbs      = ["update"]
  }

  rule {
    api_groups = [""]
    resources  = ["services/status"]
    verbs      = ["get", "patch", "update"]
  }
}

resource "kubernetes_cluster_role" "clusterrole_autoneg_metrics_reader" {
  metadata {
    name = "autoneg-metrics-reader"

    labels = {
      app = "autoneg"
    }
  }

  rule {
    non_resource_urls = ["/metrics"]
    verbs             = ["get"]
  }
}

resource "kubernetes_cluster_role" "clusterrole_autoneg_proxy_role" {
  metadata {
    name = "autoneg-proxy-role"

    labels = {
      app = "autoneg"
    }
    annotations = {}
  }

  rule {
    api_groups = ["authentication.k8s.io"]
    resources  = ["tokenreviews"]
    verbs      = ["create"]
  }

  rule {
    api_groups = ["authorization.k8s.io"]
    resources  = ["subjectaccessreviews"]
    verbs      = ["create"]
  }
}

resource "kubernetes_role_binding" "rolebinding_autoneg_leader_election_rolebinding" {
  metadata {
    namespace = kubernetes_namespace.namespace_autoneg_system.metadata[0].name
    name      = "autoneg-leader-election-rolebinding"

    labels = {
      app = "autoneg"
    }
    annotations = {}
  }

  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "Role"
    name      = kubernetes_role.role_autoneg_leader_election_role.metadata[0].name
  }
  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account.service_account.metadata[0].name
    namespace = kubernetes_namespace.namespace_autoneg_system.metadata[0].name
  }
}

resource "kubernetes_cluster_role_binding" "clusterrolebinding_autoneg_manager_rolebinding" {
  metadata {
    name = "autoneg-manager-rolebinding"
    labels = {
      app = "autoneg"
    }
    annotations = {}
  }
  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "ClusterRole"
    name      = kubernetes_cluster_role.clusterrole_autoneg_manager_role.metadata[0].name
  }
  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account.service_account.metadata[0].name
    namespace = kubernetes_namespace.namespace_autoneg_system.metadata[0].name
  }
}

resource "kubernetes_cluster_role_binding" "clusterrolebinding_autoneg_proxy_rolebinding" {
  metadata {
    name = "autoneg-proxy-rolebinding"
    labels = {
      app = "autoneg"
    }
    annotations = {}
  }
  role_ref {
    api_group = "rbac.authorization.k8s.io"
    kind      = "ClusterRole"
    name      = kubernetes_cluster_role.clusterrole_autoneg_proxy_role.metadata[0].name
  }
  subject {
    kind      = "ServiceAccount"
    name      = kubernetes_service_account.service_account.metadata[0].name
    namespace = kubernetes_namespace.namespace_autoneg_system.metadata[0].name
  }
}

resource "kubernetes_service" "service_autoneg_controller_manager_metrics_service" {
  metadata {
    annotations = {
      "prometheus.io/port"   = "8443"
      "prometheus.io/scheme" = "https"
      "prometheus.io/scrape" = "true"
      "cloud.google.com/neg" = "{}"
    }
    labels = {
      "app"           = "autoneg"
      "control-plane" = "controller-manager"
    }
    name      = "autoneg-controller-manager-metrics-service"
    namespace = kubernetes_namespace.namespace_autoneg_system.metadata[0].name
  }
  spec {
    type = "ClusterIP"
    port {
      name        = "https"
      port        = 8443
      target_port = "https"
      protocol    = "TCP"
    }
    selector = {
      app           = "autoneg"
      control-plane = "controller-manager"
    }
  }
  lifecycle {
    ignore_changes = [
      metadata[0].annotations["cloud.google.com/neg"],
      metadata[0].annotations["cloud.google.com/neg-status"],
    ]
  }
}

resource "kubernetes_deployment" "deployment_autoneg_controller_manager" {
  metadata {
    namespace = kubernetes_namespace.namespace_autoneg_system.metadata[0].name
    name      = "autoneg-controller-manager"
    labels = {
      "app"           = "autoneg"
      "control-plane" = "controller-manager"
    }
    annotations = {}
  }

  spec {
    replicas = 1
    selector {
      match_labels = {
        app           = "autoneg"
        control-plane = "controller-manager"
      }
    }

    template {
      metadata {
        labels = {
          app           = "autoneg"
          control-plane = "controller-manager"
        }
        annotations = {}
      }

      spec {
        service_account_name             = kubernetes_service_account.service_account.metadata[0].name
        automount_service_account_token  = true
        termination_grace_period_seconds = 10
        priority_class_name              = var.priority_class_name
        node_selector = {
          "iam.gke.io/gke-metadata-server-enabled" = "true"
        }

        security_context {
          run_as_non_root = true
        }

        container {
          name = "manager"

          image             = var.controller_image
          image_pull_policy = var.image_pull_policy

          args    = ["--health-probe-bind-address=:8081", "--metrics-bind-address=127.0.0.1:8080", "--leader-elect", "--zap-encoder=json"]
          command = ["/manager"]

          security_context {
            allow_privilege_escalation = false
            privileged                 = false
            capabilities {
              drop = ["ALL"]
            }
            read_only_root_filesystem = true
            run_as_non_root           = true
            run_as_user               = 65532
            run_as_group              = 65532
            seccomp_profile {
              type = "RuntimeDefault"
            }
          }

          liveness_probe {
            http_get {
              path = "/healthz"
              port = 8081
            }
            initial_delay_seconds = 15
            period_seconds        = 20
          }

          readiness_probe {
            http_get {
              path = "/readyz"
              port = 8081
            }
            initial_delay_seconds = 5
            period_seconds        = 10
          }

          resources {
            limits = {
              cpu    = "100m"
              memory = "30Mi"
            }
            requests = {
              cpu    = "100m"
              memory = "20Mi"
            }
          }
        }

        container {
          name = "kube-rbac-proxy"

          image             = var.kube_rbac_proxy_image
          image_pull_policy = var.image_pull_policy

          args = ["--secure-listen-address=0.0.0.0:8443", "--upstream=http://127.0.0.1:8080/", "--logtostderr=true", "--v=10"]

          security_context {
            allow_privilege_escalation = false
            privileged                 = false
            capabilities {
              drop = ["ALL"]
            }
            read_only_root_filesystem = true
            run_as_non_root           = true
            run_as_user               = 65532
            run_as_group              = 65532
            seccomp_profile {
              type = "RuntimeDefault"
            }
          }

          port {
            container_port = 8443
            name           = "https"
            protocol       = "TCP"
          }
        }
      }
    }
  }
  depends_on = [
    kubernetes_role_binding.rolebinding_autoneg_leader_election_rolebinding,
    kubernetes_cluster_role_binding.clusterrolebinding_autoneg_manager_rolebinding,
    kubernetes_cluster_role_binding.clusterrolebinding_autoneg_proxy_rolebinding,
  ]
}
