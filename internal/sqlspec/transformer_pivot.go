package sqlspec

import (
	"fmt"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type PivotAggregate string

const (
	PivotAggregateSum   PivotAggregate = "sum"
	PivotAggregateCount PivotAggregate = "count"
)

type PivotSpec struct {
	GroupBy     []string
	PivotColumn string
	ValueColumn string
	AggFn       PivotAggregate
	InValues    []string
}

func DerivePivot(base TableSpec, derivedTableName string, spec PivotSpec) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if len(base.Columns) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("base table requires columns")
	}
	if len(spec.GroupBy) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least one group by column is required")
	}
	if spec.PivotColumn == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("pivot column is required")
	}
	if spec.ValueColumn == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("value column is required")
	}
	if len(spec.InValues) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least one pivot value is required")
	}

	agg := spec.AggFn
	if agg == "" {
		agg = PivotAggregateSum
	}
	if agg != PivotAggregateSum && agg != PivotAggregateCount {
		return ResolvedBranch{}, nil, fmt.Errorf("unsupported pivot aggregate %q", spec.AggFn)
	}

	baseByName := map[string]ColumnSpec{}
	for _, c := range base.Columns {
		baseByName[strings.ToLower(c.Name)] = c
	}

	pivotCol, ok := baseByName[strings.ToLower(spec.PivotColumn)]
	if !ok {
		return ResolvedBranch{}, nil, fmt.Errorf("pivot column %q does not exist", spec.PivotColumn)
	}
	valueCol, ok := baseByName[strings.ToLower(spec.ValueColumn)]
	if !ok {
		return ResolvedBranch{}, nil, fmt.Errorf("value column %q does not exist", spec.ValueColumn)
	}

	groupCols := make([]ColumnSpec, 0, len(spec.GroupBy))
	groupSeen := map[string]struct{}{}
	for _, c := range spec.GroupBy {
		if c == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("group by column is required")
		}
		bc, ok := baseByName[strings.ToLower(c)]
		if !ok {
			return ResolvedBranch{}, nil, fmt.Errorf("group by column %q does not exist", c)
		}
		k := strings.ToLower(bc.Name)
		if _, exists := groupSeen[k]; exists {
			return ResolvedBranch{}, nil, fmt.Errorf("duplicate group by column %q", c)
		}
		groupSeen[k] = struct{}{}
		if strings.EqualFold(bc.Name, pivotCol.Name) || strings.EqualFold(bc.Name, valueCol.Name) {
			return ResolvedBranch{}, nil, fmt.Errorf("group by column %q cannot be pivot or value column", c)
		}
		groupCols = append(groupCols, bc)
	}

	inSeen := map[string]struct{}{}
	inValues := make([]string, 0, len(spec.InValues))
	for _, v := range spec.InValues {
		if v == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("pivot value is required")
		}
		k := strings.ToLower(v)
		if _, exists := inSeen[k]; exists {
			return ResolvedBranch{}, nil, fmt.Errorf("duplicate pivot value %q", v)
		}
		inSeen[k] = struct{}{}
		inValues = append(inValues, v)
	}

	metricType := valueCol.SQLType
	if agg == PivotAggregateCount {
		metricType = "bigint"
	}

	resolvedCols := make([]ColumnSpec, 0, len(groupCols)+len(inValues))
	resolvedCols = append(resolvedCols, groupCols...)
	for _, v := range inValues {
		resolvedCols = append(resolvedCols, ColumnSpec{Name: v, SQLType: metricType})
	}

	insertCols := make([]ast.Expression, 0, len(resolvedCols))
	selectCols := make([]ast.Expression, 0, len(resolvedCols))
	for _, c := range groupCols {
		insertCols = append(insertCols, &ast.Identifier{Name: c.Name})
		selectCols = append(selectCols, &ast.Identifier{Name: c.Name})
	}
	for _, v := range inValues {
		insertCols = append(insertCols, &ast.Identifier{Name: v})
		selectCols = append(selectCols, &ast.Identifier{Name: v})
	}

	aggExpr := &ast.FunctionCall{
		Name: strings.ToUpper(string(agg)),
		Arguments: []ast.Expression{
			&ast.Identifier{Name: valueCol.Name},
		},
	}

	query := &ast.SelectStatement{
		Columns: selectCols,
		From: []ast.TableReference{{
			Name: base.Name,
			Pivot: &ast.PivotClause{
				AggregateFunction: aggExpr,
				PivotColumn:       pivotCol.Name,
				InValues:          inValues,
			},
		}},
	}

	resolved := ResolvedBranch{Table: TableSpec{Name: derivedTableName, Columns: resolvedCols}}
	stmt := &ast.InsertStatement{TableName: derivedTableName, Columns: insertCols, Query: query}
	return resolved, stmt, nil
}
