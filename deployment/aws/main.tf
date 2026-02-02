terraform {
  required_version = ">= 1.0"
  
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.region
}

# Variables
variable "region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
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

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "t3.medium"  # 2 vCPU, 4GB RAM
}

variable "key_name" {
  description = "EC2 key pair name (must exist in region)"
  type        = string
  default     = ""
}

# Data sources
data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"]  # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

# VPC
resource "aws_vpc" "vault_vpc" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = {
    Name    = "rune-vault-${var.team_name}"
    Project = "Rune"
    Team    = var.team_name
  }
}

# Internet Gateway
resource "aws_internet_gateway" "vault_igw" {
  vpc_id = aws_vpc.vault_vpc.id

  tags = {
    Name    = "rune-vault-igw-${var.team_name}"
    Project = "Rune"
  }
}

# Public Subnet
resource "aws_subnet" "vault_subnet" {
  vpc_id                  = aws_vpc.vault_vpc.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true

  tags = {
    Name    = "rune-vault-subnet-${var.team_name}"
    Project = "Rune"
  }
}

data "aws_availability_zones" "available" {
  state = "available"
}

# Route Table
resource "aws_route_table" "vault_rt" {
  vpc_id = aws_vpc.vault_vpc.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.vault_igw.id
  }

  tags = {
    Name    = "rune-vault-rt-${var.team_name}"
    Project = "Rune"
  }
}

resource "aws_route_table_association" "vault_rta" {
  subnet_id      = aws_subnet.vault_subnet.id
  route_table_id = aws_route_table.vault_rt.id
}

# Security Group
resource "aws_security_group" "vault_sg" {
  name        = "rune-vault-sg-${var.team_name}"
  description = "Security group for Rune-Vault"
  vpc_id      = aws_vpc.vault_vpc.id

  # HTTPS
  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTPS (public)"
  }

  # Vault MCP (fallback, if SSL not configured)
  ingress {
    from_port   = 50080
    to_port     = 50080
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Vault MCP (fallback)"
  }

  # SSH
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "SSH (restrict to your IP in production)"
  }

  # Outbound
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = {
    Name    = "rune-vault-sg-${var.team_name}"
    Project = "Rune"
  }
}

# EC2 Instance
resource "aws_instance" "vault" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.instance_type
  subnet_id              = aws_subnet.vault_subnet.id
  vpc_security_group_ids = [aws_security_group.vault_sg.id]
  key_name               = var.key_name != "" ? var.key_name : null

  user_data = templatefile("${path.module}/cloud-init.yaml", {
    vault_token = var.vault_token
    team_name   = var.team_name
  })

  root_block_device {
    volume_size           = 20
    volume_type           = "gp3"
    encrypted             = true
    delete_on_termination = true
  }

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"  # IMDSv2
    http_put_response_hop_limit = 1
  }

  tags = {
    Name    = "rune-vault-${var.team_name}"
    Project = "Rune"
    Team    = var.team_name
  }
}

# Elastic IP (optional, for stable endpoint)
resource "aws_eip" "vault_eip" {
  instance = aws_instance.vault.id
  domain   = "vpc"

  tags = {
    Name    = "rune-vault-eip-${var.team_name}"
    Project = "Rune"
  }
}

# Outputs
output "vault_url" {
  description = "Rune-Vault URL"
  value       = "https://vault-${var.team_name}.${var.region}.aws.envector.io"
}

output "vault_token" {
  description = "Rune-Vault authentication token"
  value       = var.vault_token
  sensitive   = true
}

output "vault_public_ip" {
  description = "Public IP address"
  value       = aws_eip.vault_eip.public_ip
}

output "vault_private_ip" {
  description = "Private IP address"
  value       = aws_instance.vault.private_ip
}

output "ssh_command" {
  description = "SSH command to connect to Vault instance"
  value       = var.key_name != "" ? "ssh -i ~/.ssh/${var.key_name}.pem ubuntu@${aws_eip.vault_eip.public_ip}" : "SSH key not configured"
}

output "instance_id" {
  description = "EC2 instance ID"
  value       = aws_instance.vault.id
}
