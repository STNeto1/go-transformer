package main

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/ajitpratap0/GoSQLX/pkg/formatter"
	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
	_ "github.com/duckdb/duckdb-go/v2"

	"go-transformer/internal/sqlspec"
)

func main() {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	tableSpec := sqlspec.TableSpec{
		Name: "people",
		Columns: []sqlspec.ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "name", SQLType: "varchar"},
		},
	}
	createTableStmt, err := sqlspec.BuildCreateTable(tableSpec)
	if err != nil {
		log.Fatal(err)
	}
	createTableSQL := formatter.FormatStatement(createTableStmt, ast.CompactStyle())

	insertSpec := sqlspec.InsertSpec{
		Table:   "people",
		Columns: []string{"id", "name"},
		Rows: []map[string]any{
			{"id": 1, "name": "John"},
			{"id": 2, "name": "Doe"},
			{"id": 3, "name": "Mary"},
			{"id": 4, "name": "Jane"},
		},
	}
	insertStmt, err := sqlspec.BuildInsert(insertSpec)
	if err != nil {
		log.Fatal(err)
	}
	insertStmtSQL := formatter.FormatStatement(insertStmt, ast.CompactStyle())

	selectSpec := sqlspec.SelectSpec{
		Table:   "people",
		Columns: []string{"id", "name"},
	}
	selectStmt, err := sqlspec.BuildSelect(selectSpec)
	if err != nil {
		log.Fatal(err)
	}
	selectStmtSQL := formatter.FormatStatement(selectStmt, ast.CompactStyle())

	_, err = db.Exec(createTableSQL)
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(insertStmtSQL)
	if err != nil {
		log.Fatal(err)
	}

	rows, err := db.Query(selectStmtSQL)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		log.Fatal(err)
	}

	hasRows := false
	for rows.Next() {
		hasRows = true
		values := make([]any, len(cols))
		valuePtrs := make([]any, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			log.Fatal(err)
		}

		for i, col := range cols {
			fmt.Printf("%s: %v", col, values[i])
			if i < len(cols)-1 {
				fmt.Print(", ")
			}
		}
		fmt.Println()
	}

	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
	if !hasRows {
		log.Println("no rows")
	}
}
