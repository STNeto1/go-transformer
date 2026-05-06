package workflow

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/formatter"
	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"

	"go-transformer/internal/pipeline"
	"go-transformer/internal/sqlspec"
)

type ErrorCode string

const (
	ErrorCodeInvalidArgument      ErrorCode = "INVALID_ARGUMENT"
	ErrorCodeUnsupportedNodeType  ErrorCode = "UNSUPPORTED_NODE_TYPE"
	ErrorCodeDependencyResolution ErrorCode = "DEPENDENCY_RESOLUTION_FAILED"
	ErrorCodeConfigDecode         ErrorCode = "CONFIG_DECODE_FAILED"
	ErrorCodeDerive               ErrorCode = "DERIVE_FAILED"
	ErrorCodeMaterialize          ErrorCode = "MATERIALIZE_FAILED"
	ErrorCodeSinkResolution       ErrorCode = "SINK_RESOLUTION_FAILED"
	ErrorCodeSinkMaterialize      ErrorCode = "SINK_MATERIALIZE_FAILED"
	ErrorCodeSinkCount            ErrorCode = "SINK_COUNT_FAILED"
	ErrorCodeDuplicateSinkTarget  ErrorCode = "DUPLICATE_SINK_TARGET"
	ErrorCodeTableNameCollision   ErrorCode = "TABLE_NAME_COLLISION"
	ErrorCodeGraphOrder           ErrorCode = "GRAPH_ORDER_FAILED"
	ErrorCodeDependencyShape      ErrorCode = "DEPENDENCY_SHAPE_FAILED"
)

type WorkflowError struct {
	Code     ErrorCode
	NodeID   string
	NodeType string
	Stage    string
	Message  string
	Cause    error
}

