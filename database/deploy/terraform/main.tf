data "aws_caller_identity" "current" {}

data "aws_availability_zones" "available" {
  state = "available"
}

# Latest Amazon Linux 2023, arm64 (matches `GOARCH=arm64` in deploy/README.md).
data "aws_ssm_parameter" "al2023_arm64" {
  name = var.runner_ami_ssm_parameter
}

locals {
  azs = slice(data.aws_availability_zones.available.names, 0, 2)

  secret_arns = compact([
    var.enable_postgres ? aws_db_instance.postgres[0].master_user_secret[0].secret_arn : "",
    var.enable_mysql ? aws_db_instance.mysql[0].master_user_secret[0].secret_arn : "",
  ])
}

# ---------------------------------------------------------------- network
# No NAT gateway (it costs money by the hour). The databases sit in private subnets
# with no route to the internet; only the runner is in a public subnet, and it has
# no inbound rules at all (you reach it through SSM Session Manager).

resource "aws_vpc" "this" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "${var.name_prefix}-vpc" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "${var.name_prefix}-igw" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = cidrsubnet(var.vpc_cidr, 8, 0)
  availability_zone       = local.azs[0]
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.name_prefix}-public" }
}

resource "aws_subnet" "private" {
  count             = 2 # RDS subnet groups need two AZs
  vpc_id            = aws_vpc.this.id
  cidr_block        = cidrsubnet(var.vpc_cidr, 8, count.index + 1)
  availability_zone = local.azs[count.index]
  tags              = { Name = "${var.name_prefix}-private-${count.index}" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "${var.name_prefix}-public" }

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

# ---------------------------------------------------------------- security groups

resource "aws_security_group" "runner" {
  name        = "${var.name_prefix}-runner"
  description = "dbbench runner: no inbound, limited outbound"
  vpc_id      = aws_vpc.this.id
}

resource "aws_vpc_security_group_egress_rule" "runner_https" {
  security_group_id = aws_security_group.runner.id
  description       = "HTTPS to SSM, S3, Secrets Manager, package repos"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_vpc_security_group_egress_rule" "runner_postgres" {
  security_group_id = aws_security_group.runner.id
  description       = "PostgreSQL inside the VPC"
  ip_protocol       = "tcp"
  from_port         = var.postgres_port
  to_port           = var.postgres_port
  cidr_ipv4         = var.vpc_cidr
}

resource "aws_vpc_security_group_egress_rule" "runner_mysql" {
  security_group_id = aws_security_group.runner.id
  description       = "MySQL inside the VPC"
  ip_protocol       = "tcp"
  from_port         = var.mysql_port
  to_port           = var.mysql_port
  cidr_ipv4         = var.vpc_cidr
}

resource "aws_security_group" "db" {
  name        = "${var.name_prefix}-db"
  description = "dbbench databases: only the runner may connect"
  vpc_id      = aws_vpc.this.id
}

resource "aws_vpc_security_group_ingress_rule" "db_postgres" {
  security_group_id            = aws_security_group.db.id
  description                  = "PostgreSQL from the runner"
  ip_protocol                  = "tcp"
  from_port                    = var.postgres_port
  to_port                      = var.postgres_port
  referenced_security_group_id = aws_security_group.runner.id
}

resource "aws_vpc_security_group_ingress_rule" "db_mysql" {
  security_group_id            = aws_security_group.db.id
  description                  = "MySQL from the runner"
  ip_protocol                  = "tcp"
  from_port                    = var.mysql_port
  to_port                      = var.mysql_port
  referenced_security_group_id = aws_security_group.runner.id
}

# ---------------------------------------------------------------- artifacts bucket
# Holds the dbbench binary, the run script, an optional targets.yml, and results.

resource "aws_s3_bucket" "artifacts" {
  bucket        = "${var.name_prefix}-artifacts-${data.aws_caller_identity.current.account_id}-${var.region}"
  force_destroy = true # `terraform destroy` must be able to clean up
}

resource "aws_s3_bucket_public_access_block" "artifacts" {
  bucket                  = aws_s3_bucket.artifacts.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ---------------------------------------------------------------- databases
# Disposable by design: no Multi-AZ, no backups, no deletion protection, no
# storage autoscaling, not publicly accessible. Passwords are generated and kept
# in Secrets Manager by RDS (they never appear in Terraform variables or code).

resource "aws_db_subnet_group" "this" {
  name       = "${var.name_prefix}-db"
  subnet_ids = aws_subnet.private[*].id
}

resource "aws_db_instance" "postgres" {
  count = var.enable_postgres ? 1 : 0

  identifier     = "${var.name_prefix}-postgres"
  engine         = "postgres"
  engine_version = var.postgres_version
  port           = var.postgres_port
  instance_class = var.db_instance_class

  allocated_storage = var.db_storage_gb
  storage_type      = "gp3"
  storage_encrypted = true

  db_name                     = var.db_name
  username                    = var.db_username
  manage_master_user_password = true

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.db.id]
  publicly_accessible    = false
  multi_az               = false
  availability_zone      = local.azs[0] # same AZ as the runner: no cross-AZ latency in results

  backup_retention_period      = 0
  skip_final_snapshot          = true
  deletion_protection          = false
  apply_immediately            = true
  performance_insights_enabled = false
}

resource "aws_db_instance" "mysql" {
  count = var.enable_mysql ? 1 : 0

  identifier     = "${var.name_prefix}-mysql"
  engine         = "mysql"
  engine_version = var.mysql_version
  port           = var.mysql_port
  instance_class = var.db_instance_class

  allocated_storage = var.db_storage_gb
  storage_type      = "gp3"
  storage_encrypted = true

  db_name                     = var.db_name
  username                    = var.db_username
  manage_master_user_password = true

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.db.id]
  publicly_accessible    = false
  multi_az               = false
  availability_zone      = local.azs[0] # same AZ as the runner: no cross-AZ latency in results

  backup_retention_period      = 0
  skip_final_snapshot          = true
  deletion_protection          = false
  apply_immediately            = true
  performance_insights_enabled = false
}

