resource "aws_sqs_queue" "dlq" {
  name = var.dlq_name
}

resource "aws_sqs_queue" "jobs" {
  name                       = var.jobs_queue_name
  visibility_timeout_seconds = 300
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3
  })
}