package main

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/ajitpratap0/GoSQLX/pkg/formatter"
	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
	_ "github.com/duckdb/duckdb-go/v2"
)

func main() {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	createTableStmt := ast.CreateTableStatement{
		Name: "people",
		Columns: []ast.ColumnDef{
			{
				Name: "id",
				Type: "integer",
			},
			{
				Name: "name",
				Type: "varchar",
			},
		},
	}
	createTableSQL := formatter.FormatStatement(&createTableStmt, ast.CompactStyle())

	insertStmt := ast.InsertStatement{
		TableName: "people",
		Columns: []ast.Expression{
			&ast.Identifier{Name: "id"},
			&ast.Identifier{Name: "name"},
		},
		Values: [][]ast.Expression{
			{
				&ast.LiteralValue{Value: 1, Type: "integer"},
				&ast.LiteralValue{Value: "John", Type: "STRING"},
			},
			{
				&ast.LiteralValue{Value: 2, Type: "integer"},
				&ast.LiteralValue{Value: "Doe", Type: "STRING"},
			},
			{
				&ast.LiteralValue{Value: 3, Type: "integer"},
				&ast.LiteralValue{Value: "Mary", Type: "STRING"},
			},
			{
				&ast.LiteralValue{Value: 4, Type: "integer"},
				&ast.LiteralValue{Value: "Jane", Type: "STRING"},
			},
		},
	}
	insertStmtSQL := formatter.FormatStatement(&insertStmt, ast.CompactStyle())

	selectStmt := ast.SelectStatement{
		Columns: []ast.Expression{
			&ast.Identifier{Name: "id"},
			&ast.Identifier{Name: "name"},
		},
		From: []ast.TableReference{
			{Name: "people"},
		},
	}
	selectStmtSQL := formatter.FormatStatement(&selectStmt, ast.CompactStyle())

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
