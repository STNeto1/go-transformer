package workflow

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/require"

	"go-transformer/internal/pipeline"
)

func TestRun_DataSourceFilter_AllAndAny(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age,country\n1,Ana,20,BR\n2,Bob,45,US\n3,Carla,55,BR\n4,Dan,38,US\n")

	payloadAll := []byte(`{
		"pipeline_id": "wf_all",
		"version": 1,
		"nodes": [
			{"id": "src", "type": "DataSource", "config": {"format": "csv", "path": "` + csvPath + `", "mode": "infer"}},
			{"id": "f", "type": "Filter", "inputs": ["src"], "config": {"mode": "all", "rules": [
				{"column": "country", "operation": "eq", "value": "BR"},
				{"column": "age", "operation": "gt", "value": 30}
			]}}
		],
		"sinks": [{"node_id": "f", "target_table": "out_all"}]
	}`)

	specAll, err := pipeline.ParseAndValidateJSON(payloadAll)
	require.NoError(t, err)

	resultsAll, err := Run(db, specAll)
	require.NoError(t, err)
	require.Len(t, resultsAll, 1)
	require.Equal(t, 1, resultsAll[0].RowCount)
	require.Equal(t, 1, mustCountRows(t, db, "out_all"))

	payloadAny := []byte(`{
		"pipeline_id": "wf_any",
		"version": 1,
		"nodes": [
			{"id": "src", "type": "DataSource", "config": {"format": "csv", "path": "` + csvPath + `", "mode": "infer"}},
			{"id": "f", "type": "Filter", "inputs": ["src"], "config": {"mode": "any", "rules": [
				{"column": "country", "operation": "eq", "value": "BR"},
				{"column": "age", "operation": "gt", "value": 50}
			]}}
		],
		"sinks": [{"node_id": "f", "target_table": "out_any"}]
	}`)

	specAny, err := pipeline.ParseAndValidateJSON(payloadAny)
	require.NoError(t, err)

	resultsAny, err := Run(db, specAny)
	require.NoError(t, err)
	require.Len(t, resultsAny, 1)
	require.Equal(t, 2, resultsAny[0].RowCount)
	require.Equal(t, 2, mustCountRows(t, db, "out_any"))
}

func TestRun_DuplicateSinkTargetsReturnStructuredErrorBeforeExecution(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age,country\n1,Ana,20,BR\n")
	spec := &pipeline.Spec{
		PipelineID: "wf_duplicate_sink",
		Version:    1,
		Nodes: []pipeline.Node{
			{ID: "src", Type: "DataSource", Config: []byte(`{"format":"csv","path":"` + csvPath + `","mode":"infer"}`)},
		},
		Sinks: []pipeline.Sink{
			{NodeID: "src", TargetTable: " Out "},
			{NodeID: "src", TargetTable: "out"},
		},
	}

	_, err := Run(db, spec)
	require.Error(t, err)

	var wfErr *WorkflowError
	require.True(t, errors.As(err, &wfErr))
	require.Equal(t, ErrorCodeDuplicateSinkTarget, wfErr.Code)
	require.Equal(t, "validate_sinks", wfErr.Stage)
	require.False(t, tableExists(t, db, "wf_wf_duplicate_sink_src"))
}

func TestOutputRefHelpers(t *testing.T) {
	require.Equal(t, "node", normalizeOutputRef("Node"))
	require.Equal(t, "node:if", normalizeOutputRef("Node:IF"))
	require.Equal(t, "node:vip", makeOutputRef(" Node ", " Vip "))

	base, label, err := splitOutputRef(" node : Else ")
	require.NoError(t, err)
	require.Equal(t, "node", base)
	require.Equal(t, "Else", label)

	_, _, err = splitOutputRef("node:")
	require.Error(t, err)
	_, _, err = splitOutputRef("a:b:c")
	require.Error(t, err)
}

