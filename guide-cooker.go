package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// START: Functions mocked in tests.

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

	gPrintlnFn(fmt.Sprintf("guideCellKafkaMessageMap=%v", guideCellKafkaMessageMap))
	gPrintlnFn(fmt.Sprintf("skippedKafkaRecordCount=%v", skippedKafkaRecordCount))

	return nil
}

func main() {
	err := coldStart()
	if err != nil {
		panic(err)
	}

	lambda.Start(handleKafkaEvent)
}
