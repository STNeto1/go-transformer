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
15. [ ] `Conditional`
16. [ ] `Switch`
17. [ ] `UnnestArray`
18. [ ] `PivotUnpivot`
