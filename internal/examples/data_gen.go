package examples

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2"
)

const DefaultSeed int64 = 42

type PeopleRow struct {
	ID      int64
	Name    string
	Age     int64
	Country string
}

type DataFiles struct {
	CSVPath     string
	ParquetPath string
	Rows        int
	Seed        int64
}

func GeneratePeopleFiles(rows int, seed int64, outDir string) (DataFiles, error) {
	if rows <= 0 {
		return DataFiles{}, fmt.Errorf("rows must be greater than zero")
	}
	if strings.TrimSpace(outDir) == "" {
		return DataFiles{}, fmt.Errorf("output directory is required")
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return DataFiles{}, fmt.Errorf("create output directory: %w", err)
	}

	data := GeneratePeopleData(rows, seed)
	csvPath := filepath.Join(outDir, "people.csv")
	if err := writePeopleCSV(csvPath, data); err != nil {
		return DataFiles{}, err
	}

	parquetPath := filepath.Join(outDir, "people.parquet")
	if err := writePeopleParquet(parquetPath, data); err != nil {
		return DataFiles{}, err
	}

	return DataFiles{CSVPath: csvPath, ParquetPath: parquetPath, Rows: rows, Seed: seed}, nil
}

func GeneratePeopleData(rows int, seed int64) []PeopleRow {
	rng := rand.New(rand.NewSource(seed))
	firstNames := []string{"Ada", "Grace", "Linus", "Ken", "Barbara", "James", "Edsger", "Donald", "Margaret", "Guido"}
	lastNames := []string{"Lovelace", "Hopper", "Torvalds", "Thompson", "Liskov", "Gosling", "Dijkstra", "Knuth", "Hamilton", "Rossum"}
	countries := []string{"US", "BR", "DE", "IN", "JP", "CA", "PT", "FR"}

	result := make([]PeopleRow, 0, rows)
	for i := 0; i < rows; i++ {
		name := firstNames[rng.Intn(len(firstNames))] + " " + lastNames[rng.Intn(len(lastNames))]
		row := PeopleRow{
			ID:      int64(i + 1),
			Name:    name,
			Age:     int64(18 + rng.Intn(53)),
			Country: countries[rng.Intn(len(countries))],
		}
		result = append(result, row)
	}

	return result
}

func writePeopleCSV(path string, rows []PeopleRow) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create csv file: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"id", "name", "age", "country"}); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}

	for _, row := range rows {
		rec := []string{fmt.Sprintf("%d", row.ID), row.Name, fmt.Sprintf("%d", row.Age), row.Country}
		if err := w.Write(rec); err != nil {
			return fmt.Errorf("write csv row: %w", err)
		}
	}

	if err := w.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}

	return nil
}

func writePeopleParquet(path string, rows []PeopleRow) error {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return fmt.Errorf("open duckdb: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec("DROP TABLE IF EXISTS people_stage"); err != nil {
		return fmt.Errorf("drop staging table: %w", err)
	}
	if _, err := db.Exec("CREATE TABLE people_stage (id BIGINT, name VARCHAR, age BIGINT, country VARCHAR)"); err != nil {
		return fmt.Errorf("create staging table: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin parquet tx: %w", err)
	}

	stmt, err := tx.Prepare("INSERT INTO people_stage (id, name, age, country) VALUES (?, ?, ?, ?)")
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare parquet insert: %w", err)
	}

	for _, row := range rows {
		if _, err := stmt.Exec(row.ID, row.Name, row.Age, row.Country); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			return fmt.Errorf("insert parquet staging row: %w", err)
		}
	}

	if err := stmt.Close(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("close parquet insert statement: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit parquet tx: %w", err)
	}

	if _, err := db.Exec("COPY people_stage TO " + sqlStringLiteral(path) + " (FORMAT PARQUET)"); err != nil {
		return fmt.Errorf("write parquet file: %w", err)
	}

	return nil
}

func sqlStringLiteral(value string) string {
	escaped := strings.ReplaceAll(value, "'", "''")
	return "'" + escaped + "'"
}
