package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const (
	NodeTypeDataSource     = "DataSource"
	NodeTypeSelectColumns  = "SelectColumns"
	NodeTypeFilter         = "Filter"
	NodeTypeSort           = "Sort"
	NodeTypeLimit          = "Limit"
	NodeTypeLimitSample    = "LimitSample"
	NodeTypeConstantColumn = "ConstantColumn"
	NodeTypeComputeColumn  = "ComputeColumn"
	NodeTypeRenameColumns  = "RenameColumns"
	NodeTypeCastColumns    = "CastColumns"
	NodeTypeFillReplace    = "FillReplace"
	NodeTypeDeduplicate    = "Deduplicate"
	NodeTypeAggregate      = "Aggregate"
	NodeTypeMergeUnion     = "MergeUnion"
	NodeTypeJoin           = "Join"
	NodeTypeConditional    = "Conditional"
	NodeTypeSwitch         = "Switch"
	NodeTypeUnnestArray    = "UnnestArray"
	NodeTypePivot          = "Pivot"
	NodeTypeUnpivot        = "Unpivot"
)

type ValidationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path"`
}

type ValidationError struct {
	Issues []ValidationIssue
}

func (e *ValidationError) Error() string {
	if len(e.Issues) == 0 {
		return "pipeline validation failed"
	}
	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		parts = append(parts, fmt.Sprintf("%s at %s: %s", issue.Code, issue.Path, issue.Message))
	}
	return strings.Join(parts, "; ")
}

func ParseAndValidateJSON(data []byte) (*Spec, error) {
	if !json.Valid(data) {
		return nil, &ValidationError{Issues: []ValidationIssue{{Code: "INVALID_JSON", Message: "invalid JSON document", Path: "$"}}}
	}

	var spec Spec
	if err := decodeStrict(data, &spec); err != nil {
		return nil, &ValidationError{Issues: []ValidationIssue{{Code: "SCHEMA_INVALID", Message: err.Error(), Path: "$"}}}
	}

	if issues := validateSpec(&spec); len(issues) > 0 {
		return nil, &ValidationError{Issues: issues}
	}

	return &spec, nil
}

func validateSpec(spec *Spec) []ValidationIssue {
	issues := make([]ValidationIssue, 0)

	if strings.TrimSpace(spec.PipelineID) == "" {
		issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "pipeline_id is required", Path: "$.pipeline_id"})
	}
	if spec.Version < 1 {
		issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "version must be >= 1", Path: "$.version"})
	}
	if len(spec.Nodes) == 0 {
		issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "nodes must contain at least one node", Path: "$.nodes"})
		return issues
	}
	if len(spec.Sinks) == 0 {
		issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "sinks must contain at least one sink", Path: "$.sinks"})
	}

	if spec.Defaults.TelemetryFormat != "" && spec.Defaults.TelemetryFormat != "text" && spec.Defaults.TelemetryFormat != "json" {
		issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "telemetry_format must be text or json", Path: "$.defaults.telemetry_format"})
	}
	if spec.Defaults.TelemetryLevel != "" && spec.Defaults.TelemetryLevel != "basic" && spec.Defaults.TelemetryLevel != "debug" {
		issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "telemetry_level must be basic or debug", Path: "$.defaults.telemetry_level"})
	}

	nodeByID := make(map[string]int, len(spec.Nodes))
	depsByNode := make(map[string][]string, len(spec.Nodes))

	for i, node := range spec.Nodes {
		nodePath := fmt.Sprintf("$.nodes[%d]", i)
		if strings.TrimSpace(node.ID) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "node id is required", Path: nodePath + ".id"})
			continue
		}
		if prior, exists := nodeByID[node.ID]; exists {
			issues = append(issues, ValidationIssue{Code: "NODE_DUPLICATE_ID", Message: fmt.Sprintf("duplicate node id %q (already declared at index %d)", node.ID, prior), Path: nodePath + ".id"})
			continue
		}
		nodeByID[node.ID] = i

		nodeIssues, deps := validateNode(node, i)
		issues = append(issues, nodeIssues...)
		depsByNode[node.ID] = deps
	}

	issues = append(issues, validateGraphSoundness(spec, nodeByID, depsByNode)...)
	return issues
}

