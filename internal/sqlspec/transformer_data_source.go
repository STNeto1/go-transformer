package sqlspec

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type DataSourceFormat string

const (
	DataSourceFormatCSV     DataSourceFormat = "csv"
	DataSourceFormatParquet DataSourceFormat = "parquet"
)

type DataSourceMode string

const (
	DataSourceModeInfer             DataSourceMode = "infer"
	DataSourceModeDeclared          DataSourceMode = "declared"
	DataSourceModeDeclaredWithCheck DataSourceMode = "declared_with_check"
)

type DataSourceSpec struct {
	Format    DataSourceFormat
	Path      string
	Mode      DataSourceMode
	Columns   []ColumnSpec
	CSVHeader *bool
}

func DeriveDataSource(derivedTableName string, spec DataSourceSpec, db *sql.DB) (ResolvedBranch, *ast.InsertStatement, error) {
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if strings.TrimSpace(spec.Path) == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("source path is required")
	}

	format := DataSourceFormat(strings.ToLower(string(spec.Format)))
	if format != DataSourceFormatCSV && format != DataSourceFormatParquet {
		return ResolvedBranch{}, nil, fmt.Errorf("unsupported data source format %q", spec.Format)
	}
	if format == DataSourceFormatParquet && spec.CSVHeader != nil {
		return ResolvedBranch{}, nil, fmt.Errorf("csv header option is only supported for csv format")
	}

	mode := DataSourceMode(strings.ToLower(string(spec.Mode)))
	if mode == "" {
		mode = DataSourceModeInfer
	}
	if mode != DataSourceModeInfer && mode != DataSourceModeDeclared && mode != DataSourceModeDeclaredWithCheck {
		return ResolvedBranch{}, nil, fmt.Errorf("unsupported data source mode %q", spec.Mode)
	}

	if mode == DataSourceModeDeclared || mode == DataSourceModeDeclaredWithCheck {
		if len(spec.Columns) == 0 {
			return ResolvedBranch{}, nil, fmt.Errorf("declared mode requires at least one column")
		}
		if err := validateDeclaredColumns(spec.Columns); err != nil {
			return ResolvedBranch{}, nil, err
		}
	}

	relation := buildDataSourceRelation(spec)

	var inferred []ColumnSpec
	if mode == DataSourceModeInfer || mode == DataSourceModeDeclaredWithCheck {
		if db == nil {
			return ResolvedBranch{}, nil, fmt.Errorf("database connection is required for infer mode")
		}
		var err error
		inferred, err = inferDataSourceColumns(db, relation)
		if err != nil {
			return ResolvedBranch{}, nil, err
		}
	}

	resolvedCols := inferred
	if mode == DataSourceModeDeclared || mode == DataSourceModeDeclaredWithCheck {
		resolvedCols = copyColumns(spec.Columns)
	}

	if mode == DataSourceModeDeclaredWithCheck {
		if err := validateDeclaredAgainstInferredStrict(spec.Columns, inferred); err != nil {
			return ResolvedBranch{}, nil, err
		}
	}

	resolved := ResolvedBranch{Table: TableSpec{Name: derivedTableName, Columns: resolvedCols}}

	insertCols := make([]ast.Expression, 0, len(resolvedCols))
	selectCols := make([]ast.Expression, 0, len(resolvedCols))
	for i := range resolvedCols {
		insertCols = append(insertCols, &ast.Identifier{Name: resolvedCols[i].Name})

		sourceName := resolvedCols[i].Name
		if len(inferred) == len(resolvedCols) {
			sourceName = inferred[i].Name
		}

		sourceExpr := ast.Expression(&ast.Identifier{Name: sourceName})
		if mode == DataSourceModeDeclared || mode == DataSourceModeDeclaredWithCheck {
			sourceExpr = &ast.CastExpression{Expr: sourceExpr, Type: resolvedCols[i].SQLType, Try: false}
		}
		selectCols = append(selectCols, sourceExpr)
	}

	query := &ast.SelectStatement{Columns: selectCols, From: []ast.TableReference{{Name: relation}}}
	stmt := &ast.InsertStatement{TableName: derivedTableName, Columns: insertCols, Query: query}
	return resolved, stmt, nil
}

