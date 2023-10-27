package main

import (
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-playground/validator/v10"
	val "github.com/go-playground/validator/v10/non-standard/validators"

	"github.com/adjust/rmq/v5"

	"github.com/joho/godotenv"

	models "mkr.cx/3d-label-printing/src/common"
)

var validate = validator.New()

type ErrorResponse struct {
	FailedField string
	Tag         string
	Value       string
}

func ValidateStruct(s interface{}) []*ErrorResponse {
	var errors []*ErrorResponse
	err := validate.Struct(s)
	if err != nil {
		for _, err := range err.(validator.ValidationErrors) {
			var element ErrorResponse
			element.FailedField = err.StructNamespace()
			element.Tag = err.Tag()
			element.Value = err.Param()
			errors = append(errors, &element)
		}
	}
	return errors
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	rmq_url := os.Getenv("RMQ_URL")
	rmq_tag := os.Getenv("RMQ_TAG")
	rmq_queue := os.Getenv("RMQ_QUEUE")

	rmq_con, err := rmq.OpenConnection(rmq_tag, "tcp", rmq_url, 1, nil)
	if err != nil {
		panic(err)
	}

	taskQueue, err := rmq_con.OpenQueue(rmq_queue)
	if err != nil {
		panic(err)
	}

	err = validate.RegisterValidation("notblank", val.NotBlank)

	if err != nil {
		panic(err)
	}

	err = taskQueue.StartConsuming(10, time.Second)
	if err != nil {
		panic(err)
	}

	fmt.Println(" [*] Waiting for messages. To exit press CTRL+C")

	taskQueue.AddConsumerFunc("Label Printer", func(delivery rmq.Delivery) {
		decoded, err := b64.StdEncoding.DecodeString(delivery.Payload())
		if err != nil {
			fmt.Println(err)
			delivery.Reject()
			return
		}

		var print models.Print
		err = json.Unmarshal(decoded, &print)
		if err != nil {
			fmt.Println(err)
			delivery.Reject()
			return
		}

		if len(ValidateStruct(print)) > 0 {
			fmt.Println("Validation failed")
			delivery.Reject()
			return
		}

		fmt.Println(" [x] Received", print)

		label := print.GenerateLabelZPL()
		err = delivery.Ack()
		if err != nil {
			panic(err)
		}

		fmt.Println(" [x] Acked")

		fmt.Println(" [x] Sending to printer")
		temp_file, err := os.CreateTemp("", "label.zpl")
		if err != nil {
			panic(err)
		}

		_, err = temp_file.WriteString(label)
		if err != nil {
			panic(err)
		}

		err = temp_file.Close()
		if err != nil {
			panic(err)
		}

		err = exec.Command("lp", "-o", "raw", temp_file.Name()).Run()
		if err != nil {
			panic(err)
		}

		fmt.Println(" [x] Done")
	})

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT)
	defer signal.Stop(signals)

	<-signals // wait for signal
	go func() {
		<-signals // hard exit on second signal (in case shutdown gets stuck)
		os.Exit(1)
	}()

	<-rmq_con.StopAllConsuming() // wait for all Consume() calls to finish
}