func validateNode(node Node, idx int) ([]ValidationIssue, []string) {
	issues := make([]ValidationIssue, 0)
	path := fmt.Sprintf("$.nodes[%d]", idx)

	if len(node.Config) == 0 {
		issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "config is required", Path: path + ".config"})
		return issues, nil
	}

	switch node.Type {
	case NodeTypeDataSource:
		if len(node.Inputs) > 0 {
			issues = append(issues, ValidationIssue{Code: "NODE_INPUTS_FORBIDDEN", Message: "DataSource cannot declare inputs", Path: path + ".inputs"})
		}
		var cfg DataSourceConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, nil
		}
		if strings.TrimSpace(cfg.Format) == "" || !slices.Contains([]string{"csv", "parquet"}, cfg.Format) {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "format must be csv or parquet", Path: path + ".config.format"})
		}
		if cfg.Format == "parquet" && cfg.Options.Header != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "options.header is only supported for csv format", Path: path + ".config.options.header"})
		}
		if strings.TrimSpace(cfg.Path) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "path is required", Path: path + ".config.path"})
		}
		if strings.TrimSpace(cfg.Mode) == "" || !slices.Contains([]string{"infer", "declared", "declared_with_check"}, cfg.Mode) {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "mode must be infer, declared, or declared_with_check", Path: path + ".config.mode"})
		}
		if cfg.Mode == "declared" || cfg.Mode == "declared_with_check" {
			if len(cfg.Columns) == 0 {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "columns are required for declared modes", Path: path + ".config.columns"})
			}
		}
		for i, c := range cfg.Columns {
			if strings.TrimSpace(c.Name) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "column name is required", Path: fmt.Sprintf("%s.config.columns[%d].name", path, i)})
			}
			if strings.TrimSpace(c.SQLType) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "column sql_type is required", Path: fmt.Sprintf("%s.config.columns[%d].sql_type", path, i)})
			}
		}
		return issues, nil

	case NodeTypeFilter:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg FilterConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if cfg.Mode != "" && cfg.Mode != "all" && cfg.Mode != "any" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "mode must be all or any", Path: path + ".config.mode"})
		}
		if len(cfg.Rules) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "at least one rule is required", Path: path + ".config.rules"})
		}
		for i, rule := range cfg.Rules {
			if strings.TrimSpace(rule.Column) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "rule column is required", Path: fmt.Sprintf("%s.config.rules[%d].column", path, i)})
			}
			if !slices.Contains([]string{"eq", "ne", "contains", "startsWith", "endsWith", "gt", "lt"}, rule.Operation) {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "unsupported rule operation", Path: fmt.Sprintf("%s.config.rules[%d].operation", path, i)})
			}
		}
		return issues, deps

	case NodeTypeSelectColumns:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg SelectColumnsConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if len(cfg.Columns) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "at least one column is required", Path: path + ".config.columns"})
		}
		for i, c := range cfg.Columns {
			if strings.TrimSpace(c) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "column name is required", Path: fmt.Sprintf("%s.config.columns[%d]", path, i)})
			}
		}
		return issues, deps

	case NodeTypeSort:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg SortConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if len(cfg.Keys) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "at least one sort key is required", Path: path + ".config.keys"})
		}
		for i, key := range cfg.Keys {
			if strings.TrimSpace(key.Column) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "sort key column is required", Path: fmt.Sprintf("%s.config.keys[%d].column", path, i)})
			}
			if key.Direction != "" && !slices.Contains([]string{"asc", "desc"}, key.Direction) {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "direction must be asc or desc", Path: fmt.Sprintf("%s.config.keys[%d].direction", path, i)})
			}
			if key.Nulls != "" && !slices.Contains([]string{"first", "last"}, key.Nulls) {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "nulls must be first or last", Path: fmt.Sprintf("%s.config.keys[%d].nulls", path, i)})
			}
		}
		return issues, deps

	case NodeTypeLimit, NodeTypeLimitSample:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg LimitConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if cfg.Count <= 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "count must be greater than zero", Path: path + ".config.count"})
		}
		if cfg.Offset != nil && *cfg.Offset < 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "offset must be >= 0", Path: path + ".config.offset"})
		}
		return issues, deps

	case NodeTypeConstantColumn:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg ConstantColumnConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if strings.TrimSpace(cfg.Column.Name) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "column name is required", Path: path + ".config.column.name"})
		}
		if strings.TrimSpace(cfg.Column.SQLType) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "column sql_type is required", Path: path + ".config.column.sql_type"})
		}
		return issues, deps

	case NodeTypeComputeColumn:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg ComputeColumnsConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if len(cfg.Columns) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "at least one compute column is required", Path: path + ".config.columns"})
		}
		for i, c := range cfg.Columns {
			if strings.TrimSpace(c.Name) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "compute column name is required", Path: fmt.Sprintf("%s.config.columns[%d].name", path, i)})
			}
			if strings.TrimSpace(c.SQLType) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "compute column sql_type is required", Path: fmt.Sprintf("%s.config.columns[%d].sql_type", path, i)})
			}
			if strings.TrimSpace(c.Expr) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "compute expression is required", Path: fmt.Sprintf("%s.config.columns[%d].expr", path, i)})
			}
		}
		return issues, deps

	case NodeTypeRenameColumns:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg RenameColumnsConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if len(cfg.Renames) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "at least one rename is required", Path: path + ".config.renames"})
		}
		for i, r := range cfg.Renames {
			if strings.TrimSpace(r.From) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "rename from is required", Path: fmt.Sprintf("%s.config.renames[%d].from", path, i)})
			}
			if strings.TrimSpace(r.To) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "rename to is required", Path: fmt.Sprintf("%s.config.renames[%d].to", path, i)})
			}
		}
		return issues, deps

	case NodeTypeCastColumns:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg CastColumnsConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if len(cfg.Casts) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "at least one cast is required", Path: path + ".config.casts"})
		}
		for i, c := range cfg.Casts {
			if strings.TrimSpace(c.Column) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "cast column is required", Path: fmt.Sprintf("%s.config.casts[%d].column", path, i)})
			}
			if strings.TrimSpace(c.SQLType) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "cast sql_type is required", Path: fmt.Sprintf("%s.config.casts[%d].sql_type", path, i)})
			}
		}
		return issues, deps

	case NodeTypeFillReplace:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg FillReplaceConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if len(cfg.Rules) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "at least one fill/replace rule is required", Path: path + ".config.rules"})
		}
		for i, r := range cfg.Rules {
			if strings.TrimSpace(r.Column) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "rule column is required", Path: fmt.Sprintf("%s.config.rules[%d].column", path, i)})
			}
		}
		return issues, deps

	case NodeTypeDeduplicate:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg DeduplicateConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if len(cfg.Columns) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "at least one deduplicate column is required", Path: path + ".config.columns"})
		}
		if cfg.Keep != "" && !slices.Contains([]string{"first", "last"}, cfg.Keep) {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "keep must be first or last", Path: path + ".config.keep"})
		}
		if len(cfg.OrderBy) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "at least one order_by key is required", Path: path + ".config.order_by"})
		}
		return issues, deps

	case NodeTypeAggregate:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg AggregateConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if len(cfg.Metrics) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "at least one metric is required", Path: path + ".config.metrics"})
		}
		for i, m := range cfg.Metrics {
			if !slices.Contains([]string{"count", "sum", "avg", "min", "max"}, m.Function) {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "unsupported metric function", Path: fmt.Sprintf("%s.config.metrics[%d].function", path, i)})
			}
			if strings.TrimSpace(m.Column) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "metric column is required", Path: fmt.Sprintf("%s.config.metrics[%d].column", path, i)})
			}
			if strings.TrimSpace(m.As) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "metric alias is required", Path: fmt.Sprintf("%s.config.metrics[%d].as", path, i)})
			}
		}
		return issues, deps

	case NodeTypeMergeUnion:
		deps, depIssues := parseArrayInputs(node.Inputs, 2, -1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg MergeUnionConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if !slices.Contains([]string{"strict_positional", "align_by_name"}, cfg.Mode) {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "mode must be strict_positional or align_by_name", Path: path + ".config.mode"})
		}
		return issues, deps

	case NodeTypeJoin:
		deps, depIssues := parseJoinInputs(node.Inputs, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg JoinConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if !slices.Contains([]string{"inner", "left"}, cfg.Mode) {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "mode must be inner or left", Path: path + ".config.mode"})
		}
		if len(cfg.Keys) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "at least one join key is required", Path: path + ".config.keys"})
		}
		for i, key := range cfg.Keys {
			if strings.TrimSpace(key.Left) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "join key left is required", Path: fmt.Sprintf("%s.config.keys[%d].left", path, i)})
			}
			if strings.TrimSpace(key.Right) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "join key right is required", Path: fmt.Sprintf("%s.config.keys[%d].right", path, i)})
			}
		}
		return issues, deps

	case NodeTypeConditional:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg ConditionalConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if cfg.Mode != "" && cfg.Mode != "all" && cfg.Mode != "any" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "mode must be all or any", Path: path + ".config.mode"})
		}
		for i, rule := range cfg.Rules {
			if strings.TrimSpace(rule.Column) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "rule column is required", Path: fmt.Sprintf("%s.config.rules[%d].column", path, i)})
			}
			if !slices.Contains([]string{"eq", "ne", "contains", "startsWith", "endsWith", "gt", "lt"}, rule.Operation) {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "unsupported rule operation", Path: fmt.Sprintf("%s.config.rules[%d].operation", path, i)})
			}
		}
		return issues, deps

	case NodeTypeSwitch:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg SwitchConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if len(cfg.Branches) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "at least one branch is required", Path: path + ".config.branches"})
		}
		seenLabels := map[string]struct{}{}
		for i, b := range cfg.Branches {
			if strings.TrimSpace(b.Label) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "branch label is required", Path: fmt.Sprintf("%s.config.branches[%d].label", path, i)})
			}
			if b.Mode != "" && b.Mode != "all" && b.Mode != "any" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "mode must be all or any", Path: fmt.Sprintf("%s.config.branches[%d].mode", path, i)})
			}
			if strings.EqualFold(strings.TrimSpace(b.Label), "default") {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "branch label default is reserved", Path: fmt.Sprintf("%s.config.branches[%d].label", path, i)})
			}
			key := strings.ToLower(strings.TrimSpace(b.Label))
			if key != "" {
				if _, exists := seenLabels[key]; exists {
					issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "duplicate branch label", Path: fmt.Sprintf("%s.config.branches[%d].label", path, i)})
				} else {
					seenLabels[key] = struct{}{}
				}
			}
			for j, rule := range b.Rules {
				if strings.TrimSpace(rule.Column) == "" {
					issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "rule column is required", Path: fmt.Sprintf("%s.config.branches[%d].rules[%d].column", path, i, j)})
				}
				if !slices.Contains([]string{"eq", "ne", "contains", "startsWith", "endsWith", "gt", "lt"}, rule.Operation) {
					issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "unsupported rule operation", Path: fmt.Sprintf("%s.config.branches[%d].rules[%d].operation", path, i, j)})
				}
			}
		}
		return issues, deps

	case NodeTypeUnnestArray:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg UnnestArrayConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if strings.TrimSpace(cfg.ArrayColumn) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "array_column is required", Path: path + ".config.array_column"})
		}
		if strings.TrimSpace(cfg.OutputColumn.Name) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "output column name is required", Path: path + ".config.output_column.name"})
		}
		if strings.TrimSpace(cfg.OutputColumn.SQLType) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "output column sql_type is required", Path: path + ".config.output_column.sql_type"})
		}
		return issues, deps

	case NodeTypePivot:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg PivotConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if len(cfg.GroupBy) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "group_by is required", Path: path + ".config.group_by"})
		}
		if strings.TrimSpace(cfg.PivotColumn) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "pivot_column is required", Path: path + ".config.pivot_column"})
		}
		if strings.TrimSpace(cfg.ValueColumn) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "value_column is required", Path: path + ".config.value_column"})
		}
		if cfg.AggFn != "" && !slices.Contains([]string{"sum", "count"}, cfg.AggFn) {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "agg_fn must be sum or count", Path: path + ".config.agg_fn"})
		}
		if len(cfg.InValues) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "in_values is required", Path: path + ".config.in_values"})
		}
		return issues, deps

	case NodeTypeUnpivot:
		deps, depIssues := parseArrayInputs(node.Inputs, 1, 1, path+".inputs")
		issues = append(issues, depIssues...)
		var cfg UnpivotConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: err.Error(), Path: path + ".config"})
			return issues, deps
		}
		if len(cfg.InColumns) == 0 {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "in_columns is required", Path: path + ".config.in_columns"})
		}
		if strings.TrimSpace(cfg.NameColumn.Name) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "name_column.name is required", Path: path + ".config.name_column.name"})
		}
		if strings.TrimSpace(cfg.NameColumn.SQLType) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "name_column.sql_type is required", Path: path + ".config.name_column.sql_type"})
		}
		if strings.TrimSpace(cfg.ValueColumn.Name) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "value_column.name is required", Path: path + ".config.value_column.name"})
		}
		if strings.TrimSpace(cfg.ValueColumn.SQLType) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "value_column.sql_type is required", Path: path + ".config.value_column.sql_type"})
		}
		return issues, deps

	default:
		issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: fmt.Sprintf("unsupported node type %q", node.Type), Path: path + ".type"})
		return issues, nil
	}
}

