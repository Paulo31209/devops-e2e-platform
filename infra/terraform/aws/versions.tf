terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.59"
    }
  }

  # Remote state. Create the bucket first, then uncomment and run
  # `terraform init -migrate-state`. S3 native locking needs Terraform >= 1.10.
  # backend "s3" {
  #   bucket       = "<your-state-bucket>"
  #   key          = "devops-e2e-platform/terraform.tfstate"
  #   region       = "us-east-1"
  #   encrypt      = true
  #   use_lockfile = true
  # }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project     = var.name
      Environment = var.environment
      ManagedBy   = "terraform"
    }
  }
}
