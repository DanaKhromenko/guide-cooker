package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"golang.org/x/exp/maps"
)

// These variables must be constants but, unfortunately, Golang does not support this.
var gConditionalCheckFailedException *types.ConditionalCheckFailedException
var gPublicationIdAttributeName = expression.Name("UdfPublicationId")

var gCtx context.Context
var gDynamodbClient DynamodbClient
var gGuideCellTtlInSecondsAfterEndTime int64
var gMaxNumberOfWorkers int
var gGuideCellsTableName *string
var gDeletedGuideCellsTableName *string

// START: Functions mocked in tests.

var gGetCurrentTimeInSecondsFn = func() int64 {
	return time.Now().Unix()
}

var gPrintlnFn = func(msg string) {
	fmt.Println(msg)
}

// END: Functions mocked in tests.

type DynamodbClient interface {
	TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
}

type GuideCellKey struct {
	ChannelId string // DynamoDB table partition key
	EndTime   int64  // DynamoDB table sort key
}

type BaseGuideCellFields struct {
	GuideCellKey
	ShowId        string
	StartTime     int64
	PublicationId int64
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

func parseKafkaRecordHeaders(headerArray []map[string][]byte) map[string]string {
	headerMap := make(map[string]string)
	for _, header := range headerArray {
		for headerKey := range header {
			headerMap[headerKey] = string(header[headerKey])
		}
	}
	return headerMap
}

func getRequiredKafkaRecordHeader(headers map[string]string, key string) (string, error) {
	if value, exists := headers[key]; exists && value != "" {
		return value, nil
	}
	return "", fmt.Errorf("missing or empty required Kafka record header '%v'", key)
}

func getRequiredInt64KafkaRecordHeader(headers map[string]string, key string) (int64, error) {
	value, err := getRequiredKafkaRecordHeader(headers, key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(value, 10, 64)
}

func decodeBase64String(encodedValue string) (string, error) {
	bytes, err := base64.StdEncoding.DecodeString(encodedValue)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func processKafkaMessage(kafkaMessage *GuideCellKafkaMessage) ProcessingResult {
	expirationDate := kafkaMessage.EndTime + gGuideCellTtlInSecondsAfterEndTime
	if expirationDate <= gGetCurrentTimeInSecondsFn() {
		return newSkippedProcessingResult("expired")
	}

	baseGuideCellDbItem := BaseGuideCellDbItem{
		BaseGuideCellFields:  kafkaMessage.BaseGuideCellFields,
		ExpirationDate:       expirationDate,
		KafkaPartitionOffset: fmt.Sprintf("%d:%d", kafkaMessage.Partition, kafkaMessage.Offset),
	}

	var putItemTable, deleteItemTable *string
	var item interface{}
	if kafkaMessage.GuideCell == "" {
		putItemTable, deleteItemTable = gDeletedGuideCellsTableName, gGuideCellsTableName
		item = &DeletedGuideCellDbItem{BaseGuideCellDbItem: baseGuideCellDbItem}
	} else {
		putItemTable, deleteItemTable = gGuideCellsTableName, gDeletedGuideCellsTableName
		item = &GuideCellDbItem{BaseGuideCellDbItem: baseGuideCellDbItem, GuideCell: kafkaMessage.GuideCell}
	}

	// Note: no error is expected here.
	expr, _ := expression.NewBuilder().WithCondition(expression.Or(
		gPublicationIdAttributeName.AttributeNotExists(),
		gPublicationIdAttributeName.LessThan(expression.Value(kafkaMessage.PublicationId)),
	)).Build()

	// Note: no error is expected here.
	putItemAttributeValue, _ := attributevalue.MarshalMap(item)
	putItem := types.TransactWriteItem{Put: &types.Put{
		TableName:                 putItemTable,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		Item:                      putItemAttributeValue,
	}}

	// Note: no error is expected here
	deleteItemKeyAttributeValue, _ := attributevalue.MarshalMap(kafkaMessage.GuideCellKey)
	deleteItem := types.TransactWriteItem{Delete: &types.Delete{
		TableName:                 deleteItemTable,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		Key:                       deleteItemKeyAttributeValue,
	}}

	_, err := gDynamodbClient.TransactWriteItems(gCtx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{putItem, deleteItem}})

	return newDynamodbProcessingResult(err)
}

func parseKafkaRecord(kafkaRecord *events.KafkaRecord) (*GuideCellKafkaMessage, error) {
	headers := parseKafkaRecordHeaders(kafkaRecord.Headers)

	guideKafkaMessage := GuideCellKafkaMessage{
		Topic:     kafkaRecord.Topic,
		Partition: kafkaRecord.Partition,
		Offset:    kafkaRecord.Offset,
	}

	channelId, convertErr := getRequiredKafkaRecordHeader(headers, "ChannelId")
	if convertErr != nil {
		return nil, convertErr
	}

	endTime, convertErr := getRequiredInt64KafkaRecordHeader(headers, "EndTime")
	if convertErr != nil {
		return nil, convertErr
	}

	guideKafkaMessage.BaseGuideCellFields = BaseGuideCellFields{GuideCellKey: GuideCellKey{
		ChannelId: channelId,
		EndTime:   endTime,
	}}

	guideKafkaMessage.PublicationId, convertErr = getRequiredInt64KafkaRecordHeader(headers, "PublicationId")
	if convertErr != nil {
		return nil, convertErr
	}

	guideKafkaMessage.StartTime, convertErr = getRequiredInt64KafkaRecordHeader(headers, "StartTime")
	if convertErr != nil {
		return nil, convertErr
	}

	guideKafkaMessage.ShowId, convertErr = decodeBase64String(kafkaRecord.Key)
	if convertErr != nil {
		return nil, convertErr
	}

	guideKafkaMessage.GuideCell, convertErr = decodeBase64String(kafkaRecord.Value)
	if convertErr != nil {
		return nil, convertErr
	}

	return &guideKafkaMessage, nil
}

func logProcessingResult(processingResult ProcessingResult, topic string, partition, offset int64) {
	if processingResult.Status == STORED {
		return
	}

	gPrintlnFn(fmt.Sprintf("Processed Kafka record: topic=%s, partition=%d, offset=%d, status=%s, details='%s'",
		topic, partition, offset, processingResult.Status, processingResult.Details))
}

func newSkippedProcessingResult(details string) ProcessingResult {
	return ProcessingResult{Status: SKIPPED, Details: details}
}

func newSkippedProcessingResultWithError(err error) ProcessingResult {
	return newSkippedProcessingResult(err.Error())
}

func newDynamodbProcessingResult(err error) ProcessingResult {
	if err == nil {
		return ProcessingResult{Status: STORED}
	}

	// Note: errors.Is() requires argument of type error, whereas errors.As() takes target as Any.
	// For some reason compiler does not complain to dynamodb.ConditionalCheckFailedException type for Is() method
	// but it does not works as expected. So, kept As() for now.
	if errors.As(err, &gConditionalCheckFailedException) {
		return newSkippedProcessingResult("outdated")
	} else {
		return ProcessingResult{Status: FAILED, Details: err.Error()}
	}
}

// Returns: map<ChannelId, GuideCellKafkaMessage[]> and the number of skipped (invalid) Kafka messages.
func getGuideCellKafkaMessages(kafkaEvent *events.KafkaEvent) (map[string][]GuideCellKafkaMessage, int32) {
	// Note: grouping Kafka messages by ChannelId to process sequentially all messages associated with a channel
	// to guarantee data consistency.
	skippedKafkaRecordCount := 0
	kafkaMessageMap := make(map[string][]GuideCellKafkaMessage)
	for _, kafkaRecords := range kafkaEvent.Records {
		for _, kafkaRecord := range kafkaRecords {
			newRecord := kafkaRecord
			kafkaMessage, err := parseKafkaRecord(&newRecord)
			if err != nil {
				logProcessingResult(newSkippedProcessingResultWithError(err), kafkaRecord.Topic, kafkaRecord.Partition, kafkaRecord.Offset)
				skippedKafkaRecordCount++
				continue
			}
			kafkaMessageMap[kafkaMessage.ChannelId] = append(kafkaMessageMap[kafkaMessage.ChannelId], *kafkaMessage)
		}
	}
	return kafkaMessageMap, int32(skippedKafkaRecordCount)
}

func handleKafkaEvent(kafkaEvent *events.KafkaEvent) error {
	guideCellKafkaMessageMap, skippedKafkaRecordCount := getGuideCellKafkaMessages(kafkaEvent)

	channelIdSlice := maps.Keys(guideCellKafkaMessageMap)

	var storedKafkaRecordCount int32 = 0
	var failedKafkaRecordCount int32 = 0
	statusCountMap := map[ProcessingStatus]*int32{
		STORED:  &storedKafkaRecordCount,
		SKIPPED: &skippedKafkaRecordCount,
		FAILED:  &failedKafkaRecordCount,
	}

	// Note: we need as many workers as the number of ChannelIds to be processed
	// but not more than configured for the lambda.
	goRoutineCount := len(channelIdSlice)
	if goRoutineCount > gMaxNumberOfWorkers {
		goRoutineCount = gMaxNumberOfWorkers
	}

	var wg sync.WaitGroup
	wg.Add(goRoutineCount)

	var channelIdIndexGlobal int32 = -1
	for i := 0; i < goRoutineCount; i++ {
		go func() {
			for {
				channelIdIndexToProcess := atomic.AddInt32(&channelIdIndexGlobal, 1)
				if channelIdIndexToProcess >= int32(len(channelIdSlice)) {
					break
				}

				channelId := channelIdSlice[channelIdIndexToProcess]
				for _, guideCellKafkaMessage := range guideCellKafkaMessageMap[channelId] {
					kafkaMessage := guideCellKafkaMessage
					processingResult := processKafkaMessage(&kafkaMessage)

					logProcessingResult(processingResult, kafkaMessage.Topic, kafkaMessage.Partition, kafkaMessage.Offset)

					statusCount := statusCountMap[processingResult.Status]
					atomic.AddInt32(statusCount, 1)

					// All Kafka messages associated with a station must be process sequentially to guarantee data
					// consistency. This means that processing for a station must be stopped at the 1st error.
					if processingResult.Status == FAILED {
						break
					}
				}
			}
			wg.Done()
		}()
	}

	// Waiting for all routines to finish.
	wg.Wait()

	if *statusCountMap[FAILED] > 0 {
		/**
		 * When an AWS Lambda instance returns an error, AWS Kafka is supposed to resend all records from the current
		 * batch for reprocessing. And this behavior is supposed to be repeated until the AWS Lambda instance exits
		 * without error.
		 *
		 * We intentionally do not interrupt batch processing immediately after receiving the first error.
		 * When working with the same batch again, already processed records will fall into the "skipped" category.
		 * This would affect overall statistics of the number of processed messages but it should be acceptable.
		 */
		return errors.New("At least one error occurred during processing!")
	}
	return nil
}

func main() {
	err := coldStart()
	if err != nil {
		panic(err)
	}

	lambda.Start(handleKafkaEvent)
}
