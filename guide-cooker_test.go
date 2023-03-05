package main

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

const (
	TOPIC        string = "<TestDataCenter>.<TestEnvironment>.guide-cells"
	RECORD_KEY   string = "TestRecordKey"
	RECORD_VALUE string = "TestRecordValue"
	OFFSET       int64  = 1
	PARTITION    int64  = 0
)

// Note: declared as variables because it is impossible to get a pointer to a constant.
var gCurrentUnixTime int64 = time.Now().Unix()
var gEmptyInt64 int64 = -1
var gChannelId string = "ch.100"

type TestSuite struct {
	suite.Suite
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

func createKafkaRecord(showId, guideCell string, channelId *string, startTime, endTime *int64, offset int64) *events.KafkaRecord {
	headers := []map[string][]byte{}
	addHeaderToHeaderMap(&headers, "ChannelId", channelId)
	addHeaderToHeaderMap(&headers, "StartTime", startTime)
	addHeaderToHeaderMap(&headers, "EndTime", endTime)

	kafkaRecord := &events.KafkaRecord{
		Headers: headers,
	}

	kafkaRecord.Key = showId
	kafkaRecord.Value = guideCell
	kafkaRecord.Offset = offset
	kafkaRecord.Topic = TOPIC
	kafkaRecord.Partition = PARTITION

	return kafkaRecord
}

func (s *TestSuite) TestHandleKafkaEvent() {
	var headersAndRecords = map[string]*events.KafkaRecord{
		"Key": createKafkaRecord(RECORD_KEY, RECORD_VALUE, &gChannelId, &gCurrentUnixTime, &gCurrentUnixTime, OFFSET),
	}

	for testName, kafkaRecord := range headersAndRecords {
		s.T().Run(testName, func(t *testing.T) {
			kafkaRecordsMap := map[string][]events.KafkaRecord{
				fmt.Sprintf("%s-%d", kafkaRecord.Topic, kafkaRecord.Partition): {*kafkaRecord},
			}

			assert.NoError(t, handleKafkaEvent(&events.KafkaEvent{
				Records: kafkaRecordsMap,
			}))
		})
	}
}

func (s *TestSuite) TestColdStart() {
	err := coldStart()

	assert.NoError(s.T(), err)
}
