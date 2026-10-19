variable "region" {
  description = "AWS region. DBs and runner live in the same one so network latency doesn't dominate."
  type        = string
  default     = "us-east-1"
}

variable "name_prefix" {
  description = "Prefix for resource names (lowercase letters, digits, hyphens)."
  type        = string
  default     = "dbbench"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,20}$", var.name_prefix))
    error_message = "name_prefix must be 2-21 chars: lowercase letters, digits, hyphens, starting with a letter."
  }
}

variable "enable_postgres" {
  description = "Create an RDS PostgreSQL instance."
  type        = bool
  default     = true
}

variable "enable_mysql" {
  description = "Create an RDS MySQL instance."
  type        = bool
  default     = true
}

variable "postgres_version" {
  type    = string
  default = "16"
}

variable "mysql_version" {
  type    = string
  default = "8.0"
}

variable "db_instance_class" {
  description = "Same class for every DB so results are comparable. Keep it small."
  type        = string
  default     = "db.t4g.micro"
}

variable "db_storage_gb" {
  description = "Fixed storage per DB. Autoscaling is deliberately NOT enabled: a runaway test must hit a wall."
  type        = number
  default     = 20

  validation {
    condition     = var.db_storage_gb >= 20 && var.db_storage_gb <= 100
    error_message = "db_storage_gb must be between 20 and 100. Raise the cap here only with a reason."
  }
}

variable "runner_instance_type" {
  description = "EC2 type for the machine that runs dbbench (arm64, to match the binary you build)."
  type        = string
  default     = "t4g.small"
}

variable "runner_ttl_minutes" {
  description = "The runner shuts itself down this long after boot. RDS keeps running: run `terraform destroy` when done."
  type        = number
  default     = 240

  validation {
    condition     = var.runner_ttl_minutes >= 30 && var.runner_ttl_minutes <= 1440
    error_message = "runner_ttl_minutes must be between 30 and 1440."
  }
}

variable "vpc_cidr" {
  type    = string
  default = "10.42.0.0/16"
}

variable "budget_email" {
  description = "Email for cost alerts. Empty = no budget created."
  type        = string
  default     = ""
}

variable "budget_usd" {
  description = "Monthly budget for resources tagged Project=dbbench. Needs the cost allocation tag 'Project' activated."
  type        = number
  default     = 20
}

variable "tags" {
  description = "Extra tags (owner, team, ticket...)."
  type        = map(string)
  default     = {}
}
