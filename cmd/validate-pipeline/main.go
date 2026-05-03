package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"go-transformer/internal/pipeline"
)

func main() {
	filePath := flag.String("file", "", "path to pipeline JSON file")
	flag.Parse()

	if *filePath == "" {
		log.Fatal("missing required --file path/to/pipeline.json")
	}

	data, err := os.ReadFile(*filePath)
	if err != nil {
		log.Fatalf("read pipeline file: %v", err)
	}

	spec, err := pipeline.ParseAndValidateJSON(data)
	if err != nil {
		log.Fatalf("pipeline validation failed: %v", err)
	}

	fmt.Printf("VALID pipeline_id=%s version=%d nodes=%d sinks=%d\n", spec.PipelineID, spec.Version, len(spec.Nodes), len(spec.Sinks))
}
