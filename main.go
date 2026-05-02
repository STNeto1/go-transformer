package main

import (
	"database/sql"
	"errors"
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

	var (
		id   int
		name string
	)
	row := db.QueryRow(selectStmtSQL)
	err = row.Scan(&id, &name)
	if errors.Is(err, sql.ErrNoRows) {
		log.Println("no rows")
	} else if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("id: %d, name: %s\n", id, name)
}