func validateGraphSoundness(spec *Spec, nodeByID map[string]int, depsByNode map[string][]string) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	adj := make(map[string][]string, len(nodeByID))
	indegree := make(map[string]int, len(nodeByID))

	for id := range nodeByID {
		adj[id] = nil
		indegree[id] = 0
	}

	for i, node := range spec.Nodes {
		for _, dep := range depsByNode[node.ID] {
			if _, exists := nodeByID[dep]; !exists {
				issues = append(issues, ValidationIssue{Code: "NODE_MISSING_DEPENDENCY", Message: fmt.Sprintf("dependency %q does not exist", dep), Path: fmt.Sprintf("$.nodes[%d].inputs", i)})
				continue
			}
			if dep == node.ID {
				issues = append(issues, ValidationIssue{Code: "NODE_SELF_DEPENDENCY", Message: "node cannot depend on itself", Path: fmt.Sprintf("$.nodes[%d].inputs", i)})
				continue
			}
			adj[dep] = append(adj[dep], node.ID)
			indegree[node.ID]++
		}
	}

	switchLabels := buildSwitchLabelsByNode(spec)

	seenTargetPaths := map[string]string{}
	for i, sink := range spec.Sinks {
		if strings.TrimSpace(sink.NodeID) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "sink node_id is required", Path: fmt.Sprintf("$.sinks[%d].node_id", i)})
			continue
		}
		sinkNodeID, sinkLabel, parseErr := parseSinkNodeRef(sink.NodeID)
		if parseErr != nil {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: parseErr.Error(), Path: fmt.Sprintf("$.sinks[%d].node_id", i)})
			continue
		}
		if strings.TrimSpace(sink.TargetTable) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "sink target_table is required", Path: fmt.Sprintf("$.sinks[%d].target_table", i)})
		}
		if sink.Target != nil {
			targetPath := fmt.Sprintf("$.sinks[%d].target", i)
			if strings.TrimSpace(sink.Target.Type) != "file" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "sink target type must be file", Path: targetPath + ".type"})
			}
			if strings.TrimSpace(sink.Target.Path) == "" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "sink target path is required", Path: targetPath + ".path"})
			} else {
				key := strings.ToLower(strings.TrimSpace(sink.Target.Path))
				if prior, ok := seenTargetPaths[key]; ok {
					issues = append(issues, ValidationIssue{Code: "SINK_DUPLICATE_TARGET_PATH", Message: fmt.Sprintf("multiple sinks target path %q", prior), Path: targetPath + ".path"})
				} else {
					seenTargetPaths[key] = sink.Target.Path
				}
			}
			if !slices.Contains([]string{"csv", "parquet"}, strings.TrimSpace(sink.Target.Format)) {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "sink target format must be csv or parquet", Path: targetPath + ".format"})
			}
			if sink.Target.Mode != "" && sink.Target.Mode != "overwrite" {
				issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: "sink target mode must be overwrite", Path: targetPath + ".mode"})
			}
		}
		nodeIdx, exists := nodeByID[sinkNodeID]
		if !exists {
			issues = append(issues, ValidationIssue{Code: "SINK_UNKNOWN_NODE", Message: fmt.Sprintf("sink references unknown node %q", sinkNodeID), Path: fmt.Sprintf("$.sinks[%d].node_id", i)})
			continue
		}
		nodeType := spec.Nodes[nodeIdx].Type
		if sinkLabel == "" {
			continue
		}
		switch nodeType {
		case NodeTypeConditional:
			if sinkLabel != "if" && sinkLabel != "else" {
				issues = append(issues, ValidationIssue{Code: "SINK_INVALID_LABEL", Message: "conditional sink label must be if or else", Path: fmt.Sprintf("$.sinks[%d].node_id", i)})
			}
		case NodeTypeSwitch:
			allowed, ok := switchLabels[sinkNodeID]
			if !ok {
				allowed = map[string]struct{}{"default": {}}
			}
			if _, ok := allowed[sinkLabel]; !ok {
				issues = append(issues, ValidationIssue{Code: "SINK_INVALID_LABEL", Message: fmt.Sprintf("switch sink label %q is not declared", sinkLabel), Path: fmt.Sprintf("$.sinks[%d].node_id", i)})
			}
		default:
			issues = append(issues, ValidationIssue{Code: "SINK_INVALID_LABEL", Message: "sink label is only allowed for Conditional or Switch nodes", Path: fmt.Sprintf("$.sinks[%d].node_id", i)})
		}
	}

	if len(issues) > 0 {
		return issues
	}

	queue := make([]string, 0, len(indegree))
	for id, d := range indegree {
		if d == 0 {
			queue = append(queue, id)
		}
	}

	visited := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range adj[cur] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if visited != len(spec.Nodes) {
		return []ValidationIssue{{Code: "GRAPH_CYCLE", Message: "cycle detected in pipeline graph", Path: "$.nodes"}}
	}

	return nil
}

