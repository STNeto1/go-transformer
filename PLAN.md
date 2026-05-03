# PLAN

## Node Implementation Order

1. [x] `DataSource` (v1: csv/parquet; modes: infer, declared, declared_with_check (strict order/name/type))
2. [x] `SelectColumns`
3. [x] `Filter`
4. [x] `Sort`
5. [x] `LimitSample` (implemented `limit`)
6. [x] `ConstantColumn`
7. [x] `ComputeColumn` (v1: `{{column}}` string templates + raw SQL expressions; no same-node computed-column references)
8. [x] `RenameColumns`
9. [x] `CastColumns`
10. [x] `FillReplace`
11. [x] `Deduplicate`
12. [x] `Aggregate`
13. [x] `MergeUnion` (mode available: `strict_positional` (default), `align_by_name`; set op: `UNION ALL`)
14. [x] `Join` (mode available: `inner` (default), `left`)
15. [x] `Conditional` (mode available: `all` (default), `any`; empty rules -> all rows to `if`)
16. [x] `Switch` (independent per-branch matching, multi-match allowed; default gets rows matching no branch)
17. [x] `UnnestArray` (v1: single-array unnest, append output column, empty/null arrays emit no rows)
18. [x] `PivotUnpivot` (v1: `Pivot` with `sum`/`count`, and `Unpivot`)
