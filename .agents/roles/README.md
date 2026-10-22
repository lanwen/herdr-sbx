# Focused agent roles

These files define reusable instructions for focused agents.
Repository paths in role instructions resolve from the repository root.

| Role | Instructions |
| --- | --- |
| Read-only repository research | [Clerk](clerk.md) |
| Go implementation and focused tests | [Go implementer](go_implementer.md) |
| Herdr and Docker Sandboxes contracts and review | [Herdr expert](herdr_expert.md) |
| Builds, isolated tests, evidence, and assigned commits | [Legwork](legwork.md) |
| Terminal flows, layout, input, and recovery | [TUI designer](tui_designer.md) |

Before work, read the repository's AGENTS.md and the assigned role file.
Supply that role path with a concrete task, owned files, relevant context, and completion criteria.
Any agent, including Claude, can follow a role when its caller explicitly supplies the role file.
Tool-specific adapters can load these shared instructions.
