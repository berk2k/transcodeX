variable "raw_bucket_name" {
  default = "transcodex-raw-videos"
}

variable "processed_bucket_name" {
  default = "transcodex-processed-videos"
}

variable "jobs_queue_name" {
  default = "transcodeX-jobs"
}

variable "dlq_name" {
  default = "transcodeX-dlq"
}

variable "dynamodb_table_name" {
  default = "transcodeX-jobs"
}