func (e *WorkflowError) Error() string {
	parts := make([]string, 0, 6)
	if e.Code != "" {
		parts = append(parts, string(e.Code))
	}
	if e.Stage != "" {
		parts = append(parts, "stage="+e.Stage)
	}
	if e.NodeID != "" {
		parts = append(parts, "node_id="+e.NodeID)
	}
	if e.NodeType != "" {
		parts = append(parts, "node_type="+e.NodeType)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Cause != nil {
		parts = append(parts, "cause="+e.Cause.Error())
	}
	return strings.Join(parts, " ")
}

func (e *WorkflowError) Unwrap() error { return e.Cause }

var nonWord = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

type SinkResult struct {
	NodeID      string
	TargetTable string
	SourceTable string
	RowCount    int
}

func Run(db *sql.DB, spec *pipeline.Spec) ([]SinkResult, error) {
	if db == nil {
		return nil, &WorkflowError{Code: ErrorCodeInvalidArgument, Stage: "run", Message: "db is required"}
	}
	if spec == nil {
		return nil, &WorkflowError{Code: ErrorCodeInvalidArgument, Stage: "run", Message: "spec is required"}
	}
	if err := validateSinkTargets(spec.Sinks); err != nil {
		return nil, err
	}

	ordered, depsByNode, err := topologicalOrder(spec)
	if err != nil {
		return nil, &WorkflowError{Code: ErrorCodeGraphOrder, Stage: "topological_order", Message: "failed to compute node execution order", Cause: err}
	}

	state := make(map[string]sqlspec.TableSpec, len(spec.Nodes)*2)
	seenTableNames := make(map[string]string, len(spec.Nodes)*2)

	for _, node := range ordered {
		if err := executeNode(db, spec, node, depsByNode[node.ID], state, seenTableNames); err != nil {
			return nil, err
		}
	}

	results := make([]SinkResult, 0, len(spec.Sinks))
	for _, sink := range spec.Sinks {
		outputRef := normalizeOutputRef(sink.NodeID)
		table, ok := state[outputRef]
		if !ok {
			return nil, &WorkflowError{Code: ErrorCodeSinkResolution, Stage: "resolve_sink", Message: fmt.Sprintf("sink node reference %q was not materialized", sink.NodeID)}
		}
		if err := materializeSink(db, table, sink.TargetTable); err != nil {
			return nil, &WorkflowError{Code: ErrorCodeSinkMaterialize, Stage: "materialize_sink", Message: fmt.Sprintf("failed to materialize sink %q into target table %q", sink.NodeID, sink.TargetTable), Cause: err}
		}
		count, err := countRows(db, sink.TargetTable)
		if err != nil {
			return nil, &WorkflowError{Code: ErrorCodeSinkCount, Stage: "sink_count", Message: fmt.Sprintf("failed counting sink target rows for %q", sink.NodeID), Cause: err}
		}
		results = append(results, SinkResult{NodeID: sink.NodeID, TargetTable: sink.TargetTable, SourceTable: table.Name, RowCount: count})
	}

	return results, nil
}

func executeNode(db *sql.DB, spec *pipeline.Spec, node pipeline.Node, deps []string, state map[string]sqlspec.TableSpec, seenTableNames map[string]string) error {
	baseName := nodeTableName(spec.PipelineID, node.ID)
	registerName := func(tbl string) error {
		if prior, exists := seenTableNames[tbl]; exists {
			return &WorkflowError{Code: ErrorCodeTableNameCollision, NodeID: node.ID, NodeType: node.Type, Stage: "table_name", Message: fmt.Sprintf("table name %q collides with node %q", tbl, prior)}
		}
		seenTableNames[tbl] = node.ID
		return nil
	}

	switch node.Type {
	case pipeline.NodeTypeDataSource:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runDataSourceNode(db, node, baseName, state)
	case pipeline.NodeTypeFilter:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runFilterNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeSelectColumns:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runSelectNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeSort:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runSortNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeLimit, pipeline.NodeTypeLimitSample:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runLimitNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeConstantColumn:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runConstantColumnNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeComputeColumn:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runComputeNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeRenameColumns:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runRenameNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeCastColumns:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runCastNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeFillReplace:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runFillReplaceNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeDeduplicate:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runDeduplicateNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeAggregate:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runAggregateNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeUnnestArray:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runUnnestArrayNode(db, node, baseName, deps, state)
	case pipeline.NodeTypePivot:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runPivotNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeUnpivot:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runUnpivotNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeMergeUnion:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runMergeUnionNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeJoin:
		if err := registerName(baseName); err != nil {
			return err
		}
		return runJoinNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeConditional:
		if err := registerName(baseName + "_if"); err != nil {
			return err
		}
		if err := registerName(baseName + "_else"); err != nil {
			return err
		}
		return runConditionalNode(db, node, baseName, deps, state)
	case pipeline.NodeTypeSwitch:
		labels, err := switchLabels(node)
		if err != nil {
			return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode switch config", err)
		}
		for _, label := range labels {
			if err := registerName(baseName + "_" + sanitizeName(label)); err != nil {
				return err
			}
		}
		if err := registerName(baseName + "_default"); err != nil {
			return err
		}
		return runSwitchNode(db, node, baseName, deps, state)
	default:
		return &WorkflowError{Code: ErrorCodeUnsupportedNodeType, NodeID: node.ID, NodeType: node.Type, Stage: "dispatch", Message: "node type is not supported in current workflow runtime"}
	}
}

func runDataSourceNode(db *sql.DB, node pipeline.Node, tableName string, state map[string]sqlspec.TableSpec) error {
	var cfg pipeline.DataSourceConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode data source config", err)
	}
	resolved, stmt, err := sqlspec.DeriveDataSource(tableName, sqlspec.DataSourceSpec{Format: sqlspec.DataSourceFormat(cfg.Format), Path: cfg.Path, Mode: sqlspec.DataSourceMode(cfg.Mode), Columns: mapColumns(cfg.Columns)}, db)
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive data source SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runFilterNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.FilterConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode filter config", err)
	}
	mode := sqlspec.ConditionalMode(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = sqlspec.ConditionalModeAll
	}
	rules := mapFilterRules(cfg.Rules)
	predicate, err := buildFilterPredicate(upstream.Columns, rules, mode)
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to build filter predicate", err)
	}
	resolved := sqlspec.ResolvedBranch{Table: sqlspec.TableSpec{Name: tableName, Columns: slices.Clone(upstream.Columns)}}
	stmt, err := sqlspec.BuildBackfillInsert(upstream.Name, resolved, func(s *ast.SelectStatement) { s.Where = predicate })
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive filter SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runSelectNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.SelectColumnsConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode select config", err)
	}
	resolved, stmt, err := sqlspec.DeriveSelect(upstream, tableName, cfg.Columns)
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive select SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runSortNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.SortConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode sort config", err)
	}
	keys := make([]sqlspec.SortKey, 0, len(cfg.Keys))
	for _, k := range cfg.Keys {
		var nulls *sqlspec.NullsOrder
		if k.Nulls != "" {
			n := sqlspec.NullsOrder(k.Nulls)
			nulls = &n
		}
		keys = append(keys, sqlspec.SortKey{Column: k.Column, Direction: sqlspec.SortDirection(k.Direction), Nulls: nulls})
	}
	resolved, stmt, err := sqlspec.DeriveSort(upstream, tableName, keys)
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive sort SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runLimitNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.LimitConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode limit config", err)
	}
	resolved, stmt, err := sqlspec.DeriveLimit(upstream, tableName, sqlspec.LimitSpec{Count: cfg.Count, Offset: cfg.Offset})
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive limit SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runConstantColumnNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.ConstantColumnConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode constant column config", err)
	}
	resolved, stmt, err := sqlspec.DeriveConstantColumn(upstream, tableName, sqlspec.ColumnSpec{Name: cfg.Column.Name, SQLType: cfg.Column.SQLType}, cfg.Value)
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive constant column SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runComputeNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.ComputeColumnsConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode compute column config", err)
	}
	computes := make([]sqlspec.ComputeColumn, 0, len(cfg.Columns))
	for _, c := range cfg.Columns {
		computes = append(computes, sqlspec.ComputeColumn{Name: c.Name, SQLType: c.SQLType, Expr: c.Expr})
	}
	resolved, stmt, err := sqlspec.DeriveComputeColumns(upstream, tableName, computes)
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive compute column SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runRenameNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.RenameColumnsConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode rename config", err)
	}
	renames := make([]sqlspec.RenameColumn, 0, len(cfg.Renames))
	for _, r := range cfg.Renames {
		renames = append(renames, sqlspec.RenameColumn{From: r.From, To: r.To})
	}
	resolved, stmt, err := sqlspec.DeriveRenameColumns(upstream, tableName, renames)
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive rename SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runCastNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.CastColumnsConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode cast config", err)
	}
	casts := make([]sqlspec.CastColumn, 0, len(cfg.Casts))
	for _, c := range cfg.Casts {
		casts = append(casts, sqlspec.CastColumn{Column: c.Column, SQLType: c.SQLType})
	}
	resolved, stmt, err := sqlspec.DeriveCastColumns(upstream, tableName, casts)
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive cast SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runFillReplaceNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.FillReplaceConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode fill/replace config", err)
	}
	rules := make([]sqlspec.FillReplaceRule, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		rules = append(rules, sqlspec.FillReplaceRule{Column: r.Column, FillNullWith: r.FillNullWith, ReplaceFrom: r.ReplaceFrom, ReplaceTo: r.ReplaceTo})
	}
	resolved, stmt, err := sqlspec.DeriveFillReplace(upstream, tableName, rules)
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive fill/replace SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runDeduplicateNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.DeduplicateConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode deduplicate config", err)
	}
	orderBy := make([]sqlspec.SortKey, 0, len(cfg.OrderBy))
	for _, k := range cfg.OrderBy {
		var nulls *sqlspec.NullsOrder
		if k.Nulls != "" {
			n := sqlspec.NullsOrder(k.Nulls)
			nulls = &n
		}
		orderBy = append(orderBy, sqlspec.SortKey{Column: k.Column, Direction: sqlspec.SortDirection(k.Direction), Nulls: nulls})
	}
	resolved, stmt, err := sqlspec.DeriveDeduplicate(upstream, tableName, sqlspec.DeduplicateSpec{Columns: cfg.Columns, Keep: sqlspec.DeduplicateKeep(cfg.Keep), OrderBy: orderBy})
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive deduplicate SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runAggregateNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.AggregateConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode aggregate config", err)
	}
	metrics := make([]sqlspec.AggregateMetric, 0, len(cfg.Metrics))
	for _, m := range cfg.Metrics {
		metrics = append(metrics, sqlspec.AggregateMetric{Function: sqlspec.AggregateFunction(m.Function), Column: m.Column, As: m.As})
	}
	resolved, stmt, err := sqlspec.DeriveAggregate(upstream, tableName, sqlspec.AggregateSpec{GroupBy: cfg.GroupBy, Metrics: metrics})
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive aggregate SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runUnnestArrayNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.UnnestArrayConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode unnest config", err)
	}
	resolved, stmt, err := sqlspec.DeriveUnnestArray(upstream, tableName, sqlspec.UnnestArraySpec{ArrayColumn: cfg.ArrayColumn, OutputColumn: sqlspec.ColumnSpec{Name: cfg.OutputColumn.Name, SQLType: cfg.OutputColumn.SQLType}})
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive unnest SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runPivotNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.PivotConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode pivot config", err)
	}
	resolved, stmt, err := sqlspec.DerivePivot(upstream, tableName, sqlspec.PivotSpec{GroupBy: cfg.GroupBy, PivotColumn: cfg.PivotColumn, ValueColumn: cfg.ValueColumn, AggFn: sqlspec.PivotAggregate(cfg.AggFn), InValues: cfg.InValues})
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive pivot SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runUnpivotNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.UnpivotConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode unpivot config", err)
	}
	resolved, stmt, err := sqlspec.DeriveUnpivot(upstream, tableName, sqlspec.UnpivotSpec{Passthrough: cfg.Passthrough, NameColumn: sqlspec.ColumnSpec{Name: cfg.NameColumn.Name, SQLType: cfg.NameColumn.SQLType}, ValueColumn: sqlspec.ColumnSpec{Name: cfg.ValueColumn.Name, SQLType: cfg.ValueColumn.SQLType}, InColumns: cfg.InColumns})
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive unpivot SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runMergeUnionNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	inputs, err := multipleInputs(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.MergeUnionConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode merge union config", err)
	}
	resolved, stmt, err := sqlspec.DeriveMergeUnionWithSpec(inputs, tableName, sqlspec.MergeUnionSpec{Mode: sqlspec.MergeUnionMode(cfg.Mode)})
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive merge union SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runJoinNode(db *sql.DB, node pipeline.Node, tableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	if len(deps) != 2 {
		return &WorkflowError{Code: ErrorCodeDependencyShape, NodeID: node.ID, NodeType: node.Type, Stage: "dispatch", Message: "join requires exactly two inputs"}
	}
	left, ok := state[makeOutputRef(deps[0], "")]
	if !ok {
		return &WorkflowError{Code: ErrorCodeDependencyResolution, NodeID: node.ID, NodeType: node.Type, Stage: "resolve_input", Message: fmt.Sprintf("upstream node %q table not found", deps[0])}
	}
	right, ok := state[makeOutputRef(deps[1], "")]
	if !ok {
		return &WorkflowError{Code: ErrorCodeDependencyResolution, NodeID: node.ID, NodeType: node.Type, Stage: "resolve_input", Message: fmt.Sprintf("upstream node %q table not found", deps[1])}
	}
	var cfg pipeline.JoinConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode join config", err)
	}
	on := make([]sqlspec.JoinCondition, 0, len(cfg.Keys))
	for _, k := range cfg.Keys {
		on = append(on, sqlspec.JoinCondition{LeftColumn: k.Left, RightColumn: k.Right})
	}
	selects := buildDefaultJoinSelect(left.Columns, right.Columns)
	resolved, stmt, err := sqlspec.DeriveJoin(left, right, tableName, sqlspec.JoinSpec{Type: sqlspec.JoinType(cfg.Mode), On: on, Select: selects})
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive join SQL", err)
	}
	if err := materialize(db, resolved.Table, stmt); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize node output", err)
	}
	state[makeOutputRef(node.ID, "")] = resolved.Table
	return nil
}

