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
  default     = "us-central1"
}

variable "zone" {
  description = "GCP zone"
  type        = string
  default     = "us-central1-a"
}

variable "team_name" {
  description = "Team name (used for resource naming)"
  type        = string
}

variable "vault_token" {
  description = "Vault authentication token (evt_<team>_xxx)"
  type        = string
  sensitive   = true
}

variable "machine_type" {
  description = "Compute Engine machine type"
  type        = string
  default     = "e2-medium"  # 2 vCPU, 4GB RAM
}

# VPC Network
resource "google_compute_network" "vault_network" {
  name                    = "rune-vault-${var.team_name}"
  auto_create_subnetworks = false
}

# Subnet
resource "google_compute_subnetwork" "vault_subnet" {
  name          = "rune-vault-subnet-${var.team_name}"
  ip_cidr_range = "10.0.1.0/24"
  region        = var.region
  network       = google_compute_network.vault_network.id
}

# Firewall Rules
resource "google_compute_firewall" "vault_https" {
  name    = "rune-vault-https-${var.team_name}"
  network = google_compute_network.vault_network.name

  allow {
    protocol = "tcp"
    ports    = ["443"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["rune-vault"]
}

resource "google_compute_firewall" "vault_mcp" {
  name    = "rune-vault-mcp-${var.team_name}"
  network = google_compute_network.vault_network.name

  allow {
    protocol = "tcp"
    ports    = ["50080"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["rune-vault"]
}

resource "google_compute_firewall" "vault_ssh" {
  name    = "rune-vault-ssh-${var.team_name}"
  network = google_compute_network.vault_network.name

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["rune-vault"]
}

# Static IP
resource "google_compute_address" "vault_ip" {
  name   = "rune-vault-ip-${var.team_name}"
  region = var.region
}

# Compute Instance
resource "google_compute_instance" "vault" {
  name         = "rune-vault-${var.team_name}"
  machine_type = var.machine_type
  zone         = var.zone

  tags = ["rune-vault"]

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2204-lts"
      size  = 20
      type  = "pd-standard"
    }
  }

  network_interface {
    subnetwork = google_compute_subnetwork.vault_subnet.name

    access_config {
      nat_ip = google_compute_address.vault_ip.address
    }
  }

  metadata = {
    ssh-keys  = ""  # Add your SSH public key here if needed
  }

  metadata_startup_script = templatefile("${path.module}/cloud-init.yaml", {
    vault_token = var.vault_token
    team_name   = var.team_name
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
output "vault_url" {
  description = "Rune-Vault URL"
  value       = "https://vault-${var.team_name}.${var.region}.gcp.envector.io"
}

output "vault_token" {
  description = "Rune-Vault authentication token"
  value       = var.vault_token
  sensitive   = true
}

output "vault_public_ip" {
  description = "Public IP address"
  value       = google_compute_address.vault_ip.address
}

output "vault_private_ip" {
  description = "Private IP address"
  value       = google_compute_instance.vault.network_interface[0].network_ip
}

output "ssh_command" {
  description = "SSH command to connect to Vault instance"
  value       = "gcloud compute ssh ${google_compute_instance.vault.name} --zone=${var.zone}"
}

output "instance_name" {
  description = "Compute Engine instance name"
  value       = google_compute_instance.vault.name
}

output "instance_self_link" {
  description = "Compute Engine instance self-link"
  value       = google_compute_instance.vault.self_link
}
