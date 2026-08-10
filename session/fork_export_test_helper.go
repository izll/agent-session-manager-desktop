package session

// ForkArgsForTest exposes appendForkArgs to tests in other packages.
//
// The fork arguments are the whole feature — get them wrong and the agent
// either refuses to start or, worse, continues the original conversation
// instead of branching it. They are worth testing from where the capability
// list is also checked, and that lives in package main.
func ForkArgsForTest(config AgentConfig, args []string, sourceID string) []string {
	return appendForkArgs(config, args, sourceID)
}
