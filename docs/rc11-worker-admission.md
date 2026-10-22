# RC11 Worker admission assurance

Worker admission must reject inactive Workspaces, including when system membership already exists. The Workspace status is rechecked within the task admission transaction. Regression testing must cover inactive status, missing Workspace records, and membership reuse.
