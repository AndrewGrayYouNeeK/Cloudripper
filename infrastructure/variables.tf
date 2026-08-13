variable "aws_region" {
  description = "AWS region for Cloudripper resources"
  type        = string
  default     = "us-east-1"
}

variable "google_project" {
  description = "GCP project ID"
  type        = string
}

variable "google_region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
}

variable "environment" {
  description = "Deployment environment (dev, staging, prod)"
  type        = string
  default     = "dev"
}

variable "cluster_name" {
  description = "Kubernetes cluster name for chaos experiments"
  type        = string
  default     = "cloudripper"
}
