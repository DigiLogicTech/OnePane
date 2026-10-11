# Feature policy

Effective execution permissions must be resolved from the Project through Workspace and Agent scopes. Unknown actions are denied by default. Runtime gateways must enforce these permissions before executing side effects.

Policy helper functions alone do not secure execution; enforcement belongs at runtime admission and tool invocation.
