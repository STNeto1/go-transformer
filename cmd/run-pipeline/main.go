package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/joho/godotenv"

	"go-transformer/internal/duckdbx"
	"go-transformer/internal/pipeline"
	"go-transformer/internal/workflow"
)

func main() {
	_ = godotenv.Load()

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

	db, err := sql.Open("duckdb", ":memory:")
	// db, err := sql.Open("duckdb", "foo.ddb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := duckdbx.ConfigureS3FromEnv(db); err != nil {
		log.Fatalf("configure s3: %v", err)
	}

	results, err := workflow.Run(db, spec)
	if err != nil {
		log.Fatalf("workflow execution failed: %v", err)
	}

	for _, r := range results {
		if r.TargetPath != "" {
			fmt.Printf("SINK node_id=%s target_table=%s target_path=%s format=%s source_table=%s rows=%d\n", r.NodeID, r.TargetTable, r.TargetPath, r.Format, r.SourceTable, r.RowCount)
			continue
		}
		fmt.Printf("SINK node_id=%s target_table=%s source_table=%s rows=%d\n", r.NodeID, r.TargetTable, r.SourceTable, r.RowCount)
	}
}
