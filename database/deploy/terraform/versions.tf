terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.60, < 7.0"
    }
  }

  # Local state is fine for a disposable benchmark stack. If several people share
  # it, move to a remote backend (see deploy/README.md, "Implementing it properly").
}

provider "aws" {
  region = var.region

  default_tags {
    tags = merge({ Project = "dbbench", ManagedBy = "terraform" }, var.tags)
  }
}
