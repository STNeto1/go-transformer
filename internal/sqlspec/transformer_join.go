package sqlspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type JoinCondition struct {
	LeftColumn  string
	RightColumn string
}

type JoinType string

const (
	JoinTypeInner JoinType = "inner"
	JoinTypeLeft  JoinType = "left"
)

type JoinSelectColumn struct {
	Side   string
	Column string
	As     string
}

type JoinSpec struct {
	Type   JoinType
	On     []JoinCondition
	Select []JoinSelectColumn
}

func DeriveJoinInner(left TableSpec, right TableSpec, derivedTableName string, on []JoinCondition) (ResolvedBranch, *ast.InsertStatement, error) {
	legacySelect := make([]JoinSelectColumn, 0, len(left.Columns)+len(right.Columns))
	for _, c := range left.Columns {
		legacySelect = append(legacySelect, JoinSelectColumn{Side: "left", Column: c.Name, As: c.Name})
	}
	rightOutputCols := uniqueJoinRightColumns(left.Columns, right.Columns)
	for i, c := range right.Columns {
		legacySelect = append(legacySelect, JoinSelectColumn{Side: "right", Column: c.Name, As: rightOutputCols[i].Name})
	}

	return DeriveJoin(left, right, derivedTableName, JoinSpec{
		Type:   JoinTypeInner,
		On:     on,
		Select: legacySelect,
	})
}

func DeriveJoin(left TableSpec, right TableSpec, derivedTableName string, spec JoinSpec) (ResolvedBranch, *ast.InsertStatement, error) {
	if left.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("left table name is required")
	}
	if right.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("right table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if len(left.Columns) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("left table requires columns")
	}
	if len(right.Columns) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("right table requires columns")
	}
	if len(spec.On) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least one join condition is required")
	}
	if len(spec.Select) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least one selected column is required")
	}

	joinType := strings.ToLower(string(spec.Type))
	if joinType == "" {
		joinType = string(JoinTypeInner)
	}
	if joinType != string(JoinTypeInner) && joinType != string(JoinTypeLeft) {
		return ResolvedBranch{}, nil, fmt.Errorf("unsupported join type %q", spec.Type)
	}

	resolvedCols, insertCols, selectCols, err := buildJoinProjection(left.Columns, right.Columns, spec.Select)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	joinCondition, err := buildJoinConditionExpr(left.Columns, right.Columns, spec.On)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	query := &ast.SelectStatement{
		Columns: selectCols,
		From:    []ast.TableReference{{Name: left.Name, Alias: "l"}},
		Joins: []ast.JoinClause{{
			Type:      strings.ToUpper(joinType),
			Left:      ast.TableReference{Name: left.Name, Alias: "l"},
			Right:     ast.TableReference{Name: right.Name, Alias: "r"},
			Condition: joinCondition,
		}},
	}

	resolved := ResolvedBranch{Table: TableSpec{Name: derivedTableName, Columns: resolvedCols}}
	stmt := &ast.InsertStatement{TableName: derivedTableName, Columns: insertCols, Query: query}
	return resolved, stmt, nil
}

func buildJoinProjection(leftCols []ColumnSpec, rightCols []ColumnSpec, selected []JoinSelectColumn) ([]ColumnSpec, []ast.Expression, []ast.Expression, error) {
	resolvedCols := make([]ColumnSpec, 0, len(selected))
	insertCols := make([]ast.Expression, 0, len(selected))
	selectCols := make([]ast.Expression, 0, len(selected))
	seenAlias := map[string]struct{}{}

	for _, sel := range selected {
		if sel.Side == "" || sel.Column == "" || sel.As == "" {
			return nil, nil, nil, fmt.Errorf("join selected column requires side, column and alias")
		}

		side := strings.ToLower(sel.Side)
		cols := leftCols
		prefix := "l."
		if side == "right" {
			cols = rightCols
			prefix = "r."
		} else if side != "left" {
			return nil, nil, nil, fmt.Errorf("unsupported join select side %q", sel.Side)
		}

		idx := slices.IndexFunc(cols, func(c ColumnSpec) bool { return strings.EqualFold(c.Name, sel.Column) })
		if idx == -1 {
			return nil, nil, nil, fmt.Errorf("%s selected column %q does not exist", side, sel.Column)
		}

		aliasKey := strings.ToLower(sel.As)
		if _, exists := seenAlias[aliasKey]; exists {
			return nil, nil, nil, fmt.Errorf("duplicate selected alias %q", sel.As)
		}
		seenAlias[aliasKey] = struct{}{}

		resolvedCols = append(resolvedCols, ColumnSpec{Name: sel.As, SQLType: cols[idx].SQLType})
		insertCols = append(insertCols, &ast.Identifier{Name: sel.As})
		selectCols = append(selectCols, &ast.Identifier{Name: prefix + cols[idx].Name})
	}

	return resolvedCols, insertCols, selectCols, nil
}

func uniqueJoinRightColumns(leftCols []ColumnSpec, rightCols []ColumnSpec) []ColumnSpec {
	used := map[string]struct{}{}
	for _, c := range leftCols {
		used[strings.ToLower(c.Name)] = struct{}{}
	}

	result := make([]ColumnSpec, 0, len(rightCols))
	for _, c := range rightCols {
		name := c.Name
		candidate := name
		idx := 1
		for {
			if _, exists := used[strings.ToLower(candidate)]; !exists {
				break
			}
			if idx == 1 {
				candidate = name + "_right"
			} else {
				candidate = fmt.Sprintf("%s_right_%d", name, idx)
			}
			idx++
		}
		used[strings.ToLower(candidate)] = struct{}{}
		result = append(result, ColumnSpec{Name: candidate, SQLType: c.SQLType})
	}

	return result
}

func buildJoinConditionExpr(leftCols []ColumnSpec, rightCols []ColumnSpec, on []JoinCondition) (ast.Expression, error) {
	seenPairs := map[string]struct{}{}
	var cond ast.Expression

	for _, jc := range on {
		if jc.LeftColumn == "" || jc.RightColumn == "" {
			return nil, fmt.Errorf("join condition requires left and right columns")
		}

		li := slices.IndexFunc(leftCols, func(c ColumnSpec) bool { return strings.EqualFold(c.Name, jc.LeftColumn) })
		if li == -1 {
			return nil, fmt.Errorf("left join column %q does not exist", jc.LeftColumn)
		}
		ri := slices.IndexFunc(rightCols, func(c ColumnSpec) bool { return strings.EqualFold(c.Name, jc.RightColumn) })
		if ri == -1 {
			return nil, fmt.Errorf("right join column %q does not exist", jc.RightColumn)
		}

		lk := leftCols[li].Name
		rk := rightCols[ri].Name
		pairKey := strings.ToLower(lk) + "=" + strings.ToLower(rk)
		if _, ok := seenPairs[pairKey]; ok {
			return nil, fmt.Errorf("duplicate join condition %q = %q", jc.LeftColumn, jc.RightColumn)
		}
		seenPairs[pairKey] = struct{}{}

		eq := &ast.BinaryExpression{
			Left:     &ast.Identifier{Name: "l." + lk},
			Operator: "=",
			Right:    &ast.Identifier{Name: "r." + rk},
		}

		if cond == nil {
			cond = eq
			continue
		}
		cond = &ast.BinaryExpression{Left: cond, Operator: "AND", Right: eq}
	}

	return cond, nil
}
