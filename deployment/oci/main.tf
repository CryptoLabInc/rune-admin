# Terraform Deployment for OCI

terraform {
  required_version = ">= 1.0"
  required_providers {
    oci = {
      source  = "oracle/oci"
      version = "~> 5.0"
    }
  }
}

provider "oci" {
  region              = var.region
  config_file_profile = var.oci_profile
}

variable "oci_profile" {
  description = "OCI CLI config profile name (~/.oci/config)"
  type        = string
  default     = "DEFAULT"
}

variable "region" {
  description = "OCI region"
  type        = string
  default     = "us-ashburn-1"
}

variable "compartment_id" {
  description = "OCI compartment OCID"
  type        = string
}

variable "team_name" {
  description = "Team name for Vault instance"
  type        = string
}

variable "tls_mode" {
  description = "TLS mode: self-signed, custom, or none"
  type        = string
  default     = "self-signed"
}

variable "envector_endpoint" {
  description = "enVector Cloud endpoint"
  type        = string
}

variable "envector_api_key" {
  description = "enVector Cloud API key"
  type        = string
  sensitive   = true
}

variable "runevault_version" {
  description = "Pinned runevault release tag — drives the install.sh URL and binary version on the VM."
  type        = string
}

variable "public_key" {
  description = "SSH public key content for instance access"
  type        = string
  default     = ""
}

# VCN for Vault
resource "oci_core_vcn" "vault_vcn" {
  compartment_id = var.compartment_id
  display_name   = "vault-${var.team_name}-vcn"
  cidr_block     = "10.0.0.0/16"
  dns_label      = "vaultvcn"
}

# Public subnet
resource "oci_core_subnet" "vault_subnet" {
  compartment_id    = var.compartment_id
  vcn_id            = oci_core_vcn.vault_vcn.id
  display_name      = "vault-${var.team_name}-subnet"
  cidr_block        = "10.0.1.0/24"
  dns_label         = "vaultsub"
  security_list_ids = [oci_core_security_list.vault_security_list.id]
  route_table_id    = oci_core_route_table.vault_route_table.id
}

# Internet Gateway
resource "oci_core_internet_gateway" "vault_ig" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.vault_vcn.id
  display_name   = "vault-${var.team_name}-ig"
}

# Route Table
resource "oci_core_route_table" "vault_route_table" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.vault_vcn.id
  display_name   = "vault-${var.team_name}-rt"

  route_rules {
    destination       = "0.0.0.0/0"
    network_entity_id = oci_core_internet_gateway.vault_ig.id
  }
}

# Security List
resource "oci_core_security_list" "vault_security_list" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.vault_vcn.id
  display_name   = "vault-${var.team_name}-sl"

  egress_security_rules {
    destination = "0.0.0.0/0"
    protocol    = "all"
  }

  # gRPC
  ingress_security_rules {
    protocol = "6" # TCP
    source   = "0.0.0.0/0"

    tcp_options {
      min = 50051
      max = 50051
    }
  }

  # SSH
  ingress_security_rules {
    protocol = "6" # TCP
    source   = "0.0.0.0/0"

    tcp_options {
      min = 22
      max = 22
    }
  }
}

# Compute Instance for Vault
resource "oci_core_instance" "vault_instance" {
  compartment_id      = var.compartment_id
  availability_domain = data.oci_identity_availability_domains.ads.availability_domains[0].name
  display_name        = "vault-${var.team_name}"
  shape               = "VM.Standard.E5.Flex"

  shape_config {
    ocpus         = 1
    memory_in_gbs = 4
  }

  create_vnic_details {
    subnet_id        = oci_core_subnet.vault_subnet.id
    display_name     = "vault-${var.team_name}-vnic"
    assign_public_ip = true
  }

  source_details {
    source_type = "image"
    source_id   = data.oci_core_images.ubuntu_image.images[0].id
  }

  metadata = {
    ssh_authorized_keys = var.public_key
    user_data = base64encode(templatefile("${path.module}/startup-script.sh", {
      team_name          = var.team_name
      envector_endpoint  = var.envector_endpoint
      envector_api_key   = var.envector_api_key
      runevault_version  = var.runevault_version
    }))
  }
}

# Data sources
data "oci_identity_availability_domains" "ads" {
  compartment_id = var.compartment_id
}

data "oci_core_images" "ubuntu_image" {
  compartment_id           = var.compartment_id
  operating_system         = "Canonical Ubuntu"
  operating_system_version = "24.04"
  shape                    = "VM.Standard.E5.Flex"
  sort_by                  = "TIMECREATED"
  sort_order               = "DESC"

  filter {
    name   = "display_name"
    values = ["^Canonical-Ubuntu-24.04-.*"]
    regex  = true
  }
}

# Outputs
output "vault_url" {
  description = "Rune-Vault gRPC endpoint"
  value       = "${oci_core_instance.vault_instance.public_ip}:50051"
}

output "vault_public_ip" {
  value       = oci_core_instance.vault_instance.public_ip
  description = "Public IP of Vault instance"
}

output "ssh_command" {
  value       = "ssh ubuntu@${oci_core_instance.vault_instance.public_ip}"
  description = "SSH command to connect to Vault instance"
}
