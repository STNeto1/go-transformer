# PLAN

## Node Implementation Order

1. [ ] `DataSource` (kinda at the end)
2. [x] `SelectColumns`
3. [x] `Filter`
4. [x] `Sort`
5. [x] `LimitSample` (implemented `limit`)
6. [x] `ConstantColumn`
7. [ ] `ComputeColumn`
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
18. [ ] `PivotUnpivot`
