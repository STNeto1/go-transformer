package pipeline

import "encoding/json"

type Spec struct {
	PipelineID string   `json:"pipeline_id"`
	Version    int      `json:"version"`
	Defaults   Defaults `json:"defaults,omitempty"`
	Nodes      []Node   `json:"nodes"`
	Sinks      []Sink   `json:"sinks"`
}

type Defaults struct {
	TelemetryFormat string `json:"telemetry_format,omitempty"`
	TelemetryLevel  string `json:"telemetry_level,omitempty"`
}

type Sink struct {
	NodeID      string `json:"node_id"`
	TargetTable string `json:"target_table"`
}

type Node struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Inputs json.RawMessage `json:"inputs,omitempty"`
	Config json.RawMessage `json:"config"`
}

type DataSourceConfig struct {
	Format  string       `json:"format"`
	Path    string       `json:"path"`
	Mode    string       `json:"mode"`
	Columns []ColumnSpec `json:"columns,omitempty"`
}

type ColumnSpec struct {
	Name    string `json:"name"`
	SQLType string `json:"sql_type"`
}

type FilterConfig struct {
	Mode  string       `json:"mode,omitempty"`
	Rules []FilterRule `json:"rules"`
}

type FilterRule struct {
	Column    string `json:"column"`
	Operation string `json:"operation"`
	Value     any    `json:"value"`
}

type AggregateConfig struct {
	GroupBy []string          `json:"group_by"`
	Metrics []AggregateMetric `json:"metrics"`
}

type AggregateMetric struct {
	Function string `json:"function"`
	Column   string `json:"column"`
	As       string `json:"as"`
}

type MergeUnionConfig struct {
	Mode string `json:"mode"`
}

type JoinConfig struct {
	Mode string    `json:"mode"`
	Keys []JoinKey `json:"keys"`
}

type JoinKey struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

type JoinInputs struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

type SelectColumnsConfig struct {
	Columns []string `json:"columns"`
}

type SortConfig struct {
	Keys []SortKeyConfig `json:"keys"`
}

type SortKeyConfig struct {
	Column    string `json:"column"`
	Direction string `json:"direction,omitempty"`
	Nulls     string `json:"nulls,omitempty"`
}

type LimitConfig struct {
	Count  int  `json:"count"`
	Offset *int `json:"offset,omitempty"`
}

type ConstantColumnConfig struct {
	Column ColumnSpec `json:"column"`
	Value  any        `json:"value"`
}

type ComputeColumnsConfig struct {
	Columns []ComputeColumnSpec `json:"columns"`
}

type ComputeColumnSpec struct {
	Name    string `json:"name"`
	SQLType string `json:"sql_type"`
	Expr    string `json:"expr"`
}

type RenameColumnsConfig struct {
	Renames []RenameColumnSpec `json:"renames"`
}

type RenameColumnSpec struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type CastColumnsConfig struct {
	Casts []CastColumnSpec `json:"casts"`
}

type CastColumnSpec struct {
	Column  string `json:"column"`
	SQLType string `json:"sql_type"`
}

type FillReplaceConfig struct {
	Rules []FillReplaceRuleSpec `json:"rules"`
}

type FillReplaceRuleSpec struct {
	Column       string `json:"column"`
	FillNullWith any    `json:"fill_null_with,omitempty"`
	ReplaceFrom  any    `json:"replace_from,omitempty"`
	ReplaceTo    any    `json:"replace_to,omitempty"`
}

type DeduplicateConfig struct {
	Columns []string        `json:"columns"`
	Keep    string          `json:"keep,omitempty"`
	OrderBy []SortKeyConfig `json:"order_by"`
}

type ConditionalConfig struct {
	Mode  string       `json:"mode,omitempty"`
	Rules []FilterRule `json:"rules"`
}

type SwitchConfig struct {
	Branches []SwitchBranchConfig `json:"branches"`
}

type SwitchBranchConfig struct {
	Label string       `json:"label"`
	Mode  string       `json:"mode,omitempty"`
	Rules []FilterRule `json:"rules"`
}

type UnnestArrayConfig struct {
	ArrayColumn  string     `json:"array_column"`
	OutputColumn ColumnSpec `json:"output_column"`
}

type PivotConfig struct {
	GroupBy     []string `json:"group_by"`
	PivotColumn string   `json:"pivot_column"`
	ValueColumn string   `json:"value_column"`
	AggFn       string   `json:"agg_fn,omitempty"`
	InValues    []string `json:"in_values"`
}

type UnpivotConfig struct {
	Passthrough []string   `json:"passthrough"`
	NameColumn  ColumnSpec `json:"name_column"`
	ValueColumn ColumnSpec `json:"value_column"`
	InColumns   []string   `json:"in_columns"`
}
