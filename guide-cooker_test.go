package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

const (
	MISSING_ENV_VAR     string = "missing required environment variable '%v'"
	TOPIC               string = "<TestDataCenter>.<TestEnv>.guide-cells"
	RECORD_KEY          string = "dGl2bzpzdC40NTY3ODk="
	RECORD_VALUE        string = "e1Rlc3RDb29rZXJHdWlkZUNlbGx9"
	OFFSET              int64  = 1
	PARTITION           int64  = 0
	DYNAMODB_ERR_TX_MSG string = "Error: Dynamodb transaction can't be completed"
	ERROR_OCCURED_MSG   string = "At least one error occurred during processing!"
)

// Note: must be cleaned up with cleanLogMessages() before use.
var gCapturedLogMessages []string
var gExpectedLogMessages []string

// Note: declared as variables because it is impossible to get a pointer to a constant.
var gChannelId1 string = "ch.456789"
var gChannelId2 string = "ch.987654"
var gCurrentUnixTime int64 = time.Now().Unix()
var gExpiredRecordUnixTime int64 = time.Now().Unix() - 8*24*60*60
var gEmptyInt64 int64 = -1
var gPublicationId int64 = 123456

var gDynamodbClientMock *DynamodbClientMock
var gDynamodbOptions []func(*dynamodb.Options)

type DynamodbClientMock struct {
	mock.Mock
}

func (c *DynamodbClientMock) TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	args := c.Called(ctx, params, optFns)

	var transactWriteItemsOutput *dynamodb.TransactWriteItemsOutput
	if arg := args.Get(0); arg != nil {
		transactWriteItemsOutput = arg.(*dynamodb.TransactWriteItemsOutput)
	}
	return transactWriteItemsOutput, args.Error(1)
}

func (c *DynamodbClientMock) expectNoError(input *dynamodb.TransactWriteItemsInput) {
	gDynamodbClientMock.On("TransactWriteItems", gCtx, input, gDynamodbOptions).Return(&dynamodb.TransactWriteItemsOutput{}, nil).Times(1)
}

func (c *DynamodbClientMock) expectError(input *dynamodb.TransactWriteItemsInput, errorMsg string) {
	gDynamodbClientMock.On("TransactWriteItems", gCtx, input, gDynamodbOptions).Return(nil, fmt.Errorf(errorMsg)).Times(1)
}

func (c *DynamodbClientMock) expectErrorForChannelId(channelId string, errorMsg string) {
	gDynamodbClientMock.On("TransactWriteItems", gCtx, mock.MatchedBy(func(input *dynamodb.TransactWriteItemsInput) bool {
		return getStringValueFromAttributeValue(input.TransactItems[0].Put.Item["ChannelId"]) == channelId
	}), gDynamodbOptions).Return(nil, fmt.Errorf(errorMsg)).Times(1)
}

func (c *DynamodbClientMock) expectConditionalCheckFailedError(input *dynamodb.TransactWriteItemsInput) {
	gDynamodbClientMock.On("TransactWriteItems", gCtx, input, gDynamodbOptions).Return(nil, gConditionalCheckFailedException).Times(1)
}

func setup() {
	os.Setenv("GUIDE_CELLS_TABLE_NAME", "testGuideCellsTable")
	os.Setenv("DELETED_GUIDE_CELLS_TABLE_NAME", "testDeletedGuideCellsTable")
	os.Setenv("GUIDE_CELL_TTL_IN_DAYS_AFTER_END_TIME", "8")
	os.Setenv("MAX_NUMBER_OF_WORKERS", "32")
	os.Setenv("METRICS_NAMESPACE", "cloudcore-guide-cooker")

	err := coldStart()
	if err != nil {
		panic(err)
	}
}

func init() {
	// Note: redefining gPrintlnFn function to save log with Kafka record storing result.
	gPrintlnFnOrig := gPrintlnFn
	gPrintlnFn = func(msg string) {
		gCapturedLogMessages = append(gCapturedLogMessages, msg)
		gPrintlnFnOrig(msg)
	}
}

func cleanLogMessages() {
	gCapturedLogMessages = []string{}
	gExpectedLogMessages = []string{}
}

func appendExpectedLogMessage(msg string) {
	gExpectedLogMessages = append(gExpectedLogMessages, msg)
}

func assertEqualLogMessages(t *testing.T) {
	assert.Equal(t, gExpectedLogMessages, gCapturedLogMessages)
}

