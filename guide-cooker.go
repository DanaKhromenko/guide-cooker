package main

import (
	"context"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

type DynamodbClient interface {
	TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
}

type GuideCellKey struct {
	ChannelId string // DynamoDB table partition key
	EndTime   int64  // DynamoDB table sort key
}

type BaseGuideCellFields struct {
	GuideCellKey
	ShowId    string
	StartTime int64
}

type GuideCellKafkaMessage struct {
	BaseGuideCellFields
	Topic     string
	Partition int64
	Offset    int64
	GuideCell string
}

type BaseGuideCellDbItem struct {
	BaseGuideCellFields
	ExpirationDate       int64
	KafkaPartitionOffset string
}

type GuideCellDbItem struct {
	BaseGuideCellDbItem
	GuideCell string
}

type DeletedGuideCellDbItem struct {
	BaseGuideCellDbItem
}

type ProcessingStatus string

const (
	STORED  ProcessingStatus = "stored"
	SKIPPED ProcessingStatus = "skipped"
	FAILED  ProcessingStatus = "failed"
)

/**
 * ProcessingResult is a special structure that stores a processing status and details, describing the error
 * or reason for skipping the Kafka record.
 */
type ProcessingResult struct {
	Status  ProcessingStatus
	Details string
}

func coldStart() error {
	return nil
}

func handleKafkaEvent(kafkaEvent *events.KafkaEvent) error {
	return nil
}

func main() {
	err := coldStart()
	if err != nil {
		panic(err)
	}

	lambda.Start(handleKafkaEvent)
}
