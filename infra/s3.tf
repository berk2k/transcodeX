resource "aws_s3_bucket" "raw_videos" {
  bucket = var.raw_bucket_name
}

resource "aws_s3_bucket" "processed_videos" {
  bucket = var.processed_bucket_name
}