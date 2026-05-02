package sqlspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type SortDirection string

const (
	SortDirectionAsc  SortDirection = "asc"
	SortDirectionDesc SortDirection = "desc"
)

type NullsOrder string

const (
	NullsOrderFirst NullsOrder = "first"
	NullsOrderLast  NullsOrder = "last"
)

type SortKey struct {
	Column    string
	Direction SortDirection
	Nulls     *NullsOrder
}

func DeriveSort(base TableSpec, derivedTableName string, keys []SortKey) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if len(keys) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least one sort key is required")
	}

	cols := make([]ColumnSpec, len(base.Columns))
	copy(cols, base.Columns)

	orderBy := make([]ast.OrderByExpression, 0, len(keys))
	selectedCols := make([]string, 0, len(keys))

	for _, key := range keys {
		if key.Column == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("sort column is required")
		}

		idx := slices.IndexFunc(cols, func(col ColumnSpec) bool {
			return strings.EqualFold(col.Name, key.Column)
		})
		if idx == -1 {
			return ResolvedBranch{}, nil, fmt.Errorf("column does not exist")
		}

		canonicalCol := cols[idx].Name
		if slices.IndexFunc(selectedCols, func(name string) bool {
			return strings.EqualFold(name, canonicalCol)
		}) >= 0 {
			return ResolvedBranch{}, nil, fmt.Errorf("duplicate sort column %q", key.Column)
		}
		selectedCols = append(selectedCols, canonicalCol)

		ascending := true
		switch key.Direction {
		case "", SortDirectionAsc:
			ascending = true
		case SortDirectionDesc:
			ascending = false
		default:
			return ResolvedBranch{}, nil, fmt.Errorf("unsupported sort direction %q", key.Direction)
		}

		var nullsFirst *bool
		if key.Nulls != nil {
			nf := true
			switch *key.Nulls {
			case NullsOrderFirst:
				nf = true
			case NullsOrderLast:
				nf = false
			default:
				return ResolvedBranch{}, nil, fmt.Errorf("unsupported nulls order %q", *key.Nulls)
			}
			nullsFirst = &nf
		}

		orderBy = append(orderBy, ast.OrderByExpression{
			Expression: &ast.Identifier{Name: canonicalCol},
			Ascending:  ascending,
			NullsFirst: nullsFirst,
		})
	}

	resolved := ResolvedBranch{
		Table: TableSpec{
			Name:    derivedTableName,
			Columns: cols,
		},
	}

	selectFn := func(selectAst *ast.SelectStatement) {
		selectAst.OrderBy = orderBy
	}

	stmt, err := BuildBackfillInsert(base.Name, resolved, selectFn)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	return resolved, stmt, nil
}
