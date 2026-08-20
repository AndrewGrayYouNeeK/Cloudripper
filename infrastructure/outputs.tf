output "aws_vpc_id" {
  description = "AWS VPC ID"
  value       = aws_vpc.cloudripper.id
}

output "aws_subnet_ids" {
  description = "AWS public subnet IDs"
  value       = aws_subnet.public[*].id
}

output "aws_asg_name" {
  description = "AWS autoscaling group name"
  value       = aws_autoscaling_group.cloudripper.name
}

output "gcp_network_id" {
  description = "GCP VPC network ID"
  value       = google_compute_network.cloudripper.id
}

output "gcp_subnet_id" {
  description = "GCP subnetwork ID"
  value       = google_compute_subnetwork.cloudripper.id
}
