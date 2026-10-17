package featurepolicy

// AuthorizeExecution evaluates an action against the effective policy of all scopes.
func AuthorizeExecution(project Policy, action string, descendants ...Policy) bool {
	return ResolvePolicyChain(project, descendants...).ValidateExecution(action)
}
