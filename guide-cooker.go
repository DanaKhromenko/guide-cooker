package main

import (
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

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
