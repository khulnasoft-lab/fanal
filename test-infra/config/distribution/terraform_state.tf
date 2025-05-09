resource "aws_s3_bucket" "fanal-distribution-state-bucket" {
  bucket = "fanal-distribution-state-bucket"
  versioning {
    enabled = true
  }
  lifecycle {
    prevent_destroy = true
  }

  server_side_encryption_configuration {
    rule {
      bucket_key_enabled = false
      
      apply_server_side_encryption_by_default {
        sse_algorithm = "AES256"
      }
    }
  }
}

resource "aws_dynamodb_table" "fanal-distribution-state-bucket-lock" {
  name           = "fanal-distribution-state-bucket-lock"
  hash_key       = "LockID"
  read_capacity  = 20
  write_capacity = 20

  attribute {
    name = "LockID"
    type = "S"
  }
}
