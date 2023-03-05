output "db_config" {
  value = {
    primary_region    = var.db_region_config.primary
    replica_regions   = var.db_region_config.replicas
    guide_cells_table = aws_dynamodb_table.guide_cells_dynamodb_table.name
  }
}

output "deleted_guide_cells_table" {
  value = aws_dynamodb_table.deleted_guide_cells_dynamodb_table.name
}

output "function_name" {
  value = aws_lambda_function.lambda.function_name
}
