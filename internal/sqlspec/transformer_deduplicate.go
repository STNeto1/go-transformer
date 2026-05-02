package sqlspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type DeduplicateKeep string

const (
	DeduplicateKeepFirst DeduplicateKeep = "first"
	DeduplicateKeepLast  DeduplicateKeep = "last"
)

type DeduplicateSpec struct {
	Columns []string
	Keep    DeduplicateKeep
	OrderBy []SortKey
}

func DeriveDeduplicate(base TableSpec, derivedTableName string, spec DeduplicateSpec) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if len(spec.Columns) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least one deduplicate column is required")
	}
	if len(spec.OrderBy) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least one order by key is required for deduplicate")
	}

	keep := spec.Keep
	if keep == "" {
		keep = DeduplicateKeepFirst
	}
	if keep != DeduplicateKeepFirst && keep != DeduplicateKeepLast {
		return ResolvedBranch{}, nil, fmt.Errorf("unsupported keep mode %q", keep)
	}

	cols := make([]ColumnSpec, len(base.Columns))
	copy(cols, base.Columns)

	distinctOn := make([]ast.Expression, 0, len(spec.Columns))
	dedupIdentifiers := make([]string, 0, len(spec.Columns))
	seenPartition := map[string]struct{}{}
	for _, c := range spec.Columns {
		if c == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("deduplicate column is required")
		}
		idx := slices.IndexFunc(base.Columns, func(col ColumnSpec) bool {
			return strings.EqualFold(col.Name, c)
		})
		if idx == -1 {
			return ResolvedBranch{}, nil, fmt.Errorf("column %q does not exist", c)
		}
		canonical := base.Columns[idx].Name
		key := strings.ToLower(canonical)
		if _, ok := seenPartition[key]; ok {
			return ResolvedBranch{}, nil, fmt.Errorf("duplicate deduplicate column %q", c)
		}
		seenPartition[key] = struct{}{}
		dedupIdentifiers = append(dedupIdentifiers, canonical)
		distinctOn = append(distinctOn, &ast.Identifier{Name: canonical})
	}

	orderBy, err := buildDeduplicateOrderBy(base.Columns, spec.OrderBy, keep)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	resolved := ResolvedBranch{Table: TableSpec{Name: derivedTableName, Columns: cols}}

	selectFn := func(selectAst *ast.SelectStatement) {
		selectAst.DistinctOnColumns = distinctOn
		selectAst.OrderBy = mergeDeduplicateOrderBy(dedupIdentifiers, orderBy)
	}

	stmt, err := BuildBackfillInsert(base.Name, resolved, selectFn)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	return resolved, stmt, nil
}

func mergeDeduplicateOrderBy(dedupColumns []string, keys []ast.OrderByExpression) []ast.OrderByExpression {
	merged := make([]ast.OrderByExpression, 0, len(dedupColumns)+len(keys))
	seen := map[string]struct{}{}

	for _, c := range dedupColumns {
		merged = append(merged, ast.OrderByExpression{Expression: &ast.Identifier{Name: c}, Ascending: true})
		seen[strings.ToLower(c)] = struct{}{}
	}

	for _, key := range keys {
		id, ok := key.Expression.(*ast.Identifier)
		if !ok {
			merged = append(merged, key)
			continue
		}
		k := strings.ToLower(id.Name)
		if _, exists := seen[k]; exists {
			continue
		}
		merged = append(merged, key)
		seen[k] = struct{}{}
	}

	return merged
}

func buildDeduplicateOrderBy(baseCols []ColumnSpec, keys []SortKey, keep DeduplicateKeep) ([]ast.OrderByExpression, error) {
	orderBy := make([]ast.OrderByExpression, 0, len(keys))
	seen := map[string]struct{}{}

	for _, key := range keys {
		if key.Column == "" {
			return nil, fmt.Errorf("order by column is required")
		}

		idx := slices.IndexFunc(baseCols, func(col ColumnSpec) bool {
			return strings.EqualFold(col.Name, key.Column)
		})
		if idx == -1 {
			return nil, fmt.Errorf("column %q does not exist", key.Column)
		}

		canonical := baseCols[idx].Name
		seenKey := strings.ToLower(canonical)
		if _, ok := seen[seenKey]; ok {
			return nil, fmt.Errorf("duplicate order by column %q", key.Column)
		}
		seen[seenKey] = struct{}{}

		ascending := true
		switch key.Direction {
		case "", SortDirectionAsc:
			ascending = true
		case SortDirectionDesc:
			ascending = false
		default:
			return nil, fmt.Errorf("unsupported sort direction %q", key.Direction)
		}

		if keep == DeduplicateKeepLast {
			ascending = !ascending
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
				return nil, fmt.Errorf("unsupported nulls order %q", *key.Nulls)
			}
			if keep == DeduplicateKeepLast {
				nf = !nf
			}
			nullsFirst = &nf
		}

		orderBy = append(orderBy, ast.OrderByExpression{
			Expression: &ast.Identifier{Name: canonical},
			Ascending:  ascending,
			NullsFirst: nullsFirst,
		})
	}

	return orderBy, nil
}
