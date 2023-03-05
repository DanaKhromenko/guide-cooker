locals {
  service_name  = "guide-cooker"
  namespace     = "${local.service_name}-${var.instance_name}"
  function_name = "${local.namespace}-lambda"

  tags = {
    Feature = "Guide"
    Info    = "Cloud Core Upgrade"
    Owner   = var.owner
    Env     = local.namespace
    TTL     = "AlwaysOn"
  }
}

data "aws_region" "current" {}

resource "null_resource" "aws_region_check" {
  count = (data.aws_region.current.name == var.db_region_config.primary) ? 0 : "AWS region mismatch"
}

resource "aws_cloudwatch_log_group" "lambda_log_group" {
  name              = "/aws/lambda/${local.function_name}"
  retention_in_days = var.cloudwatch_logs_retention_in_days
  tags              = local.tags
}

resource "aws_dynamodb_table" "guide_cells_dynamodb_table" {
  name             = "${local.namespace}-guide-cells"
  billing_mode     = "PAY_PER_REQUEST"
  stream_enabled   = true
  stream_view_type = "NEW_AND_OLD_IMAGES"
  tags             = local.tags
  hash_key         = "StationId"
  range_key        = "EndTime"

  attribute {
    name = "StationId"
    type = "S"
  }

  attribute {
    name = "EndTime"
    type = "N"
  }

  ttl {
    enabled        = true
    attribute_name = "ExpirationDate"
  }

  dynamic "replica" {
    for_each = var.db_region_config.replicas
    content {
      region_name    = replica.value
      propagate_tags = true
    }
  }
}

resource "aws_dynamodb_table" "deleted_guide_cells_dynamodb_table" {
  name         = "${local.namespace}-deleted-guide-cells"
  billing_mode = "PAY_PER_REQUEST"
  tags         = local.tags
  hash_key     = "StationId"
  range_key    = "EndTime"

  attribute {
    name = "StationId"
    type = "S"
  }

  attribute {
    name = "EndTime"
    type = "N"
  }

  ttl {
    enabled        = true
    attribute_name = "ExpirationDate"
  }
}

resource "aws_iam_role" "lambda_role" {
  name = "${local.function_name}-role"
  tags = local.tags
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
        Effect = "Allow"
      }
    ]
  })

  inline_policy {
    name = "logging-policy"
    policy = jsonencode({
      Version = "2012-10-17"
      Statement = [
        {
          Action = [
            "logs:CreateLogStream",
            "logs:PutLogEvents"
          ],
          Resource = "${aws_cloudwatch_log_group.lambda_log_group.arn}:*",
          Effect   = "Allow"
        }
      ]
    })
  }

  inline_policy {
    name = "network-policy"
    policy = jsonencode({
      Version = "2012-10-17"
      Statement = [
        {
          Action = [
            "ec2:CreateNetworkInterface",
            "ec2:DeleteNetworkInterface",
            "ec2:DescribeNetworkInterfaces",
            "ec2:DescribeSecurityGroups",
            "ec2:DescribeSubnets",
            "ec2:DescribeVpcs"
          ],
          Resource = "*",
          Effect   = "Allow"
        }
      ]
    })
  }

  inline_policy {
    name = "dynamodb-policy"
    policy = jsonencode({
      Version = "2012-10-17"
      Statement = [
        {
          Action = [
            "dynamodb:PutItem",
            "dynamodb:UpdateItem",
            "dynamodb:DeleteItem"
          ],
          Resource = [
            aws_dynamodb_table.guide_cells_dynamodb_table.arn,
            aws_dynamodb_table.deleted_guide_cells_dynamodb_table.arn
          ],
          Effect = "Allow"
        }
      ]
    })
  }
}

resource "aws_lambda_function" "lambda" {
  function_name    = local.function_name
  filename         = var.function_file
  source_code_hash = filebase64sha256(var.function_file)
  handler          = local.service_name
  runtime          = "go1.x"
  memory_size      = var.lambda_memory_size_in_mb
  role             = aws_iam_role.lambda_role.arn
  tags             = local.tags
  timeout          = var.lambda_timeout_in_seconds

  environment {
    variables = {
      GUIDE_CELLS_TABLE_NAME         = aws_dynamodb_table.guide_cells_dynamodb_table.name
      DELETED_GUIDE_CELLS_TABLE_NAME = aws_dynamodb_table.deleted_guide_cells_dynamodb_table.name
    }
  }
}

resource "aws_lambda_permission" "kafka_lambda_permission" {
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.lambda.function_name
  principal     = "kafka.amazonaws.com"
}

resource "aws_lambda_event_source_mapping" "kafka_lambda_event_source_mapping" {
  function_name     = aws_lambda_function.lambda.function_name
  topics            = [var.guide_cells_kafka_topic]
  starting_position = "TRIM_HORIZON"
  batch_size        = var.kafka_batch_size

  self_managed_event_source {
    endpoints = {
      KAFKA_BOOTSTRAP_SERVERS = var.kafka_endpoint
    }
  }

  self_managed_kafka_event_source_config {
    consumer_group_id = local.namespace
  }

  dynamic "source_access_configuration" {
    for_each = toset(var.vpc_subnets)
    content {
      type = "VPC_SUBNET"
      uri  = source_access_configuration.key
    }
  }

  source_access_configuration {
    type = "VPC_SECURITY_GROUP"
    uri  = var.vpc_security_group
  }
}
