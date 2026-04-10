terraform {
  required_providers {
    aws = { source = "hashicorp/aws" }
    google = { source = "hashicorp/google" }
  }
}

provider "aws" {
  region = var.aws_region
}

provider "google" {
  project = var.google_project
  region  = var.google_region
}