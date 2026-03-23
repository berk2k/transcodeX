package config

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

func NewAWSConfig() (aws.Config, error) {
	return awsconfig.LoadDefaultConfig(context.TODO(),
		awsconfig.WithRegion(os.Getenv("AWS_REGION")),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				os.Getenv("AWS_ACCESS_KEY"),
				os.Getenv("AWS_SECRET_KEY"),
				"",
			),
		),
	)
}

func NewS3Client(cfg aws.Config) *s3.Client {
	endpoint := os.Getenv("AWS_ENDPOINT")
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
}

func NewSQSClient(cfg aws.Config) *sqs.Client {
	endpoint := os.Getenv("AWS_ENDPOINT")
	return sqs.NewFromConfig(cfg, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})
}

func NewDynamoDBClient(cfg aws.Config) *dynamodb.Client {
	endpoint := os.Getenv("AWS_ENDPOINT")
	return dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})
}
