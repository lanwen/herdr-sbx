# Project guidance

Before changing setup, behavior, or tests, read [the project reference](docs/reference.md).
Use public documentation and exposed API contracts in repository content and commit messages.
Run tests with isolated Herdr sessions and sandbox fixtures.

## Focused agents

Use the project agents in `.codex/agents/` when a task benefits from focused work.
For small changes, keep the work in the main agent.
The main agent owns task scope, file assignments, integration, and the final response.

Give each agent a concrete question or change, relevant context, and a completion condition.
Assign each file to one editor at a time.
Run independent reads in parallel, then pass findings to the implementer.
After implementation, request domain review when behavior crosses Herdr or sandbox boundaries.
Give legwork a stable revision to test after edits finish.
For commits, assign the exact files after review and required tests pass.

Reuse the Herdr expert within a task when its context remains useful.
Across sessions, rebuild context from `docs/reference.md` and current code.
If a change alters documented behavior, update the reference as part of that change.
