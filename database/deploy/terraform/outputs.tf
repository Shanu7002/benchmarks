output "artifacts_bucket" {
  description = "Upload the dbbench binary and dbbench-run.sh here."
  value       = aws_s3_bucket.artifacts.bucket
}

output "runner_instance_id" {
  value = aws_instance.runner.id
}

output "ssm_session_command" {
  description = "Open a shell on the runner (no SSH, no open ports)."
  value       = "aws ssm start-session --region ${var.region} --target ${aws_instance.runner.id}"
}

output "postgres_endpoint" {
  value = var.enable_postgres ? aws_db_instance.postgres[0].address : null
}

output "mysql_endpoint" {
  value = var.enable_mysql ? aws_db_instance.mysql[0].address : null
}

output "db_secret_arns" {
  description = "Secrets Manager secrets holding the generated DB passwords."
  value       = local.secret_arns
}