func TestRun_UnsupportedNodeReturnsStructuredError(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age,country\n1,Ana,20,BR\n")
	payload := []byte(`{
		"pipeline_id": "wf_panic",
		"version": 1,
		"nodes": [
			{"id": "src", "type": "DataSource", "config": {"format": "csv", "path": "` + csvPath + `", "mode": "infer"}},
			{"id": "s", "type": "Sort", "inputs": ["src"], "config": {"keys": [{"column": "age", "direction": "desc"}]}}
		],
		"sinks": [{"node_id": "s", "target_table": "out"}]
	}`)

	spec, err := pipeline.ParseAndValidateJSON(payload)
	require.NoError(t, err)

	// force a structurally valid spec that contains unsupported node type
	spec = &pipeline.Spec{
		PipelineID: "wf_panic",
		Version:    1,
		Nodes: []pipeline.Node{
			{ID: "src", Type: "DataSource", Config: []byte(`{"format":"csv","path":"` + csvPath + `","mode":"infer"}`)},
			{ID: "x", Type: "NotSupportedYet", Inputs: []byte(`["src"]`), Config: []byte(`{"ok":true}`)},
		},
		Sinks: []pipeline.Sink{{NodeID: "x", TargetTable: "out"}},
	}

	_, err = Run(db, spec)
	require.Error(t, err)

	var wfErr *WorkflowError
	require.True(t, errors.As(err, &wfErr))
	require.Equal(t, ErrorCodeUnsupportedNodeType, wfErr.Code)
	require.Equal(t, "dispatch", wfErr.Stage)
	require.Equal(t, "x", wfErr.NodeID)
	require.Equal(t, "NotSupportedYet", wfErr.NodeType)
}

func TestRun_MissingSinkOutputRefReturnsStructuredError(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age,country\n1,Ana,20,BR\n")
	spec := &pipeline.Spec{
		PipelineID: "wf_sink",
		Version:    1,
		Nodes: []pipeline.Node{
			{ID: "src", Type: "DataSource", Config: []byte(`{"format":"csv","path":"` + csvPath + `","mode":"infer"}`)},
		},
		Sinks: []pipeline.Sink{{NodeID: "missing", TargetTable: "out"}},
	}

	_, err := Run(db, spec)
	require.Error(t, err)

	var wfErr *WorkflowError
	require.True(t, errors.As(err, &wfErr))
	require.Equal(t, ErrorCodeSinkResolution, wfErr.Code)
	require.Equal(t, "resolve_sink", wfErr.Stage)
}

func TestRun_TableNameCollisionReturnsStructuredError(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age,country\n1,Ana,20,BR\n")
	spec := &pipeline.Spec{
		PipelineID: "wf collision",
		Version:    1,
		Nodes: []pipeline.Node{
			{ID: "A B", Type: "DataSource", Config: []byte(`{"format":"csv","path":"` + csvPath + `","mode":"infer"}`)},
			{ID: "A_B", Type: "DataSource", Config: []byte(`{"format":"csv","path":"` + csvPath + `","mode":"infer"}`)},
		},
		Sinks: []pipeline.Sink{{NodeID: "A_B", TargetTable: "out"}},
	}

	_, err := Run(db, spec)
	require.Error(t, err)

	var wfErr *WorkflowError
	require.True(t, errors.As(err, &wfErr))
	require.Equal(t, ErrorCodeTableNameCollision, wfErr.Code)
	require.Equal(t, "table_name", wfErr.Stage)
}

