---
name: go-review-code
description: 'Review Go code changes, pull requests, diffs, bug fixes, and refactors. Use when asked to review code, inspect a patch, find regressions, identify risks, or check missing tests in Go projects.'
argument-hint: 'What should be reviewed: diff, files, PR, or feature area'
user-invocable: true
disable-model-invocation: false
---

# Go Code Review

Review Go changes with a defect-finding mindset. Optimize for identifying bugs, behavioral regressions, concurrency mistakes, error-handling gaps, API contract breaks, and missing validation. Keep summaries short and make findings the main output.

## When to Use

- Review a Go diff, pull request, commit, or local uncommitted changes.
- Inspect a bug fix or refactor for regressions.
- Check whether a change needs tests, validation, or narrower scope.
- Audit code paths that touch CLI flow, config loading, concurrency, I/O, or external services.

## Inputs To Gather

- The review target: changed files, git diff, commit, PR description, or feature area.
- The intended behavior or acceptance criteria if they are known.
- The narrowest validation available: tests, `go test`, `go build`, or a command-specific smoke check.

## Review Procedure

1. Anchor on the concrete change surface first.
   Read the diff, the changed files, or the named symbols before exploring unrelated areas.

2. Identify the controlling code path.
   Step from wiring into the nearest logic that computes behavior, mutates state, manages resources, or handles errors.

3. Look for defects before style issues.
   Prioritize correctness, safety, behavior changes, race potential, resource leaks, nil handling, boundary conditions, and broken assumptions.

4. Check Go-specific failure modes.
   Review error propagation, deferred cleanup, context cancellation, goroutine lifecycle, channel ownership, zero values, pointer/value semantics, and interface contract mismatches.

5. Validate claims against nearby evidence.
   Use the surrounding implementation, call sites, and available tests to confirm whether each concern is real.

6. Check test and validation coverage.
   Call out missing tests when the change alters behavior, error paths, parsing, concurrency, config handling, or persistence.

7. Keep the report scoped.
   Exclude nits unless they hide a bug, create maintenance risk, or contradict established project conventions.

## Decision Rules

- If there is no concrete defect, say that explicitly instead of padding the review.
- If a possible issue depends on an assumption, state the assumption and what would disprove it.
- If the code is mostly wiring, hop once to the implementation that actually decides behavior.
- If executable validation exists, prefer that evidence over speculative findings.
- If the review target is partial, mention the coverage boundary instead of pretending the review was exhaustive.

## Quality Bar For Findings

Each finding should have all of the following:

- A concrete impact: what breaks, regresses, or becomes unsafe.
- The triggering condition: when the problem occurs.
- The code location: file and line reference when available.
- A short rationale tied to the actual code path.

Do not report a finding when it is only a vague possibility with no local evidence.

## Output Format

Present results in this order:

1. Findings, ordered by severity.
2. Open questions or assumptions that affect confidence.
3. Brief change summary only if useful.
4. Residual risks or testing gaps if no findings were discovered.

For each finding, include:

- Severity: `high`, `medium`, or `low`.
- Location: file path with line reference when available.
- Why it matters in one or two sentences.

## Go Review Checklist

- Are errors returned, wrapped, logged, or intentionally ignored?
- Are `context.Context` values passed through and honored on blocking work?
- Do goroutines, channels, and deferred cleanup have a clear owner and shutdown path?
- Do config or flag changes preserve previous defaults and startup behavior?
- Are nil, empty, and zero-value cases handled correctly?
- Do public interfaces and exported behavior remain backward compatible?
- Is file, network, or client lifecycle cleanup guaranteed on success and failure?
- Does the change need a focused build, test, or smoke check?

## Completion Criteria

- The review names the highest-value findings first.
- Every finding is backed by local code evidence.
- The report clearly distinguishes facts, assumptions, and missing coverage.
- The review notes missing tests or validation where behavior changed.
