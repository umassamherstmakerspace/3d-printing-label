package main

import (
	b64 "encoding/base64"
	"encoding/json"
	"log"
	"os"

	"crypto/hmac"
	"crypto/sha256"

	"github.com/go-playground/validator/v10"
	val "github.com/go-playground/validator/v10/non-standard/validators"
	"github.com/redis/go-redis/v9"

	"github.com/gofiber/fiber/v2"

	"github.com/adjust/rmq/v5"

	"github.com/joho/godotenv"
	models "mkr.cx/3d-printing-label/src/common"
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
		log.Println("No .env file found")
	}

	hmac_secret := []byte(os.Getenv("HMAC_SECRET"))
	rmq_url := os.Getenv("RMQ_URL")
	rmq_password := os.Getenv("RMQ_PASSWORD")
	rmq_tag := os.Getenv("RMQ_TAG")
	rmq_queue := os.Getenv("RMQ_QUEUE")

	redis_client := redis.NewClient(&redis.Options{
		Addr:     rmq_url,
		Password: rmq_password,
		DB:       1,
	})

	rmq_con, err := rmq.OpenConnectionWithRedisClient(rmq_tag, redis_client, nil)
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

	app := fiber.New()

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("Welcome to the 3D Printing Label Server!")
	})

	app.Get("/print", func(c *fiber.Ctx) error {
		var request struct {
			Data      string `json:"data" validate:"required,notblank"`
			Signature string `json:"signature" validate:"required,notblank"`
		}

		if err := c.BodyParser(&request); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": err.Error(),
			})
		}

		{
			errors := ValidateStruct(request)
			if errors != nil {
				return c.Status(fiber.StatusBadRequest).JSON(errors)
			}
		}

		sig := hmac.New(sha256.New, hmac_secret)
		sig.Write([]byte(request.Data))
		expected := sig.Sum(nil)

		decoded_sig, err := b64.StdEncoding.DecodeString(request.Signature)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": err.Error(),
			})
		}

		if !hmac.Equal(decoded_sig, expected) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": "invalid signature",
			})
		}

		decoded, err := b64.StdEncoding.DecodeString(request.Data)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": err.Error(),
			})
		}

		var data models.Print
		if err := json.Unmarshal(decoded, &data); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": err.Error(),
			})
		}

		{
			errors := ValidateStruct(data)
			if errors != nil {
				return c.Status(fiber.StatusBadRequest).JSON(errors)
			}
		}

		taskQueue.Publish(request.Data)

		return c.SendStatus(fiber.StatusOK)
	})

	app.Listen(":3000")
}