func createKafkaEventWithRecords(kafkaRecords []events.KafkaRecord) *events.KafkaEvent {
	kafkaRecordsMap := map[string][]events.KafkaRecord{
		fmt.Sprintf("%s-%d", TOPIC, PARTITION): kafkaRecords,
	}
	return &events.KafkaEvent{
		Records: kafkaRecordsMap,
	}
}

func createKafkaEventWithRecord(kafkaRecord *events.KafkaRecord) *events.KafkaEvent {
	return createKafkaEventWithRecords([]events.KafkaRecord{*kafkaRecord})
}

// Note: header will not be added if the value is nil.
func addHeaderToHeaderMap(headers *[]map[string][]byte, key string, value interface{}) {
	var byteValue []byte

	switch convertedValue := value.(type) {
	case *int64:
		if convertedValue == nil {
			return
		} else if *convertedValue == gEmptyInt64 {
			byteValue = []byte("")
		} else {
			byteValue = []byte(strconv.FormatInt(*convertedValue, 10))
		}
	case *string:
		if convertedValue == nil {
			return
		}
		byteValue = []byte(*convertedValue)
	}

	*headers = append(*headers, map[string][]byte{key: byteValue})
}

func createTransactWriteItemsInput(showId, guideCell string, channelId *string, startTime, endTime *int64, publicationId *int64, offset int64) *dynamodb.TransactWriteItemsInput {
	// Note: no error is expected here.
	decodedShowId, _ := decodeBase64String(showId)
	baseGuideCellDbItem := BaseGuideCellDbItem{
		BaseGuideCellFields: BaseGuideCellFields{
			GuideCellKey:  GuideCellKey{ChannelId: *channelId, EndTime: *endTime},
			ShowId:        decodedShowId,
			StartTime:     *startTime,
			PublicationId: *publicationId,
		},
		ExpirationDate:       *endTime + gGuideCellTtlInSecondsAfterEndTime,
		KafkaPartitionOffset: fmt.Sprintf("%d:%d", PARTITION, offset),
	}

	// Note: no error is expected here.
	decodedGuideCell, _ := decodeBase64String(guideCell)
	var putItemTable, deleteItemTable *string
	var item interface{}
	if guideCell == "" {
		putItemTable, deleteItemTable = gDeletedGuideCellsTableName, gGuideCellsTableName
		item = &DeletedGuideCellDbItem{BaseGuideCellDbItem: baseGuideCellDbItem}
	} else {
		putItemTable, deleteItemTable = gGuideCellsTableName, gDeletedGuideCellsTableName
		item = &GuideCellDbItem{BaseGuideCellDbItem: baseGuideCellDbItem, GuideCell: decodedGuideCell}
	}

	// Note: no error is expected here.
	expr, _ := expression.NewBuilder().WithCondition(expression.Or(
		gPublicationIdAttributeName.AttributeNotExists(),
		gPublicationIdAttributeName.LessThan(expression.Value(publicationId)),
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
	deleteItemKeyAttributeValue, _ := attributevalue.MarshalMap(baseGuideCellDbItem.GuideCellKey)
	deleteItem := types.TransactWriteItem{Delete: &types.Delete{
		TableName:                 deleteItemTable,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		Key:                       deleteItemKeyAttributeValue,
	}}

	return &dynamodb.TransactWriteItemsInput{TransactItems: []types.TransactWriteItem{putItem, deleteItem}}
}

func createKafkaRecord(showId, guideCell string, channelId *string, startTime, endTime *int64, publicationId *int64, offset int64, createInput bool) (*events.KafkaRecord, *dynamodb.TransactWriteItemsInput) {
	headers := []map[string][]byte{}
	addHeaderToHeaderMap(&headers, "ChannelId", channelId)
	addHeaderToHeaderMap(&headers, "StartTime", startTime)
	addHeaderToHeaderMap(&headers, "EndTime", endTime)
	addHeaderToHeaderMap(&headers, "PublicationId", publicationId)

	kafkaRecord := &events.KafkaRecord{
		Headers: headers,
	}

	kafkaRecord.Key = showId
	kafkaRecord.Value = guideCell
	kafkaRecord.Offset = offset
	kafkaRecord.Topic = TOPIC
	kafkaRecord.Partition = PARTITION

	var transactWriteItemsInput *dynamodb.TransactWriteItemsInput
	if createInput {
		transactWriteItemsInput = createTransactWriteItemsInput(showId, guideCell, channelId, startTime, endTime, publicationId, offset)
	}
	return kafkaRecord, transactWriteItemsInput
}

func createKafkaRecordUsingKeyValue(showId, guideCell string) (*events.KafkaRecord, *dynamodb.TransactWriteItemsInput) {
	return createKafkaRecord(showId, guideCell, &gChannelId1, &gCurrentUnixTime, &gCurrentUnixTime, &gPublicationId, OFFSET, true)
}

func createKafkaRecordUsingHeaderAttributes(channelId *string, startTime, endTime *int64, publicationId *int64) (*events.KafkaRecord, *dynamodb.TransactWriteItemsInput) {
	return createKafkaRecord(RECORD_KEY, RECORD_VALUE, channelId, startTime, endTime, publicationId, OFFSET, true)
}

func createKafkaRecordUsingHeaderAttributesWithoutExpDbInput(channelId *string, startTime, endTime *int64, publicationId *int64) *events.KafkaRecord {
	kafkaRecord, _ := createKafkaRecord(RECORD_KEY, RECORD_VALUE, channelId, startTime, endTime, publicationId, OFFSET, false)
	return kafkaRecord
}

func createKafkaRecordUsingOffset(offset int64) (*events.KafkaRecord, *dynamodb.TransactWriteItemsInput) {
	return createKafkaRecord(RECORD_KEY, RECORD_VALUE, &gChannelId1, &gCurrentUnixTime, &gCurrentUnixTime, &gPublicationId, offset, true)
}

func buildKafkaRecordLogMessage(offset int64, details string, status string) string {
	return fmt.Sprintf("Processed Kafka record: topic=%s, partition=%d, offset=%d, status=%s, details='%s'",
		TOPIC, PARTITION, offset, status, details)
}

func buildSkippedKafkaRecordLogMessage(offset int64, details string) string {
	return buildKafkaRecordLogMessage(offset, details, "skipped")
}

func getStringValueFromAttributeValue(attributeValue types.AttributeValue) string {
	var stringValue string
	_ = attributevalue.Unmarshal(attributeValue, &stringValue)
	return stringValue
}

type TestSuite struct {
	suite.Suite
}

func (s *TestSuite) SetupTest() {
	setup()
	cleanLogMessages()

	gDynamodbClientMock = new(DynamodbClientMock)
	gDynamodbClient = gDynamodbClientMock
}

func (s *TestSuite) TearDownTest() {
	assertEqualLogMessages(s.T())
	gDynamodbClientMock.AssertExpectations(s.T())
}

func TestHandleKafkaEvent(t *testing.T) {
	suite.Run(t, &TestSuite{})
}

func (s *TestSuite) TestHandleKafkaEvent_Stored() {
	putKafkaRecord, putTransactWriteItemsInput := createKafkaRecordUsingKeyValue(RECORD_KEY, RECORD_VALUE)
	deleteKafkaRecord, deleteTransactWriteItemsInput := createKafkaRecordUsingKeyValue(RECORD_KEY, "")

	gDynamodbClientMock.expectNoError(putTransactWriteItemsInput)
	gDynamodbClientMock.expectNoError(deleteTransactWriteItemsInput)

	var operations = map[string]*events.KafkaRecord{
		"Put":    putKafkaRecord,
		"Delete": deleteKafkaRecord,
	}

	for storeMsg, kafkaRecord := range operations {
		s.T().Run(storeMsg, func(t *testing.T) {
			assert.NoError(t, handleKafkaEvent(createKafkaEventWithRecord(kafkaRecord)))
		})
	}
}

func (s *TestSuite) TestHandleKafkaEvent_Outdated() {
	outdatedError := "outdated"

	putKafkaRecord, putTransactWriteItemsInput := createKafkaRecordUsingKeyValue(RECORD_KEY, RECORD_VALUE)
	deleteKafkaRecord, deleteTransactWriteItemsInput := createKafkaRecordUsingKeyValue(RECORD_KEY, "")

	gDynamodbClientMock.expectConditionalCheckFailedError(putTransactWriteItemsInput)
	gDynamodbClientMock.expectConditionalCheckFailedError(deleteTransactWriteItemsInput)

	var operations = map[string]*events.KafkaRecord{
		"Put":    putKafkaRecord,
		"Delete": deleteKafkaRecord,
	}

	for operation, kafkaRecord := range operations {
		s.T().Run(operation, func(t *testing.T) {
			appendExpectedLogMessage(buildSkippedKafkaRecordLogMessage(OFFSET, outdatedError))

			assert.NoError(t, handleKafkaEvent(createKafkaEventWithRecord(kafkaRecord)))
		})
	}
}

func (s *TestSuite) TestHandleKafkaEvent_DynamoDbError() {
	putKafkaRecord, putTransactWriteItemsInput := createKafkaRecordUsingKeyValue(RECORD_KEY, RECORD_VALUE)
	deleteKafkaRecord, deleteTransactWriteItemsInput := createKafkaRecordUsingKeyValue(RECORD_KEY, "")

	dynamoDBError := "some DynamoDB error"
	gDynamodbClientMock.expectError(putTransactWriteItemsInput, dynamoDBError)
	gDynamodbClientMock.expectError(deleteTransactWriteItemsInput, dynamoDBError)

	var operations = map[string]*events.KafkaRecord{
		"Put":    putKafkaRecord,
		"Delete": deleteKafkaRecord,
	}

	for operation, kafkaRecord := range operations {
		s.T().Run(operation, func(t *testing.T) {
			appendExpectedLogMessage(buildKafkaRecordLogMessage(OFFSET, dynamoDBError, "failed"))

			assert.EqualError(t, handleKafkaEvent(createKafkaEventWithRecord(kafkaRecord)),
				ERROR_OCCURED_MSG)
		})
	}
}

func (s *TestSuite) TestHandleKafkaEvent_ExpiredItem() {
	appendExpectedLogMessage(buildSkippedKafkaRecordLogMessage(OFFSET, "expired"))
	kafkaRecord := createKafkaRecordUsingHeaderAttributesWithoutExpDbInput(&gChannelId1, &gCurrentUnixTime, &gExpiredRecordUnixTime, &gPublicationId)

	err := handleKafkaEvent(createKafkaEventWithRecord(kafkaRecord))

	assert.NoError(s.T(), err)
}

func (s *TestSuite) TestHandleKafkaEvent_MultipleRecords() {
	var kafkaRecords []events.KafkaRecord
	for i := 1; i <= 3; i++ {
		kafkaRecord, transactWriteItemsInput := createKafkaRecordUsingOffset(int64(i))
		gDynamodbClientMock.expectNoError(transactWriteItemsInput)

		kafkaRecords = append(kafkaRecords, *kafkaRecord)
	}

	err := handleKafkaEvent(createKafkaEventWithRecords(kafkaRecords))

	assert.NoError(s.T(), err)
}

func (s *TestSuite) TestHandleKafkaEvent_NoRecords() {
	err := handleKafkaEvent(createKafkaEventWithRecords([]events.KafkaRecord{}))
	assert.NoError(s.T(), err)
}

func (s *TestSuite) TestHandleKafkaEvent_MultipleRecords_DynamoDbError() {
	appendExpectedLogMessage(buildKafkaRecordLogMessage(OFFSET, DYNAMODB_ERR_TX_MSG, string(FAILED)))
	gDynamodbClientMock.expectErrorForChannelId(gChannelId2, DYNAMODB_ERR_TX_MSG)

	var kafkaRecords []events.KafkaRecord

	for i := 1; i <= 3; i++ {
		// Records for channel 1:
		kafkaRecord, transactWriteItemsInput := createKafkaRecordUsingHeaderAttributes(&gChannelId1, &gCurrentUnixTime, &gCurrentUnixTime, &gPublicationId)
		gDynamodbClientMock.expectNoError(transactWriteItemsInput)

		kafkaRecords = append(kafkaRecords, *kafkaRecord)

		// Records for channel 2:
		kafkaRecords = append(kafkaRecords, *createKafkaRecordUsingHeaderAttributesWithoutExpDbInput(&gChannelId2, &gCurrentUnixTime, &gCurrentUnixTime, &gPublicationId))
	}

	err := handleKafkaEvent(createKafkaEventWithRecords(kafkaRecords))

	assert.EqualError(s.T(), err, ERROR_OCCURED_MSG)
}

func (s *TestSuite) TestRequiredKafkaRecordHeader_EmptyOrMissing() {
	var headersAndRecords = map[string]*events.KafkaRecord{
		"ChannelId_missing":     createKafkaRecordUsingHeaderAttributesWithoutExpDbInput(nil, &gCurrentUnixTime, &gCurrentUnixTime, &gPublicationId),
		"StartTime_missing":     createKafkaRecordUsingHeaderAttributesWithoutExpDbInput(&gChannelId1, nil, &gCurrentUnixTime, &gPublicationId),
		"EndTime_missing":       createKafkaRecordUsingHeaderAttributesWithoutExpDbInput(&gChannelId1, &gCurrentUnixTime, nil, &gPublicationId),
		"PublicationId_missing": createKafkaRecordUsingHeaderAttributesWithoutExpDbInput(&gChannelId1, &gCurrentUnixTime, &gCurrentUnixTime, nil),
		"ChannelId_empty":       createKafkaRecordUsingHeaderAttributesWithoutExpDbInput(new(string), &gCurrentUnixTime, &gCurrentUnixTime, &gPublicationId),
		"StartTime_empty":       createKafkaRecordUsingHeaderAttributesWithoutExpDbInput(&gChannelId1, &gEmptyInt64, &gCurrentUnixTime, &gPublicationId),
		"EndTime_empty":         createKafkaRecordUsingHeaderAttributesWithoutExpDbInput(&gChannelId1, &gCurrentUnixTime, &gEmptyInt64, &gPublicationId),
		"PublicationId_empty":   createKafkaRecordUsingHeaderAttributesWithoutExpDbInput(&gChannelId1, &gCurrentUnixTime, &gCurrentUnixTime, &gEmptyInt64),
	}

	for kafkaHeader, kafkaRecord := range headersAndRecords {
		s.T().Run(kafkaHeader, func(t *testing.T) {
			appendExpectedLogMessage(buildSkippedKafkaRecordLogMessage(OFFSET, fmt.Sprintf(
				"missing or empty required Kafka record header '%s'", kafkaHeader[0:strings.Index(kafkaHeader, "_")])))

			assert.NoError(t, handleKafkaEvent(createKafkaEventWithRecord(kafkaRecord)))
		})
	}
}

func (s *TestSuite) TestDecodeBase64String_IllegalBase64Data() {
	nonEncodedRecordKey := "1"
	nonEncodedRecordValue := "{GuideCell}"

	keyKafkaRecord, _ := createKafkaRecordUsingKeyValue(nonEncodedRecordKey, RECORD_VALUE)
	valueKafkaRecord, _ := createKafkaRecordUsingKeyValue(RECORD_KEY, nonEncodedRecordValue)

	var headersAndRecords = map[string]*events.KafkaRecord{
		"Key":   keyKafkaRecord,
		"Value": valueKafkaRecord,
	}

	for kafkaRecordAttribute, kafkaRecord := range headersAndRecords {
		s.T().Run(kafkaRecordAttribute, func(t *testing.T) {
			cleanLogMessages()
			appendExpectedLogMessage(buildSkippedKafkaRecordLogMessage(OFFSET, "illegal base64 data at input byte 0"))
			assert.NoError(t, handleKafkaEvent(createKafkaEventWithRecord(kafkaRecord)))
		})
	}
}

func (s *TestSuite) TestColdStart_NonIntValueForIntVar() {
	envVarStringValue := "SomeStringVariableValue"
	var envVars = []string{
		"GUIDE_CELL_TTL_IN_DAYS_AFTER_END_TIME",
		"MAX_NUMBER_OF_WORKERS",
	}

	for _, envVarName := range envVars {
		s.T().Run(envVarName, func(t *testing.T) {
			os.Setenv(envVarName, envVarStringValue)

			err := coldStart()

			assert.EqualError(t, err, fmt.Sprintf("strconv.Atoi: parsing \"%s\": invalid syntax", envVarStringValue))
		})
	}
}

func (s *TestSuite) TestColdStart_MissingEnvVar() {
	var envVars = []string{
		"GUIDE_CELLS_TABLE_NAME",
		"DELETED_GUIDE_CELLS_TABLE_NAME",
		"GUIDE_CELL_TTL_IN_DAYS_AFTER_END_TIME",
		"MAX_NUMBER_OF_WORKERS",
	}
	for _, envVarName := range envVars {
		s.T().Run(envVarName, func(t *testing.T) {
			defer setup()
			os.Unsetenv(envVarName)

			err := coldStart()

			assert.EqualError(t, err, fmt.Sprintf(MISSING_ENV_VAR, envVarName))
		})
	}
}

func (s *TestSuite) TestColdStart_Error_AwsConfig() {
	origLoadAwsConfigFn := gLoadAwsConfigFn
	defer func() {
		gLoadAwsConfigFn = origLoadAwsConfigFn
	}()

	awsError := "some error"
	gLoadAwsConfigFn = func(ctx context.Context) (aws.Config, error) {
		return aws.Config{}, fmt.Errorf(awsError)
	}

	err := coldStart()

	assert.EqualError(s.T(), err, "failed to load AWS config: "+awsError)
}

func (s *TestSuite) TestColdStart() {
	err := coldStart()

	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "testGuideCellsTable", *gGuideCellsTableName)
	assert.Equal(s.T(), "testDeletedGuideCellsTable", *gDeletedGuideCellsTableName)
	assert.Equal(s.T(), int64(8*24*60*60), gGuideCellTtlInSecondsAfterEndTime)
	assert.Equal(s.T(), 32, gMaxNumberOfWorkers)
}