func runConditionalNode(db *sql.DB, node pipeline.Node, baseTableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.ConditionalConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode conditional config", err)
	}
	result, err := sqlspec.DeriveConditional(upstream, baseTableName+"_if", baseTableName+"_else", sqlspec.ConditionalSpec{Mode: sqlspec.ConditionalMode(cfg.Mode), Rules: mapFilterRules(cfg.Rules)})
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive conditional SQL", err)
	}
	if err := materialize(db, result.IfBranch.Table, result.IfBackfill); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize conditional if branch", err)
	}
	if err := materialize(db, result.ElseBranch.Table, result.ElseBackfill); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize conditional else branch", err)
	}
	state[makeOutputRef(node.ID, "if")] = result.IfBranch.Table
	state[makeOutputRef(node.ID, "else")] = result.ElseBranch.Table
	return nil
}

func runSwitchNode(db *sql.DB, node pipeline.Node, baseTableName string, deps []string, state map[string]sqlspec.TableSpec) error {
	upstream, err := singleInput(node, deps, state)
	if err != nil {
		return err
	}
	var cfg pipeline.SwitchConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return wrapNodeErr(node, ErrorCodeConfigDecode, "decode_config", "failed to decode switch config", err)
	}
	branches := make([]sqlspec.SwitchBranchSpec, 0, len(cfg.Branches))
	for _, b := range cfg.Branches {
		label := strings.TrimSpace(b.Label)
		branches = append(branches, sqlspec.SwitchBranchSpec{
			Label:     label,
			TableName: baseTableName + "_" + sanitizeName(label),
			Mode:      sqlspec.ConditionalMode(b.Mode),
			Rules:     mapFilterRules(b.Rules),
		})
	}
	result, err := sqlspec.DeriveSwitch(upstream, sqlspec.SwitchSpec{Branches: branches, DefaultLabel: "default", DefaultTableName: baseTableName + "_default"})
	if err != nil {
		return wrapNodeErr(node, ErrorCodeDerive, "derive", "failed to derive switch SQL", err)
	}
	for label, branch := range result.Branches {
		stmt := result.Backfills[label]
		if err := materialize(db, branch.Table, stmt); err != nil {
			return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", fmt.Sprintf("failed to materialize switch branch %q", label), err)
		}
		state[makeOutputRef(node.ID, label)] = branch.Table
	}
	if err := materialize(db, result.DefaultBranch.Table, result.DefaultBackfill); err != nil {
		return wrapNodeErr(node, ErrorCodeMaterialize, "materialize", "failed to materialize switch default branch", err)
	}
	state[makeOutputRef(node.ID, "default")] = result.DefaultBranch.Table
	return nil
}

