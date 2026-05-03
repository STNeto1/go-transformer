package main

import (
	"flag"
	"fmt"
	"log"

	"go-transformer/internal/examples"
)

func main() {
	rows := flag.Int("rows", 1000, "number of rows to generate")
	seed := flag.Int64("seed", examples.DefaultSeed, "deterministic random seed")
	outDir := flag.String("out-dir", "data", "output directory for generated files")
	flag.Parse()

	files, err := examples.GeneratePeopleFiles(*rows, *seed, *outDir)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Generated dataset with %d rows (seed=%d)\n", files.Rows, files.Seed)
	fmt.Printf("CSV: %s\n", files.CSVPath)
	fmt.Printf("Parquet: %s\n", files.ParquetPath)
}