func TestRun_JoinMergeConditionalSwitch(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	leftPath := writeCSV(t, "left.csv", "id,name,age,country\n1,Ana,20,BR\n2,Bob,45,US\n3,Carla,55,BR\n")
	rightPath := writeCSV(t, "right.csv", "id,segment\n1,retail\n2,vip\n3,vip\n")

	payload := []byte(`{
		"pipeline_id": "wf_full",
		"version": 1,
		"nodes": [
			{"id":"src_left","type":"DataSource","config":{"format":"csv","path":"` + leftPath + `","mode":"infer"}},
			{"id":"src_right","type":"DataSource","config":{"format":"csv","path":"` + rightPath + `","mode":"infer"}},
			{"id":"sel","type":"SelectColumns","inputs":["src_left"],"config":{"columns":["id","age","country"]}},
			{"id":"cast","type":"CastColumns","inputs":["sel"],"config":{"casts":[{"column":"age","sql_type":"DOUBLE"}]}},
			{"id":"cast2","type":"CastColumns","inputs":["sel"],"config":{"casts":[{"column":"age","sql_type":"DOUBLE"}]}},
			{"id":"merge","type":"MergeUnion","inputs":["cast","cast2"],"config":{"mode":"strict_positional"}},
			{"id":"join","type":"Join","inputs":{"left":"src_left","right":"src_right"},"config":{"mode":"left","keys":[{"left":"id","right":"id"}]}},
			{"id":"cond","type":"Conditional","inputs":["join"],"config":{"mode":"all","rules":[{"column":"age","operation":"gt","value":30}]}},
			{"id":"sw","type":"Switch","inputs":["join"],"config":{"branches":[
				{"label":"vip","mode":"all","rules":[{"column":"segment","operation":"eq","value":"vip"}]}
			]}}
		],
		"sinks": [
			{"node_id":"merge","target_table":"merge_out"},
			{"node_id":"cond:if","target_table":"cond_if"},
			{"node_id":"cond:else","target_table":"cond_else"},
			{"node_id":"sw:VIP","target_table":"sw_vip"},
			{"node_id":"sw:default","target_table":"sw_default"}
		]
	}`)

	spec, err := pipeline.ParseAndValidateJSON(payload)
	require.NoError(t, err)

	results, err := Run(db, spec)
	require.NoError(t, err)
	require.Len(t, results, 5)

	counts := map[string]int{}
	for _, r := range results {
		counts[r.NodeID] = r.RowCount
	}
	require.Equal(t, 6, counts["merge"])
	require.Equal(t, 2, counts["cond:if"])
	require.Equal(t, 1, counts["cond:else"])
	require.Equal(t, 2, counts["sw:VIP"])
	require.Equal(t, 1, counts["sw:default"])
	require.Equal(t, 6, mustCountRows(t, db, "merge_out"))
	require.Equal(t, 2, mustCountRows(t, db, "cond_if"))
	require.Equal(t, 1, mustCountRows(t, db, "cond_else"))
	require.Equal(t, 2, mustCountRows(t, db, "sw_vip"))
	require.Equal(t, 1, mustCountRows(t, db, "sw_default"))
}

func TestRun_DiamondDAG(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age,country\n1,Ana,20,BR\n2,Bob,45,US\n3,Carla,55,BR\n4,Dan,38,US\n")
	payload := []byte(`{
		"pipeline_id":"wf_diamond",
		"version":1,
		"nodes":[
			{"id":"src","type":"DataSource","config":{"format":"csv","path":"` + csvPath + `","mode":"infer"}},
			{"id":"left","type":"Filter","inputs":["src"],"config":{"rules":[{"column":"country","operation":"eq","value":"BR"}]}},
			{"id":"right","type":"Filter","inputs":["src"],"config":{"rules":[{"column":"age","operation":"gt","value":30}]}},
			{"id":"merged","type":"MergeUnion","inputs":["left","right"],"config":{"mode":"strict_positional"}}
		],
		"sinks":[{"node_id":"merged","target_table":"diamond_out"}]
	}`)

	spec, err := pipeline.ParseAndValidateJSON(payload)
	require.NoError(t, err)
	results, err := Run(db, spec)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "merged", results[0].NodeID)
	require.Equal(t, "diamond_out", results[0].TargetTable)
	require.Equal(t, 5, results[0].RowCount)
	require.Equal(t, 5, mustCountRows(t, db, "diamond_out"))
}