func wrapNodeErr(node pipeline.Node, code ErrorCode, stage string, message string, cause error) error {
	return &WorkflowError{Code: code, NodeID: node.ID, NodeType: node.Type, Stage: stage, Message: message, Cause: cause}
}

func singleInput(node pipeline.Node, deps []string, state map[string]sqlspec.TableSpec) (sqlspec.TableSpec, error) {
	if len(deps) != 1 {
		return sqlspec.TableSpec{}, &WorkflowError{Code: ErrorCodeDependencyShape, NodeID: node.ID, NodeType: node.Type, Stage: "dispatch", Message: "requires exactly one input"}
	}
	upstream, ok := state[makeOutputRef(deps[0], "")]
	if !ok {
		return sqlspec.TableSpec{}, &WorkflowError{Code: ErrorCodeDependencyResolution, NodeID: node.ID, NodeType: node.Type, Stage: "resolve_input", Message: fmt.Sprintf("upstream node %q table not found", deps[0])}
	}
	return upstream, nil
}

func multipleInputs(node pipeline.Node, deps []string, state map[string]sqlspec.TableSpec) ([]sqlspec.TableSpec, error) {
	if len(deps) < 2 {
		return nil, &WorkflowError{Code: ErrorCodeDependencyShape, NodeID: node.ID, NodeType: node.Type, Stage: "dispatch", Message: "requires at least two inputs"}
	}
	inputs := make([]sqlspec.TableSpec, 0, len(deps))
	for _, dep := range deps {
		t, ok := state[makeOutputRef(dep, "")]
		if !ok {
			return nil, &WorkflowError{Code: ErrorCodeDependencyResolution, NodeID: node.ID, NodeType: node.Type, Stage: "resolve_input", Message: fmt.Sprintf("upstream node %q table not found", dep)}
		}
		inputs = append(inputs, t)
	}
	return inputs, nil
}

