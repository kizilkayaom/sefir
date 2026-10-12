resource "aws_s3_bucket" "screenshots" {
  bucket_prefix = "sefir-screenshots-"

  tags = {
    Project     = "sefir"
    Environment = "dev"
  }
}

resource "aws_s3_bucket_public_access_block" "screenshots" {
  bucket                  = aws_s3_bucket.screenshots.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
} 