func parseArrayInputs(raw json.RawMessage, min int, max int, path string) ([]string, []ValidationIssue) {
	if len(raw) == 0 {
		return nil, []ValidationIssue{{Code: "SCHEMA_REQUIRED", Message: "inputs are required", Path: path}}
	}
	var inputs []string
	if err := decodeStrict(raw, &inputs); err != nil {
		return nil, []ValidationIssue{{Code: "SCHEMA_INVALID", Message: "inputs must be an array of node ids", Path: path}}
	}
	issues := make([]ValidationIssue, 0)
	if len(inputs) < min {
		issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: fmt.Sprintf("requires at least %d input(s)", min), Path: path})
	}
	if max >= 0 && len(inputs) > max {
		issues = append(issues, ValidationIssue{Code: "SCHEMA_INVALID", Message: fmt.Sprintf("requires at most %d input(s)", max), Path: path})
	}
	seen := map[string]struct{}{}
	for i, in := range inputs {
		if strings.TrimSpace(in) == "" {
			issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "input id is required", Path: fmt.Sprintf("%s[%d]", path, i)})
			continue
		}
		if _, ok := seen[in]; ok {
			issues = append(issues, ValidationIssue{Code: "NODE_DUPLICATE_INPUT", Message: fmt.Sprintf("duplicate input %q", in), Path: fmt.Sprintf("%s[%d]", path, i)})
			continue
		}
		seen[in] = struct{}{}
	}
	return inputs, issues
}