func TestRun_BranchingSinkRefs(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age,segment\n1,Ana,20,retail\n2,Bob,45,vip\n3,Carla,55,vip\n4,Dan,38,retail\n")
	payload := []byte(`{
		"pipeline_id":"wf_branch_sinks",
		"version":1,
		"nodes":[
			{"id":"src","type":"DataSource","config":{"format":"csv","path":"` + csvPath + `","mode":"infer"}},
			{"id":"cond","type":"Conditional","inputs":["src"],"config":{"rules":[{"column":"age","operation":"gt","value":40}]}},
			{"id":"sw","type":"Switch","inputs":["src"],"config":{"branches":[{"label":"vip","rules":[{"column":"segment","operation":"eq","value":"vip"}]}]}}
		],
		"sinks":[
			{"node_id":"cond:IF","target_table":"branch_cond_if"},
			{"node_id":"cond:else","target_table":"branch_cond_else"},
			{"node_id":"sw:VIP","target_table":"branch_sw_vip"},
			{"node_id":"sw:default","target_table":"branch_sw_default"}
		]
	}`)

	spec, err := pipeline.ParseAndValidateJSON(payload)
	require.NoError(t, err)
	results, err := Run(db, spec)
	require.NoError(t, err)
	require.Len(t, results, 4)

	counts := map[string]int{}
	for _, r := range results {
		counts[r.NodeID] = r.RowCount
	}
	require.Equal(t, 2, counts["cond:IF"])
	require.Equal(t, 2, counts["cond:else"])
	require.Equal(t, 2, counts["sw:VIP"])
	require.Equal(t, 2, counts["sw:default"])
	require.Equal(t, 2, mustCountRows(t, db, "branch_cond_if"))
	require.Equal(t, 2, mustCountRows(t, db, "branch_cond_else"))
	require.Equal(t, 2, mustCountRows(t, db, "branch_sw_vip"))
	require.Equal(t, 2, mustCountRows(t, db, "branch_sw_default"))
}

func TestRun_ExistingSinkTargetReturnsStructuredError(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age,country\n1,Ana,20,BR\n")
	require.NoError(t, execSQL(db, "CREATE TABLE out_existing (id BIGINT)"))
	payload := []byte(`{
		"pipeline_id": "wf_existing_sink",
		"version": 1,
		"nodes": [
			{"id": "src", "type": "DataSource", "config": {"format": "csv", "path": "` + csvPath + `", "mode": "infer"}}
		],
		"sinks": [{"node_id": "src", "target_table": "out_existing"}]
	}`)

	spec, err := pipeline.ParseAndValidateJSON(payload)
	require.NoError(t, err)
	_, err = Run(db, spec)
	require.Error(t, err)

	var wfErr *WorkflowError
	require.True(t, errors.As(err, &wfErr))
	require.Equal(t, ErrorCodeSinkMaterialize, wfErr.Code)
	require.Equal(t, "materialize_sink", wfErr.Stage)
}

func TestRun_InvalidRuntimeConfigsReturnStructuredErrors(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age,country\n1,Ana,20,BR\n")
	tests := []struct {
		name     string
		spec     *pipeline.Spec
		code     ErrorCode
		stage    string
		nodeID   string
		nodeType string
	}{
		{
			name: "bad config json",
			spec: &pipeline.Spec{PipelineID: "wf_bad_json", Version: 1, Nodes: []pipeline.Node{
				{ID: "src", Type: "DataSource", Config: []byte(`{"format":"csv","path":"` + csvPath + `","mode":"infer"}`)},
				{ID: "f", Type: "Filter", Inputs: []byte(`["src"]`), Config: []byte(`{"rules":`)},
			}, Sinks: []pipeline.Sink{{NodeID: "f", TargetTable: "bad_json_out"}}},
			code: ErrorCodeConfigDecode, stage: "decode_config", nodeID: "f", nodeType: "Filter",
		},
		{
			name: "derive error",
			spec: &pipeline.Spec{PipelineID: "wf_derive", Version: 1, Nodes: []pipeline.Node{
				{ID: "src", Type: "DataSource", Config: []byte(`{"format":"csv","path":"` + csvPath + `","mode":"infer"}`)},
				{ID: "sel", Type: "SelectColumns", Inputs: []byte(`["src"]`), Config: []byte(`{"columns":["missing"]}`)},
			}, Sinks: []pipeline.Sink{{NodeID: "sel", TargetTable: "derive_out"}}},
			code: ErrorCodeDerive, stage: "derive", nodeID: "sel", nodeType: "SelectColumns",
		},
		{
			name: "dependency shape",
			spec: &pipeline.Spec{PipelineID: "wf_dep_shape", Version: 1, Nodes: []pipeline.Node{
				{ID: "src", Type: "DataSource", Config: []byte(`{"format":"csv","path":"` + csvPath + `","mode":"infer"}`)},
				{ID: "f", Type: "Filter", Inputs: []byte(`["src","src"]`), Config: []byte(`{"rules":[{"column":"age","operation":"gt","value":10}]}`)},
			}, Sinks: []pipeline.Sink{{NodeID: "f", TargetTable: "dep_shape_out"}}},
			code: ErrorCodeDependencyShape, stage: "dispatch", nodeID: "f", nodeType: "Filter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Run(db, tt.spec)
			require.Error(t, err)
			var wfErr *WorkflowError
			require.True(t, errors.As(err, &wfErr))
			require.Equal(t, tt.code, wfErr.Code)
			require.Equal(t, tt.stage, wfErr.Stage)
			require.Equal(t, tt.nodeID, wfErr.NodeID)
			require.Equal(t, tt.nodeType, wfErr.NodeType)
		})
	}
}

