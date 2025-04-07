package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"time"

	"github.com/gorilla/websocket"

	"github.com/joho/godotenv"

	models "mkr.cx/3d-printing-label/src/common"
)

type ErrorResponse struct {
	FailedField string
	Tag         string
	Value       string
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found")
	}

	ws_host := os.Getenv("WS_HOST")
	ws_password := os.Getenv("WS_PASSWORD")
	label_max_age_s := os.Getenv("LABEL_MAX_AGE")

	label_max_age, err := strconv.ParseInt(label_max_age_s, 10, 64)
	if err != nil {
		panic(err)
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	u := url.URL{Scheme: "ws", Host: ws_host, Path: "/ws"}
	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	done := make(chan struct{})

	fmt.Println(" [*] Waiting for messages. To exit press CTRL+C")

	go func() {
		authenticated := false
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				return
			}
			if !authenticated {
				if bytes.Equal(message, []byte("authenticated")) {
					log.Println("Successfully Authenticated")
					authenticated = true
				}
			} else {
				var data models.WebsocketMessage
				err = json.Unmarshal(message, &data)
				if err != nil {
					fmt.Println(err)
					return
				}

				fmt.Println(" [x] Received", data.ID, data.Timestamp)

				err = c.WriteMessage(websocket.TextMessage, fmt.Appendf(nil, "%v", data.ID))
				if err != nil {
					panic(err)
				}

				fmt.Println(" [x] Acked")

				timestamp := time.Unix(data.Timestamp, 0)

				if timestamp.Before(time.Now().Add(-time.Millisecond * time.Duration(label_max_age))) {
					fmt.Println(" [x] Too old")
					return
				}

				fmt.Println(" [x] Sending to printer")
				temp_file, err := os.CreateTemp("", "label.zpl")
				if err != nil {
					panic(err)
				}

				_, err = temp_file.WriteString(data.Print)
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
			}
		}
	}()

	timer := time.NewTimer(time.Second)

	for {
		select {
		case <-done:
			return
		case <-timer.C:
			err := c.WriteMessage(websocket.TextMessage, []byte(ws_password))
			if err != nil {
				log.Println("write:", err)
				return
			}
		case <-interrupt:
			log.Println("interrupt")

			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("write close:", err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}