func parseJoinInputs(raw json.RawMessage, path string) ([]string, []ValidationIssue) {
	if len(raw) == 0 {
		return nil, []ValidationIssue{{Code: "SCHEMA_REQUIRED", Message: "inputs are required", Path: path}}
	}
	var in JoinInputs
	if err := decodeStrict(raw, &in); err != nil {
		return nil, []ValidationIssue{{Code: "SCHEMA_INVALID", Message: "join inputs must be an object with left/right", Path: path}}
	}
	issues := make([]ValidationIssue, 0)
	if strings.TrimSpace(in.Left) == "" {
		issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "left input is required", Path: path + ".left"})
	}
	if strings.TrimSpace(in.Right) == "" {
		issues = append(issues, ValidationIssue{Code: "SCHEMA_REQUIRED", Message: "right input is required", Path: path + ".right"})
	}
	if in.Left != "" && in.Left == in.Right {
		issues = append(issues, ValidationIssue{Code: "NODE_DUPLICATE_INPUT", Message: "left and right cannot point to the same node", Path: path})
	}
	deps := []string{in.Left, in.Right}
	return deps, issues
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

func parseSinkNodeRef(v string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(v), ":")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", "", fmt.Errorf("sink node_id is required")
	}
	if len(parts) > 2 {
		return "", "", fmt.Errorf("sink node_id must be node_id or node_id:label")
	}
	nodeID := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		return nodeID, "", nil
	}
	label := strings.ToLower(strings.TrimSpace(parts[1]))
	if label == "" {
		return "", "", fmt.Errorf("sink label is required when using node_id:label")
	}
	return nodeID, label, nil
}

func buildSwitchLabelsByNode(spec *Spec) map[string]map[string]struct{} {
	labelsByNode := make(map[string]map[string]struct{})
	for _, node := range spec.Nodes {
		if node.Type != NodeTypeSwitch {
			continue
		}
		labels := map[string]struct{}{"default": {}}
		var cfg SwitchConfig
		if err := decodeStrict(node.Config, &cfg); err != nil {
			labelsByNode[node.ID] = labels
			continue
		}
		for _, b := range cfg.Branches {
			k := strings.ToLower(strings.TrimSpace(b.Label))
			if k != "" {
				labels[k] = struct{}{}
			}
		}
		labelsByNode[node.ID] = labels
	}
	return labelsByNode
}
