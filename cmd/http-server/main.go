package main

import (
	"flag"
	"log"

	"github.com/joho/godotenv"

	"go-transformer/internal/appdb"
	"go-transformer/internal/httpapi"
	"go-transformer/internal/inputfiles"
	"go-transformer/internal/storage"
)

func main() {
	_ = godotenv.Load()

	addr := flag.String("addr", ":8888", "HTTP listen address")
	flag.Parse()

	db, err := appdb.OpenFromEnv()
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	storageClient, err := storage.NewFromEnv()
	if err != nil {
		log.Fatalf("open rustfs client: %v", err)
	}

	h := httpapi.NewServer(
		httpapi.Config{Addr: *addr},
		httpapi.Deps{InputFiles: inputfiles.NewService(db, storageClient)},
	)

	log.Printf("serving HTTP on %s", *addr)
	h.Spin()
}
