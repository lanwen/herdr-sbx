# Project guidance

Before changing setup, behavior, or tests, read [the project reference](docs/reference.md).
Use public documentation and exposed API contracts in repository content and commit messages.
Run tests with isolated Herdr sessions and sandbox fixtures.

## Focused agents

The main agent owns the specification, requirements alignment, task scope, file assignments, integration, and the final response.
Delegate implementation, code edits, tests, visual design, and domain review using the [shared agent roles](.agents/roles/README.md), including for small code changes.
The main agent must delegate repository inspection, including source, tests, documentation, diffs, and codedb queries, to the [clerk](.agents/roles/clerk.md) role and make decisions from its findings, requesting focused follow-ups when evidence is missing.
Except for mandatory bootstrap instructions, the main agent must not read repository files directly or use another tool to bypass this boundary.
Codex configurations in `.codex/agents/` load those roles; other agents can read the same Markdown instructions.
For assignments, contract changes, reviews, and verification handoffs, follow [the development workflow](docs/development-workflow.md).
The main agent maintains the authoritative feature specification and updates operational contracts in the reference when behavior changes.
