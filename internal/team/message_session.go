package team

func messageSessionAllows(status, kind string) bool {
	switch status {
	case "deliberating", "plan_proposed", "paused_for_deliberation", "team_review":
		return true
	case "plan_accepted", "executing":
		return kind == "human"
	}
	return false
}
