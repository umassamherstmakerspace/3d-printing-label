package main

import (
	"bytes"
	"database/sql"
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"crypto/hmac"
	"crypto/sha256"

	"github.com/go-playground/validator/v10"
	val "github.com/go-playground/validator/v10/non-standard/validators"
	"github.com/google/uuid"
	"github.com/nsqio/go-nsq"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"

	"github.com/joho/godotenv"
	models "mkr.cx/3d-printing-label/src/common"
)

type myMessageHandler struct {
	WebSocket *connList
	db        *gorm.DB
}

// HandleMessage implements the Handler interface.
func (h *myMessageHandler) HandleMessage(m *nsq.Message) error {
	if len(m.Body) == 0 {
		// Returning nil will automatically send a FIN command to NSQ to mark the message as processed.
		// In this case, a message with an empty body is simply ignored/discarded.
		return nil
	}

	// do whatever actual message processing is desired
	if h.WebSocket.Count() == 0 {
		return fmt.Errorf("no handlers")
	}

	id, err := strconv.Atoi(string(m.Body))
	if err != nil {
		return nil
	}

	var message models.Message
	message.ID = uint(id)
	message.Printed = false

	if res := h.db.Limit(1).Where(&message).Find(&message); res.Error != nil || res.RowsAffected == 0 {
		// Already printed
		return nil
	}

	ws_message := models.WebsocketMessage{
		ID:        message.ID,
		Print:     message.Print,
		Timestamp: message.CreatedAt.Unix(),
	}

	data, err := json.Marshal(ws_message)
	if err != nil {
		return err
	}

	h.WebSocket.SendAll(websocket.TextMessage, data)

	// Returning a non-nil error will automatically send a REQ command to NSQ to re-queue the message.
	return nil
}

var validate = validator.New()

type ErrorResponse struct {
	FailedField string
	Tag         string
	Value       string
}

func ValidateStruct(s any) []*ErrorResponse {
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

type connList struct {
	mu   sync.Mutex
	conn map[uuid.UUID]*websocket.Conn
}

func (c *connList) Add(connection *websocket.Conn) uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	u := uuid.New()
	c.conn[u] = connection
	return u
}

func (c *connList) Remove(u uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.conn, u)
}

func (c *connList) SendAll(messageType int, message []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var err error
	for _, conn := range c.conn {
		err = conn.WriteMessage(messageType, message)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *connList) DisconnectAll() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var err error
	for _, conn := range c.conn {
		err = conn.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *connList) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.conn)
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found")
	}

	hmac_secret := []byte(os.Getenv("HMAC_SECRET"))
	ws_password := os.Getenv("WS_PASSWORD")

	// Initialize DB
	db, err := gorm.Open(mysql.Open(os.Getenv("DB")), &gorm.Config{})
	if err != nil {
		log.Panicln(err)
	}

	err = db.AutoMigrate(&models.Message{})
	if err != nil {
		log.Panicln(err)
	}

	websocket_conns := connList{
		conn: make(map[uuid.UUID]*websocket.Conn),
	}

	err = validate.RegisterValidation("notblank", val.NotBlank)
	if err != nil {
		panic(err)
	}

	config := nsq.NewConfig()
	producer, err := nsq.NewProducer(os.Getenv("NSQD_HOST"), config)
	if err != nil {
		log.Fatal(err)
	}

	config = nsq.NewConfig()
	config.MaxInFlight = 0
	nsq_consumer, err := nsq.NewConsumer(models.AddTopicName, "websocket", config)
	if err != nil {
		log.Fatal(err)
	}

	nsq_consumer.AddHandler(&myMessageHandler{WebSocket: &websocket_conns, db: db})

	err = nsq_consumer.ConnectToNSQLookupd(os.Getenv("NSQLOOKUP_HOST"))
	if err != nil {
		log.Fatal(err)
	}

	app := fiber.New()

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("Welcome to the 3D Printing Label Server!")
	})

	app.Use("/ws", func(c *fiber.Ctx) error {
		// IsWebSocketUpgrade returns true if the client
		// requested upgrade to the WebSocket protocol.
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		var (
			mt  int
			msg []byte
			err error
		)

		var u uuid.UUID
		authenticated := false

		for {
			mt, msg, err = c.ReadMessage()
			if err != nil {
				break
			}

			if authenticated {
				// Recieve ACKs
				if mt == websocket.TextMessage {
					id, err := strconv.ParseUint(string(msg), 10, 64)
					if err != nil {
						log.Printf("Invalid ack: %s\n", err.Error())
						continue
					}

					log.Printf("Acked: %d\n", id)
					db.Model(&models.Message{}).Where("id = ?", id).Updates(models.Message{PrintedAt: sql.NullTime{Time: time.Now(), Valid: true}, Printed: true})
				}
			} else {
				if mt == websocket.TextMessage {
					if !bytes.Equal(msg, []byte(ws_password)) {
						c.WriteMessage(websocket.TextMessage, []byte("Fail to authenticate"))
						break
					}

					u = websocket_conns.Add(c)
					c.WriteMessage(websocket.TextMessage, []byte("authenticated"))
					authenticated = true
					nsq_consumer.ChangeMaxInFlight(1)
				}
			}
		}

		if authenticated {
			websocket_conns.Remove(u)
			if websocket_conns.Count() == 0 {
				nsq_consumer.ChangeMaxInFlight(0)
			}
		}
	}))

	app.Post("/print", func(c *fiber.Ctx) error {
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

		var print models.Print
		if err := json.Unmarshal(decoded, &print); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"message": err.Error(),
			})
		}

		{
			errors := ValidateStruct(print)
			if errors != nil {
				return c.Status(fiber.StatusBadRequest).JSON(errors)
			}
		}

		var data models.Message
		data.Print = print.GenerateLabelZPL()
		data.Printed = false
		db.Create(&data)
		fmt.Println(data)

		err = producer.Publish(models.AddTopicName, fmt.Appendf(nil, "%v", data.ID))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"message": err.Error(),
			})
		}

		// websocket_conns.SendAll(websocket.TextMessage, res)

		return c.SendStatus(fiber.StatusOK)
	})

	app.Listen(":3000")
	fmt.Println("done")

	producer.Stop()
	nsq_consumer.Stop()
}