func validateDeclaredColumns(columns []ColumnSpec) error {
	seen := map[string]struct{}{}
	for i, c := range columns {
		if strings.TrimSpace(c.Name) == "" {
			return fmt.Errorf("declared column %d requires name", i)
		}
		if strings.TrimSpace(c.SQLType) == "" {
			return fmt.Errorf("declared column %q requires SQL type", c.Name)
		}
		k := strings.ToLower(c.Name)
		if _, ok := seen[k]; ok {
			return fmt.Errorf("duplicate declared column %q", c.Name)
		}
		seen[k] = struct{}{}
	}
	return nil
}

func inferDataSourceColumns(db *sql.DB, relation string) ([]ColumnSpec, error) {
	rows, err := db.Query("SELECT * FROM " + relation + " LIMIT 0")
	if err != nil {
		return nil, fmt.Errorf("infer source schema: %w", err)
	}
	defer rows.Close()

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("read inferred source schema: %w", err)
	}
	if len(colTypes) == 0 {
		return nil, fmt.Errorf("inferred source has no columns")
	}

	cols := make([]ColumnSpec, 0, len(colTypes))
	for _, ct := range colTypes {
		sqlType := normalizeSQLTypeName(ct.DatabaseTypeName())
		if sqlType == "" {
			sqlType = "VARCHAR"
		}
		cols = append(cols, ColumnSpec{Name: ct.Name(), SQLType: sqlType})
	}

	return cols, nil
}

func validateDeclaredAgainstInferredStrict(declared []ColumnSpec, inferred []ColumnSpec) error {
	if len(declared) != len(inferred) {
		return fmt.Errorf("declared/inferred column count mismatch: expected %d, got %d", len(declared), len(inferred))
	}

	for i := range declared {
		d := declared[i]
		in := inferred[i]
		if !strings.EqualFold(d.Name, in.Name) {
			return fmt.Errorf("declared/inferred column %d name mismatch: expected %q, got %q", i, d.Name, in.Name)
		}
		if !strings.EqualFold(normalizeSQLTypeName(d.SQLType), normalizeSQLTypeName(in.SQLType)) {
			return fmt.Errorf("declared/inferred column %d type mismatch for %q: expected %q, got %q", i, d.Name, d.SQLType, in.SQLType)
		}
	}

	return nil
}

func buildDataSourceRelation(spec DataSourceSpec) string {
	pathLiteral := sqlStringLiteral(spec.Path)
	if DataSourceFormat(strings.ToLower(string(spec.Format))) == DataSourceFormatParquet {
		return "read_parquet(" + pathLiteral + ")"
	}

	header := true
	if spec.CSVHeader != nil {
		header = *spec.CSVHeader
	}
	headerLiteral := "false"
	if header {
		headerLiteral = "true"
	}

	return "read_csv_auto(" + pathLiteral + ", header=" + headerLiteral + ")"
}

func sqlStringLiteral(value string) string {
	escaped := strings.ReplaceAll(value, "'", "''")
	return "'" + escaped + "'"
}

func copyColumns(columns []ColumnSpec) []ColumnSpec {
	out := make([]ColumnSpec, len(columns))
	copy(out, columns)
	return out
}

func normalizeSQLTypeName(sqlType string) string {
	t := strings.TrimSpace(strings.ToUpper(sqlType))
	t = strings.ReplaceAll(t, " ", "")
	t = strings.ReplaceAll(t, "_", "")
	switch t {
	case "INT", "INTEGER", "INT4", "SIGNED":
		return "INTEGER"
	case "BIGINT", "INT8", "LONG":
		return "BIGINT"
	case "SMALLINT", "INT2", "SHORT":
		return "SMALLINT"
	case "TINYINT":
		return "TINYINT"
	case "DOUBLE", "FLOAT8", "DOUBLEPRECISION":
		return "DOUBLE"
	case "FLOAT", "REAL", "FLOAT4":
		return "FLOAT"
	case "DECIMAL", "NUMERIC":
		return "DECIMAL"
	case "BOOLEAN", "BOOL":
		return "BOOLEAN"
	case "VARCHAR", "CHAR", "CHARACTER", "STRING", "TEXT":
		return "VARCHAR"
	case "DATE":
		return "DATE"
	case "TIMESTAMP", "DATETIME", "TIMESTAMPWITHOUTTIMEZONE":
		return "TIMESTAMP"
	case "TIMESTAMPWITHTIMEZONE", "TIMESTAMPTZ":
		return "TIMESTAMPTZ"
	default:
		return t
	}
}