func mapFilterRules(rules []pipeline.FilterRule) []sqlspec.FilterRow {
	out := make([]sqlspec.FilterRow, 0, len(rules))
	for _, r := range rules {
		out = append(out, sqlspec.FilterRow{Column: r.Column, Operation: sqlspec.FilterOperation(r.Operation), Value: r.Value})
	}
	return out
}

func buildDefaultJoinSelect(left []sqlspec.ColumnSpec, right []sqlspec.ColumnSpec) []sqlspec.JoinSelectColumn {
	selected := make([]sqlspec.JoinSelectColumn, 0, len(left)+len(right))
	used := map[string]struct{}{}
	for _, c := range left {
		selected = append(selected, sqlspec.JoinSelectColumn{Side: "left", Column: c.Name, As: c.Name})
		used[strings.ToLower(c.Name)] = struct{}{}
	}
	for _, c := range right {
		alias := c.Name
		candidate := alias
		i := 1
		for {
			if _, exists := used[strings.ToLower(candidate)]; !exists {
				break
			}
			if i == 1 {
				candidate = alias + "_right"
			} else {
				candidate = fmt.Sprintf("%s_right_%d", alias, i)
			}
			i++
		}
		used[strings.ToLower(candidate)] = struct{}{}
		selected = append(selected, sqlspec.JoinSelectColumn{Side: "right", Column: c.Name, As: candidate})
	}
	return selected
}

