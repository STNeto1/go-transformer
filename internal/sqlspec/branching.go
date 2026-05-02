package sqlspec

import (
	"fmt"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

func ResolveBranch(base TableSpec, branch TableBranchSpec) (ResolvedBranch, error) {
	if base.Name == "" {
		return ResolvedBranch{}, fmt.Errorf("base table name is required")
	}
	if branch.TargetTable == "" {
		return ResolvedBranch{}, fmt.Errorf("target table is required")
	}

	cols := make([]ColumnSpec, len(base.Columns))
	copy(cols, base.Columns)
	staticValues := map[string]any{}

	for _, op := range branch.Ops {
		switch op.Type {
		case SchemaOpAddColumn:
			if op.Column.Name == "" || op.Column.SQLType == "" {
				return ResolvedBranch{}, fmt.Errorf("add_column requires column name and SQL type")
			}
			if idxOfColumn(cols, op.Column.Name) >= 0 {
				return ResolvedBranch{}, fmt.Errorf("column %q already exists", op.Column.Name)
			}
			cols = append(cols, op.Column)
			if op.StaticValue != nil {
				staticValues[op.Column.Name] = op.StaticValue
			}
		case SchemaOpDropColumn:
			if op.ColumnName == "" {
				return ResolvedBranch{}, fmt.Errorf("drop_column requires column name")
			}
			idx := idxOfColumn(cols, op.ColumnName)
			if idx < 0 {
				return ResolvedBranch{}, fmt.Errorf("column %q does not exist", op.ColumnName)
			}
			cols = append(cols[:idx], cols[idx+1:]...)
			delete(staticValues, op.ColumnName)
		case SchemaOpRename:
			if op.ColumnName == "" || op.NewName == "" {
				return ResolvedBranch{}, fmt.Errorf("rename_column requires column name and new name")
			}
			idx := idxOfColumn(cols, op.ColumnName)
			if idx < 0 {
				return ResolvedBranch{}, fmt.Errorf("column %q does not exist", op.ColumnName)
			}
			if idxOfColumn(cols, op.NewName) >= 0 {
				return ResolvedBranch{}, fmt.Errorf("column %q already exists", op.NewName)
			}
			cols[idx].Name = op.NewName
			if v, ok := staticValues[op.ColumnName]; ok {
				delete(staticValues, op.ColumnName)
				staticValues[op.NewName] = v
			}
		case SchemaOpChangeType:
			if op.ColumnName == "" || op.NewSQLType == "" {
				return ResolvedBranch{}, fmt.Errorf("change_type requires column name and new SQL type")
			}
			idx := idxOfColumn(cols, op.ColumnName)
			if idx < 0 {
				return ResolvedBranch{}, fmt.Errorf("column %q does not exist", op.ColumnName)
			}
			cols[idx].SQLType = op.NewSQLType
		default:
			return ResolvedBranch{}, fmt.Errorf("unsupported schema op %q", op.Type)
		}
	}

	return ResolvedBranch{
		Table: TableSpec{
			Name:    branch.TargetTable,
			Columns: cols,
		},
		StaticValues: staticValues,
	}, nil
}

func BuildBackfillInsert(sourceTable string, branch ResolvedBranch, opts ...func(*ast.SelectStatement)) (*ast.InsertStatement, error) {
	if sourceTable == "" {
		return nil, fmt.Errorf("source table is required")
	}
	if branch.Table.Name == "" {
		return nil, fmt.Errorf("target table is required")
	}
	if len(branch.Table.Columns) == 0 {
		return nil, fmt.Errorf("target table requires columns")
	}

	insertCols := make([]ast.Expression, 0, len(branch.Table.Columns))
	selectCols := make([]ast.Expression, 0, len(branch.Table.Columns))
	for _, col := range branch.Table.Columns {
		insertCols = append(insertCols, &ast.Identifier{Name: col.Name})
		if v, ok := branch.StaticValues[col.Name]; ok {
			expr := ToExpression(v)
			if expr == nil {
				return nil, fmt.Errorf("unsupported static value type for column %q: %T", col.Name, v)
			}
			selectCols = append(selectCols, expr)
			continue
		}
		selectCols = append(selectCols, &ast.Identifier{Name: col.Name})
	}

	queryStmt := &ast.SelectStatement{
		Columns: selectCols,
		From:    []ast.TableReference{{Name: sourceTable}},
	}

	for _, fn := range opts {
		fn(queryStmt)
	}

	return &ast.InsertStatement{
		TableName: branch.Table.Name,
		Columns:   insertCols,
		Query:     queryStmt,
	}, nil
}

func BuildSelectAll(table TableSpec) (*ast.SelectStatement, error) {
	if table.Name == "" {
		return nil, fmt.Errorf("table name is required")
	}
	if len(table.Columns) == 0 {
		return nil, fmt.Errorf("at least one column is required")
	}

	cols := make([]string, 0, len(table.Columns))
	for _, c := range table.Columns {
		cols = append(cols, c.Name)
	}

	return BuildSelect(SelectSpec{Table: table.Name, Columns: cols})
}

func idxOfColumn(cols []ColumnSpec, name string) int {
	for i, c := range cols {
		if c.Name == name {
			return i
		}
	}
	return -1
}
