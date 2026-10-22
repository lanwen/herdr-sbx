# Project guidance

Before changing setup, behavior, or tests, read [the project reference](docs/reference.md).
Use public documentation and exposed API contracts in repository content and commit messages.
Run tests with isolated Herdr sessions and sandbox fixtures.

## Focused agents

The main agent owns the specification, requirements alignment, task scope, file assignments, integration, and the final response.
Delegate implementation, code edits, tests, visual design, and domain review to focused project agents in `.codex/agents/`, including for small code changes.
For assignments, contract changes, reviews, and verification handoffs, follow [the development workflow](docs/development-workflow.md).
The main agent maintains the authoritative feature specification and updates operational contracts in the reference when behavior changes.
