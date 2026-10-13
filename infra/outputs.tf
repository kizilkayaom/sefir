output "screenshots_bucket_name" {
  value = aws_s3_bucket.screenshots.bucket
}

output "screenshots_bucket_arn" {
  value = aws_s3_bucket.screenshots.arn
}