# ---------------------------------------------------------------- runner

data "aws_iam_policy_document" "assume_ec2" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "runner" {
  name               = "${var.name_prefix}-runner"
  assume_role_policy = data.aws_iam_policy_document.assume_ec2.json
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.runner.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

data "aws_iam_policy_document" "runner" {
  statement {
    sid       = "ArtifactsRead"
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.artifacts.arn}/*"]
  }
  statement {
    sid       = "ResultsWrite"
    actions   = ["s3:PutObject"]
    resources = ["${aws_s3_bucket.artifacts.arn}/results/*"]
  }
  statement {
    sid       = "ListBucket"
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.artifacts.arn]
  }
  dynamic "statement" {
    for_each = length(local.secret_arns) > 0 ? [1] : []
    content {
      sid       = "ReadDbSecrets"
      actions   = ["secretsmanager:GetSecretValue"]
      resources = local.secret_arns
    }
  }
}

resource "aws_iam_role_policy" "runner" {
  name   = "dbbench-runner"
  role   = aws_iam_role.runner.id
  policy = data.aws_iam_policy_document.runner.json
}

resource "aws_iam_instance_profile" "runner" {
  name = "${var.name_prefix}-runner"
  role = aws_iam_role.runner.name
}

resource "aws_instance" "runner" {
  ami                    = data.aws_ssm_parameter.al2023_arm64.value
  instance_type          = var.runner_instance_type
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.runner.id]
  iam_instance_profile   = aws_iam_instance_profile.runner.name

  associate_public_ip_address          = true
  instance_initiated_shutdown_behavior = "stop"
  user_data_replace_on_change          = true

  metadata_options {
    http_tokens = "required" # IMDSv2 only
  }

  root_block_device {
    volume_type = "gp3"
    volume_size = 16
    encrypted   = true
  }

  user_data = templatefile("${path.module}/runner-user-data.sh.tftpl", {
    region        = var.region
    bucket        = aws_s3_bucket.artifacts.bucket
    ttl_minutes   = var.runner_ttl_minutes
    db_name       = var.db_name
    pg_port       = var.postgres_port
    my_port       = var.mysql_port
    pg_host       = var.enable_postgres ? aws_db_instance.postgres[0].address : ""
    pg_secret_arn = var.enable_postgres ? aws_db_instance.postgres[0].master_user_secret[0].secret_arn : ""
    pg_label      = "rds postgres ${var.postgres_version} ${var.db_instance_class} ${var.db_storage_gb}GB ${var.region}"
    my_host       = var.enable_mysql ? aws_db_instance.mysql[0].address : ""
    my_secret_arn = var.enable_mysql ? aws_db_instance.mysql[0].master_user_secret[0].secret_arn : ""
    my_label      = "rds mysql ${var.mysql_version} ${var.db_instance_class} ${var.db_storage_gb}GB ${var.region}"
  })

  tags = { Name = "${var.name_prefix}-runner" }
}

# ---------------------------------------------------------------- cost alert (optional)

resource "aws_budgets_budget" "this" {
  count = var.budget_email == "" ? 0 : 1

  name         = "${var.name_prefix}-monthly"
  budget_type  = "COST"
  limit_amount = tostring(var.budget_usd)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  cost_filter {
    name   = "TagKeyValue"
    values = ["user:Project$dbbench"]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.budget_email]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.budget_email]
  }
}
