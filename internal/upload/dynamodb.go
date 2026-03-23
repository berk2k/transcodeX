package upload

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func createJob(ctx context.Context, client *dynamodb.Client, tableName, jobID, s3Key string) error {
	_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item: map[string]types.AttributeValue{
			"jobId":     &types.AttributeValueMemberS{Value: jobID},
			"s3Key":     &types.AttributeValueMemberS{Value: s3Key},
			"status":    &types.AttributeValueMemberS{Value: "queued"},
			"createdAt": &types.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339)},
		},
	})
	return err
}