func switchLabels(node pipeline.Node) ([]string, error) {
	var cfg pipeline.SwitchConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return nil, err
	}
	labels := make([]string, 0, len(cfg.Branches))
	for _, b := range cfg.Branches {
		labels = append(labels, b.Label)
	}
	return labels, nil
}

func buildFilterPredicate(cols []sqlspec.ColumnSpec, rules []sqlspec.FilterRow, mode sqlspec.ConditionalMode) (ast.Expression, error) {
	if len(rules) == 0 {
		return nil, fmt.Errorf("at least one filter rule is required")
	}

	var pred ast.Expression
	for _, rule := range rules {
		idx := slices.IndexFunc(cols, func(c sqlspec.ColumnSpec) bool { return strings.EqualFold(c.Name, rule.Column) })
		if idx < 0 {
			return nil, fmt.Errorf("column does not exist")
		}
		expr, err := buildFilterExpr(cols[idx], rule)
		if err != nil {
			return nil, err
		}
		if pred == nil {
			pred = expr
			continue
		}
		op := "AND"
		if mode == sqlspec.ConditionalModeAny {
			op = "OR"
		}
		pred = &ast.BinaryExpression{Left: pred, Operator: op, Right: expr}
	}

	return pred, nil
}

func buildFilterExpr(col sqlspec.ColumnSpec, filter sqlspec.FilterRow) (ast.Expression, error) {
	left := &ast.Identifier{Name: col.Name}

	switch filter.Operation {
	case sqlspec.FilterOperationEq, sqlspec.FilterOperationNe, sqlspec.FilterOperationGt, sqlspec.FilterOperationLt:
		rightExpr := sqlspec.ToExpression(filter.Value)
		if rightExpr == nil {
			return nil, fmt.Errorf("unsupported filter value type %T", filter.Value)
		}

		op := "="
		switch filter.Operation {
		case sqlspec.FilterOperationNe:
			op = "!="
		case sqlspec.FilterOperationGt:
			op = ">"
		case sqlspec.FilterOperationLt:
			op = "<"
		}

		return &ast.BinaryExpression{Left: left, Operator: op, Right: rightExpr}, nil

	case sqlspec.FilterOperationContains, sqlspec.FilterOperationStartsWith, sqlspec.FilterOperationEndsWith:
		v, ok := filter.Value.(string)
		if !ok {
			return nil, fmt.Errorf("operation %q requires string value", filter.Operation)
		}

		pattern := v
		switch filter.Operation {
		case sqlspec.FilterOperationContains:
			pattern = "%" + v + "%"
		case sqlspec.FilterOperationStartsWith:
			pattern = v + "%"
		case sqlspec.FilterOperationEndsWith:
			pattern = "%" + v
		}

		return &ast.BinaryExpression{Left: left, Operator: "LIKE", Right: &ast.LiteralValue{Value: pattern, Type: "STRING"}}, nil
	default:
		return nil, fmt.Errorf("unsupported filter operation %q", filter.Operation)
	}
}

