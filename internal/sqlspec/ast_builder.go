package sqlspec

import (
	"fmt"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

func BuildCreateTable(spec TableSpec) (*ast.CreateTableStatement, error) {
	if spec.Name == "" {
		return nil, fmt.Errorf("table name is required")
	}
	if len(spec.Columns) == 0 {
		return nil, fmt.Errorf("at least one column is required")
	}

	columns := make([]ast.ColumnDef, 0, len(spec.Columns))
	for _, c := range spec.Columns {
		if c.Name == "" {
			return nil, fmt.Errorf("column name is required")
		}
		if c.SQLType == "" {
			return nil, fmt.Errorf("column type is required for %q", c.Name)
		}
		columns = append(columns, ast.ColumnDef{Name: c.Name, Type: c.SQLType})
	}

	return &ast.CreateTableStatement{
		Name:    spec.Name,
		Columns: columns,
	}, nil
}

func BuildInsert(spec InsertSpec) (*ast.InsertStatement, error) {
	if spec.Table == "" {
		return nil, fmt.Errorf("insert table is required")
	}
	if len(spec.Rows) == 0 {
		return nil, fmt.Errorf("at least one insert row is required")
	}

	columns := spec.Columns
	if len(columns) == 0 {
		for col := range spec.Rows[0] {
			columns = append(columns, col)
		}
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("insert columns could not be inferred")
	}

	astColumns := make([]ast.Expression, 0, len(columns))
	for _, col := range columns {
		if col == "" {
			return nil, fmt.Errorf("insert column name is required")
		}
		astColumns = append(astColumns, &ast.Identifier{Name: col})
	}

	values := make([][]ast.Expression, 0, len(spec.Rows))
	for rowIdx, row := range spec.Rows {
		rowValues := make([]ast.Expression, 0, len(columns))
		for _, col := range columns {
			v, ok := row[col]
			if !ok {
				return nil, fmt.Errorf("row %d is missing column %q", rowIdx, col)
			}
			expr := ToExpression(v)
			if expr == nil {
				return nil, fmt.Errorf("row %d column %q has unsupported value type %T", rowIdx, col, v)
			}
			rowValues = append(rowValues, expr)
		}
		values = append(values, rowValues)
	}

	return &ast.InsertStatement{
		TableName: spec.Table,
		Columns:   astColumns,
		Values:    values,
	}, nil
}

func BuildSelect(spec SelectSpec) (*ast.SelectStatement, error) {
	if spec.Table == "" {
		return nil, fmt.Errorf("select table is required")
	}
	if len(spec.Columns) == 0 {
		return nil, fmt.Errorf("at least one select column is required")
	}

	columns := make([]ast.Expression, 0, len(spec.Columns))
	for _, c := range spec.Columns {
		if c == "" {
			return nil, fmt.Errorf("select column name is required")
		}
		columns = append(columns, &ast.Identifier{Name: c})
	}

	return &ast.SelectStatement{
		Columns: columns,
		From:    []ast.TableReference{{Name: spec.Table}},
	}, nil
}

func ToExpression(v any) ast.Expression {
	switch typed := v.(type) {
	case nil:
		return &ast.LiteralValue{Value: nil, Type: "NULL"}
	case string:
		return &ast.LiteralValue{Value: typed, Type: "STRING"}
	case bool:
		return &ast.LiteralValue{Value: typed, Type: "BOOLEAN"}
	case int:
		return &ast.LiteralValue{Value: typed, Type: "INTEGER"}
	case int8:
		return &ast.LiteralValue{Value: typed, Type: "INTEGER"}
	case int16:
		return &ast.LiteralValue{Value: typed, Type: "INTEGER"}
	case int32:
		return &ast.LiteralValue{Value: typed, Type: "INTEGER"}
	case int64:
		return &ast.LiteralValue{Value: typed, Type: "INTEGER"}
	case uint:
		return &ast.LiteralValue{Value: typed, Type: "INTEGER"}
	case uint8:
		return &ast.LiteralValue{Value: typed, Type: "INTEGER"}
	case uint16:
		return &ast.LiteralValue{Value: typed, Type: "INTEGER"}
	case uint32:
		return &ast.LiteralValue{Value: typed, Type: "INTEGER"}
	case uint64:
		return &ast.LiteralValue{Value: typed, Type: "INTEGER"}
	case float32:
		return &ast.LiteralValue{Value: typed, Type: "FLOAT"}
	case float64:
		return &ast.LiteralValue{Value: typed, Type: "FLOAT"}
	case fmt.Stringer:
		return &ast.LiteralValue{Value: typed.String(), Type: "STRING"}
	default:
		if strings.EqualFold(fmt.Sprintf("%T", v), "json.Number") {
			return &ast.LiteralValue{Value: fmt.Sprintf("%v", v), Type: "STRING"}
		}
		return nil
	}
}
