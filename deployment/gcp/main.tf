terraform {
  required_version = ">= 1.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# Variables
variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region"
  type        = string
  default     = "asia-northeast3"
}

variable "zone" {
  description = "GCP zone (defaults to region-a)"
  type        = string
  default     = ""
}

variable "team_name" {
  description = "Team name (used for resource naming)"
  type        = string
  default     = "runeconsole"
}

locals {
  zone = var.zone != "" ? var.zone : "${var.region}-a"
}

variable "tls_mode" {
  description = "TLS mode: self-signed, custom, or none"
  type        = string
  default     = "self-signed"
}

variable "machine_type" {
  description = "Compute Engine machine type"
  type        = string
  default     = "e2-medium" # 2 vCPU, 4GB RAM
}

variable "runeconsole_version" {
  description = "Pinned runeconsole release tag — drives the install.sh URL and binary version on the VM."
  type        = string
}

variable "public_key" {
  description = "SSH public key content for instance access"
  type        = string
  default     = ""
}

# VPC Network
resource "google_compute_network" "console_network" {
  name                    = "rune-console-${var.team_name}"
  auto_create_subnetworks = false
}

# Subnet
resource "google_compute_subnetwork" "console_subnet" {
  name          = "rune-console-subnet-${var.team_name}"
  ip_cidr_range = "10.0.1.0/24"
  region        = var.region
  network       = google_compute_network.console_network.id
}

# Firewall Rules
resource "google_compute_firewall" "console_grpc" {
  name    = "rune-console-grpc-${var.team_name}"
  network = google_compute_network.console_network.name

  allow {
    protocol = "tcp"
    ports    = ["50051"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["rune-console"]
}

resource "google_compute_firewall" "console_ssh" {
  name    = "rune-console-ssh-${var.team_name}"
  network = google_compute_network.console_network.name

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["rune-console"]
}

# Static IP
resource "google_compute_address" "console_ip" {
  name   = "rune-console-ip-${var.team_name}"
  region = var.region
}

# Compute Instance
resource "google_compute_instance" "console" {
  name         = "rune-console-${var.team_name}"
  machine_type = var.machine_type
  zone         = local.zone

  tags = ["rune-console"]

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2404-lts-amd64"
      size  = 20
      type  = "pd-standard"
    }
  }

  network_interface {
    subnetwork = google_compute_subnetwork.console_subnet.name

    access_config {
      nat_ip = google_compute_address.console_ip.address
    }
  }

  metadata = {
    ssh-keys = var.public_key != "" ? "ubuntu:${var.public_key}" : ""
  }

  metadata_startup_script = templatefile("${path.module}/startup-script.sh", {
    runeconsole_version = var.runeconsole_version
  })

  service_account {
    scopes = ["cloud-platform"]
  }

  labels = {
    project = "rune"
    team    = var.team_name
  }

  lifecycle {
    ignore_changes = [metadata["ssh-keys"]]
  }
}

# Outputs
output "console_url" {
  description = "Rune-Console gRPC endpoint"
  value       = "${google_compute_address.console_ip.address}:50051"
}

output "console_public_ip" {
  description = "Public IP address"
  value       = google_compute_address.console_ip.address
}

output "console_private_ip" {
  description = "Private IP address"
  value       = google_compute_instance.console.network_interface[0].network_ip
}

output "ssh_command" {
  description = "SSH command to connect to Console instance"
  value       = "gcloud compute ssh ${google_compute_instance.console.name} --zone=${local.zone}"
}

output "instance_name" {
  description = "Compute Engine instance name"
  value       = google_compute_instance.console.name
}