func materialize(db *sql.DB, table sqlspec.TableSpec, stmt *ast.InsertStatement) error {
	if _, err := db.Exec("DROP TABLE IF EXISTS " + table.Name); err != nil {
		return err
	}
	createStmt, err := sqlspec.BuildCreateTable(table)
	if err != nil {
		return err
	}
	if _, err := db.Exec(formatter.FormatStatement(createStmt, ast.CompactStyle())); err != nil {
		return err
	}
	if _, err := db.Exec(formatter.FormatStatement(stmt, ast.CompactStyle())); err != nil {
		return err
	}
	return nil
}

func validateSinkTargets(sinks []pipeline.Sink) error {
	seen := make(map[string]string, len(sinks))
	for _, sink := range sinks {
		target := strings.TrimSpace(sink.TargetTable)
		if target == "" {
			continue
		}
		key := strings.ToLower(target)
		if prior, ok := seen[key]; ok {
			return &WorkflowError{Code: ErrorCodeDuplicateSinkTarget, Stage: "validate_sinks", Message: fmt.Sprintf("multiple sinks target table %q", prior)}
		}
		seen[key] = target
	}
	return nil
}

func materializeSink(db *sql.DB, source sqlspec.TableSpec, targetTable string) error {
	target := sqlspec.TableSpec{Name: strings.TrimSpace(targetTable), Columns: source.Columns}
	createStmt, err := sqlspec.BuildCreateTable(target)
	if err != nil {
		return err
	}
	if _, err := db.Exec(formatter.FormatStatement(createStmt, ast.CompactStyle())); err != nil {
		return err
	}
	backfill, err := sqlspec.BuildBackfillInsert(source.Name, sqlspec.ResolvedBranch{Table: target})
	if err != nil {
		return err
	}
	if _, err := db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle())); err != nil {
		return err
	}
	return nil
}

func countRows(db *sql.DB, table string) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
	return count, err
}

