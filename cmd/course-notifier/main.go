package main

import (
	"log"
	"net/http"
	"os"

	"github.com/infrai-examples/durable-course-notifications/internal/notifications"
)

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	baseURL := os.Getenv("INFRAI_BASE_URL")
	client := notifications.NewClient(baseURL, key, nil)
	service := notifications.NewService(client, nil)

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("course notification service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, service.Handler()))
}
