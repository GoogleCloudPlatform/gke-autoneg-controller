# Testing suite for real GKE clusters

This directory contains a test setup for two GKE Autopilot clusters.

- Provisions project (optionally, can reuse project)
- Provisions VPC, subnetworks
- Provisions XLB
- Provisions GKE clusters
- Installs Autoneg on clusters
- Installs test workload on clusters

A Python test suite is available with [test.py](test.py), which reads the output from
Terraform and performs some tests.
