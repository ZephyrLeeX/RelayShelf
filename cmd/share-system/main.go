package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/relayshelf/relay-shelf/internal/httpapi"
)

type server struct{}

func (server) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(httpapi.HealthResponse{Status: "ok"})
}

func main() {
	router := chi.NewRouter()
	httpapi.HandlerFromMux(server{}, router)

	address := os.Getenv("LISTEN_ADDR")
	if address == "" {
		address = ":8080"
	}
	log.Printf("share-system listening on %s", address)
	log.Fatal(http.ListenAndServe(address, router))
}