func TestRun_SortLimit_And_MutationChain(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "mut.csv", "id,name,age,country\n1,Ana,20,BR\n2,Bob,45,US\n3,Carla,55,\n4,Dan,38,US\n")

	payload := []byte(`{
		"pipeline_id": "wf_mut",
		"version": 1,
		"nodes": [
			{"id":"src","type":"DataSource","config":{"format":"csv","path":"` + csvPath + `","mode":"infer"}},
			{"id":"sort","type":"Sort","inputs":["src"],"config":{"keys":[{"column":"age","direction":"desc"}]}},
			{"id":"lim","type":"Limit","inputs":["sort"],"config":{"count":3}},
			{"id":"const","type":"ConstantColumn","inputs":["lim"],"config":{"column":{"name":"src_tag","sql_type":"VARCHAR"},"value":"demo"}},
			{"id":"comp","type":"ComputeColumn","inputs":["const"],"config":{"columns":[{"name":"label","sql_type":"VARCHAR","expr":"{{name}}-{{country}}"}]}},
			{"id":"ren","type":"RenameColumns","inputs":["comp"],"config":{"renames":[{"from":"name","to":"full_name"}]}},
			{"id":"cast","type":"CastColumns","inputs":["ren"],"config":{"casts":[{"column":"age","sql_type":"DOUBLE"}]}},
			{"id":"fill","type":"FillReplace","inputs":["cast"],"config":{"rules":[{"column":"country","fill_null_with":"UNKNOWN"}]}}
		],
		"sinks": [
			{"node_id":"lim","target_table":"out_lim"},
			{"node_id":"fill","target_table":"out_fill"}
		]
	}`)

	spec, err := pipeline.ParseAndValidateJSON(payload)
	require.NoError(t, err)

	results, err := Run(db, spec)
	require.NoError(t, err)
	require.Len(t, results, 2)

	counts := map[string]int{}
	for _, r := range results {
		counts[r.NodeID] = r.RowCount
	}
	require.Equal(t, 3, counts["lim"])
	require.Equal(t, 3, counts["fill"])
}

func TestRun_DeduplicateAggregate(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "dedup.csv", "id,country,age\n1,US,20\n2,US,30\n3,BR,40\n4,BR,25\n")
	payload := []byte(`{
		"pipeline_id":"wf_dedup",
		"version":1,
		"nodes":[
			{"id":"src","type":"DataSource","config":{"format":"csv","path":"` + csvPath + `","mode":"infer"}},
			{"id":"dedup","type":"Deduplicate","inputs":["src"],"config":{"columns":["country"],"keep":"first","order_by":[{"column":"age","direction":"desc"}]}},
			{"id":"agg","type":"Aggregate","inputs":["dedup"],"config":{"group_by":["country"],"metrics":[{"function":"count","column":"*","as":"cnt"}]}}
		],
		"sinks":[
			{"node_id":"dedup","target_table":"out_dedup"},
			{"node_id":"agg","target_table":"out_agg"}
		]
	}`)

	spec, err := pipeline.ParseAndValidateJSON(payload)
	require.NoError(t, err)
	results, err := Run(db, spec)
	require.NoError(t, err)

	counts := map[string]int{}
	for _, r := range results {
		counts[r.NodeID] = r.RowCount
	}
	require.Equal(t, 2, counts["dedup"])
	require.Equal(t, 2, counts["agg"])
}

