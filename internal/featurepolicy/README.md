# Feature policy

Effective execution permissions must be resolved from the Project through Workspace and Agent scopes. Unknown actions are denied by default. Runtime gateways must enforce these permissions before executing side effects.

Policy helper functions alone do not secure execution; enforcement belongs at runtime admission and tool invocation.

## QA checks

Verify inherited cloud routing is denied unless explicitly granted. Verify a disabled Workspace cannot admit an Agent Worker, even when system membership already exists. Verify that no Agent or execution override can restore a denied parent permission. Verify unknown tool actions are rejected and do not execute side effects.