func mapColumns(cols []pipeline.ColumnSpec) []sqlspec.ColumnSpec {
	out := make([]sqlspec.ColumnSpec, 0, len(cols))
	for _, c := range cols {
		out = append(out, sqlspec.ColumnSpec{Name: c.Name, SQLType: c.SQLType})
	}
	return out
}

func normalizeOutputRef(ref string) string {
	base, label, err := splitOutputRef(ref)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(ref))
	}
	if label == "" {
		return strings.ToLower(base)
	}
	return strings.ToLower(base) + ":" + strings.ToLower(label)
}

func makeOutputRef(nodeID string, label string) string {
	nodeID = strings.ToLower(strings.TrimSpace(nodeID))
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" {
		return nodeID
	}
	return nodeID + ":" + label
}

func splitOutputRef(ref string) (string, string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", fmt.Errorf("output reference is required")
	}
	parts := strings.Split(ref, ":")
	if len(parts) > 2 {
		return "", "", fmt.Errorf("invalid output reference %q", ref)
	}
	base := strings.TrimSpace(parts[0])
	if base == "" {
		return "", "", fmt.Errorf("node id is required")
	}
	if len(parts) == 1 {
		return base, "", nil
	}
	label := strings.TrimSpace(parts[1])
	if label == "" {
		return "", "", fmt.Errorf("label is required when using node_id:label")
	}
	return base, label, nil
}

func nodeTableName(pipelineID string, nodeID string) string {
	base := "wf_" + sanitizeName(pipelineID) + "_" + sanitizeName(nodeID)
	return strings.ToLower(strings.Trim(base, "_"))
}

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "x"
	}
	s = nonWord.ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

func topologicalOrder(spec *pipeline.Spec) ([]pipeline.Node, map[string][]string, error) {
	nodeByID := make(map[string]pipeline.Node, len(spec.Nodes))
	depsByNode := make(map[string][]string, len(spec.Nodes))
	indegree := make(map[string]int, len(spec.Nodes))
	adj := make(map[string][]string, len(spec.Nodes))

	for _, node := range spec.Nodes {
		nodeByID[node.ID] = node
		indegree[node.ID] = 0
		adj[node.ID] = nil
	}

	for _, node := range spec.Nodes {
		deps, err := extractDeps(node)
		if err != nil {
			return nil, nil, fmt.Errorf("node %s: %w", node.ID, err)
		}
		depsByNode[node.ID] = deps
		for _, dep := range deps {
			if _, ok := nodeByID[dep]; !ok {
				return nil, nil, fmt.Errorf("node %s dependency %q does not exist", node.ID, dep)
			}
			adj[dep] = append(adj[dep], node.ID)
			indegree[node.ID]++
		}
	}

	queue := make([]string, 0, len(spec.Nodes))
	for _, node := range spec.Nodes {
		if indegree[node.ID] == 0 {
			queue = append(queue, node.ID)
		}
	}

	ordered := make([]pipeline.Node, 0, len(spec.Nodes))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		ordered = append(ordered, nodeByID[id])
		for _, next := range adj[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(ordered) != len(spec.Nodes) {
		return nil, nil, fmt.Errorf("cycle detected in pipeline graph")
	}

	return ordered, depsByNode, nil
}

func extractDeps(node pipeline.Node) ([]string, error) {
	if len(node.Inputs) == 0 {
		return nil, nil
	}
	var arr []string
	if err := decodeStrict(node.Inputs, &arr); err == nil {
		return arr, nil
	}
	var obj pipeline.JoinInputs
	if err := decodeStrict(node.Inputs, &obj); err == nil {
		deps := make([]string, 0, 2)
		if obj.Left != "" {
			deps = append(deps, obj.Left)
		}
		if obj.Right != "" {
			deps = append(deps, obj.Right)
		}
		return deps, nil
	}
	return nil, fmt.Errorf("unsupported inputs shape")
}

func decodeStrict(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("unexpected trailing content")
	}
	return nil
}