func TestRun_Reshape_UnnestPivotUnpivot(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	unnestPath := writeCSV(t, "unnest.csv", "id,tags\n1,\"[1,2]\"\n2,\"[3]\"\n")
	pivotPath := writeCSV(t, "pivot.csv", "id,region,amount\n1,north,10\n1,south,5\n2,north,7\n")
	widePath := writeCSV(t, "wide.csv", "id,m1,m2\n1,10,20\n2,30,40\n")

	payload := []byte(`{
		"pipeline_id":"wf_reshape",
		"version":1,
		"nodes":[
			{"id":"src_u","type":"DataSource","config":{"format":"csv","path":"` + unnestPath + `","mode":"infer"}},
			{"id":"cast_u","type":"CastColumns","inputs":["src_u"],"config":{"casts":[{"column":"tags","sql_type":"INTEGER[]"}]}},
			{"id":"unn","type":"UnnestArray","inputs":["cast_u"],"config":{"array_column":"tags","output_column":{"name":"tag","sql_type":"INTEGER"}}},

			{"id":"src_p","type":"DataSource","config":{"format":"csv","path":"` + pivotPath + `","mode":"infer"}},
			{"id":"piv","type":"Pivot","inputs":["src_p"],"config":{"group_by":["id"],"pivot_column":"region","value_column":"amount","agg_fn":"sum","in_values":["north","south"]}},

			{"id":"src_w","type":"DataSource","config":{"format":"csv","path":"` + widePath + `","mode":"infer"}},
			{"id":"unp","type":"Unpivot","inputs":["src_w"],"config":{"passthrough":["id"],"name_column":{"name":"metric","sql_type":"VARCHAR"},"value_column":{"name":"val","sql_type":"BIGINT"},"in_columns":["m1","m2"]}}
		],
		"sinks":[
			{"node_id":"unn","target_table":"out_unn"},
			{"node_id":"piv","target_table":"out_piv"},
			{"node_id":"unp","target_table":"out_unp"}
		]
	}`)

	spec, err := pipeline.ParseAndValidateJSON(payload)
	require.NoError(t, err)
	results, err := Run(db, spec)
	require.NoError(t, err)

	counts := map[string]int{}
	for _, r := range results {
		counts[r.NodeID] = r.RowCount
	}
	require.Equal(t, 3, counts["unn"])
	require.Equal(t, 2, counts["piv"])
	require.Equal(t, 4, counts["unp"])
}

func TestWorkflowRun_ExportsFileSinkTarget(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	csvPath := writeCSV(t, "people.csv", "id,name,age\n1,Ana,20\n2,Bob,45\n")
	outPath := filepath.Join(t.TempDir(), "people_out.parquet")
	payload := []byte(`{
		"pipeline_id": "wf_file_sink",
		"version": 1,
		"nodes": [
			{"id":"src","type":"DataSource","config":{"format":"csv","path":"` + csvPath + `","mode":"infer","options":{"header":true}}}
		],
		"sinks": [{"node_id":"src","target_table":"out_file_sink","target":{"type":"file","format":"parquet","path":"` + outPath + `","mode":"overwrite"}}]
	}`)

	spec, err := pipeline.ParseAndValidateJSON(payload)
	require.NoError(t, err)
	results, err := Run(db, spec)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, outPath, results[0].TargetPath)
	require.Equal(t, "parquet", results[0].Format)
	require.Equal(t, 2, results[0].RowCount)
	require.FileExists(t, outPath)
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", ":memory:")
	require.NoError(t, err)
	return db
}

func writeCSV(t *testing.T, name string, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func mustCountRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&count))
	return count
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, db.QueryRow("SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = ?)", table).Scan(&exists))
	return exists
}

func execSQL(db *sql.DB, query string) error {
	_, err := db.Exec(query)
	return err
}
