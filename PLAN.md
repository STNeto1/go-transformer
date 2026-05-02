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
12. [ ] `Aggregate`
13. [ ] `MergeUnion` (implement `UNION ALL` first)
14. [ ] `Join` (implement `INNER JOIN` first)
15. [ ] `Conditional`
16. [ ] `Switch`
17. [ ] `UnnestArray`
18. [ ] `PivotUnpivot`
