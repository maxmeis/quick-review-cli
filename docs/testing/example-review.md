# Review: maxmeis/quick-review-example #1

Head: `2a635456588a14fee9f3b123ff65532e9468ee8c`  
Merge-base: `8349878ddf47b2a00be97a4b9910b5c5a9d3435f`

# Review summary

This revision documents `Percent(1, 2) == 50` but changes the implementation so that the same call returns `0`.

- **Head:** `2a635456588a14fee9f3b123ff65532e9468ee8c`
- **Merge-base:** `8349878ddf47b2a00be97a4b9910b5c5a9d3435f`
- **Coverage:** Full diff, implementation, documentation, tests, callers, workflow, and repository guidance inspected.
- **Subagents:** Two bounded read-only reviewers covered arithmetic correctness and test coverage. Both completed; their shared finding was independently validated.
- **Verification:** Code inspection only; project tests were not run. `git diff --check` passed.

## Verified findings

### P1 — Division before scaling breaks the documented percentage contract

**Location:** `progress.go:8`

**Trigger:** Any partial workload where `0 < done < total`, including the newly documented `Percent(1, 2)` example.

**Impact:** Go performs integer division first, so `1 / 2` becomes `0` and the function returns `0` instead of `50`. All partial progress below 100% is similarly reported as zero; other ratios are quantized into multiples of 100.

**Evidence:** The result contradicts both the new contract at `README.md:5` and the existing `{1, 2, 50}` test case at `progress_test.go:6`.

**Fix direction:** Restore `done * 100 / total`. If overflow for large integers is a concern, use an overflow-safe quotient/remainder calculation that preserves fractional percentages.

## Style and maintainability

No actionable style or maintainability issues were identified.

## Remaining questions and suggested checks

- Run `go test ./...` after correcting the expression; the existing test directly covers this regression.
- A newer PR head (`9f7d7be560784f08276fcc3ad2489ac17487d7a2`) appeared during review and requires a separate review. This report applies only to `2a635456588a14fee9f3b123ff65532e9468ee8c`.

The completed report remains scoped to `2a635456588a14fee9f3b123ff65532e9468ee8c`. The newer head `9f7d7be560784f08276fcc3ad2489ac17487d7a2` was not reviewed.
