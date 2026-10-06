package chatcommands

import "sort"

var commandRegistry = []CommandSpec{
	{Name: "queue", Usage: "/queue [prompt|list|pause|resume|clear|remove <n>|move <n> <n>]", Description: "Queue prompts to run after the current turn completes.", Category: CategoryFlow, Mutating: true},
	{Name: "steer", Usage: "/steer <guidance>", Description: "Inject guidance at the next safe boundary of the current turn.", Category: CategoryFlow, Mutating: true, RequiresRun: true},
	{Name: "busy", Usage: "/busy [queue|steer|interrupt]", Description: "Choose how normal messages behave while a turn is running.", Category: CategoryFlow, Mutating: true},
	{Name: "background", Aliases: []string{"bg"}, Usage: "/bg <prompt>", Description: "Start a separate child/background session.", Category: CategoryFlow, Mutating: true},
	{Name: "stop", Usage: "/stop", Description: "Request safe cancellation of the current turn.", Category: CategoryFlow, Mutating: true, RequiresRun: true},
	{Name: "retry", Usage: "/retry", Description: "Retry the previous turn when replay is safe.", Category: CategoryFlow, Mutating: true},
	{Name: "continue", Usage: "/continue", Description: "Continue from the previous assistant response.", Category: CategoryFlow, Mutating: true},
	{Name: "branch", Usage: "/branch [name]", Description: "Fork this conversation into a new session.", Category: CategorySession, Mutating: true},
	{Name: "new", Usage: "/new", Description: "Create a new chat session.", Category: CategorySession, Mutating: true},
	{Name: "title", Usage: "/title <name>", Description: "Rename the current chat session.", Category: CategorySession, Mutating: true},
	{Name: "sessions", Usage: "/sessions", Description: "List available chat sessions.", Category: CategorySession},
	{Name: "resume", Usage: "/resume <session>", Description: "Resume another chat session.", Category: CategorySession, Mutating: true},
	{Name: "goal", Usage: "/goal [objective]", Description: "Set or inspect the durable multi-turn goal.", Category: CategorySession, Mutating: true},
	{Name: "subgoal", Usage: "/subgoal <criterion>", Description: "Add a completion criterion to the active goal.", Category: CategorySession, Mutating: true},
	{Name: "status", Usage: "/status", Description: "Show current chat, task, agent, model, sandbox and queue state.", Category: CategoryObservability},
	{Name: "context", Usage: "/context", Description: "Show context-window usage and composition.", Category: CategoryObservability},
	{Name: "compress", Usage: "/compress", Description: "Request controlled conversation-context compression.", Category: CategorySession, Mutating: true},
	{Name: "usage", Usage: "/usage", Description: "Show token, context and runtime usage for this session.", Category: CategoryObservability},
	{Name: "cost", Usage: "/cost", Description: "Show monetary and provider-allowance usage.", Category: CategoryObservability},
	{Name: "model", Usage: "/model [name|auto]", Description: "Inspect or change the session model override.", Category: CategoryModel, Mutating: true},
	{Name: "next", Usage: "/next <model-or-agent>", Description: "Override routing for the next turn only.", Category: CategoryModel, Mutating: true},
	{Name: "agent", Usage: "/agent [name|auto]", Description: "Inspect or change the session agent/runtime override.", Category: CategoryModel, Mutating: true},
	{Name: "reasoning", Usage: "/reasoning [off|low|medium|high|extra-high|auto]", Description: "Set supported reasoning effort for this session.", Category: CategoryModel, Mutating: true},
	{Name: "route", Usage: "/route [explain]", Description: "Show the current inference route and fallbacks.", Category: CategoryObservability},
	{Name: "why", Usage: "/why [blocked|route|last]", Description: "Explain the most recent orchestration decision.", Category: CategoryObservability},
	{Name: "node", Usage: "/node", Description: "Show compute/node state relevant to this chat.", Category: CategoryObservability},
	{Name: "tools", Usage: "/tools [list|enable <tool>|disable <tool>]", Description: "Inspect or adjust session tools within existing policy.", Category: CategoryExecution, Mutating: true},
	{Name: "skills", Usage: "/skills [list|enable <skill>|disable <skill>]", Description: "Inspect or adjust available session skills.", Category: CategoryExecution, Mutating: true},
	{Name: "sandbox", Usage: "/sandbox [status|internet|files|reset]", Description: "Inspect or request changes to this session sandbox.", Category: CategoryExecution, Mutating: true},
	{Name: "egress", Usage: "/egress", Description: "Show effective internet, LAN and destination egress policy.", Category: CategoryObservability},
	{Name: "task", Usage: "/task [status|children|dependencies|pause|cancel]", Description: "Inspect or control the task associated with this chat.", Category: CategoryExecution, Mutating: true},
	{Name: "plan", Usage: "/plan [show|revise|objections]", Description: "Inspect or work with the governing Task/Team plan.", Category: CategoryExecution, Mutating: true},
	{Name: "diff", Usage: "/diff", Description: "Show workspace/repository changes attributable to this session.", Category: CategoryObservability},
	{Name: "evidence", Usage: "/evidence [latest]", Description: "Show evidence accumulated by the current task/session.", Category: CategoryObservability},
	{Name: "verify", Usage: "/verify [status|rerun]", Description: "Run or inspect independent completion verification.", Category: CategoryGovernance, Mutating: true},
	{Name: "checkpoint", Usage: "/checkpoint [create <name>|list|restore <id>]", Description: "Create, list or restore verified checkpoints.", Category: CategoryGovernance, Mutating: true},
	{Name: "attention", Usage: "/attention", Description: "Show approvals, blocks and unknown outcomes related to this chat.", Category: CategoryObservability},
	{Name: "trace", Usage: "/trace", Description: "Show the orchestration/execution trace for the current turn.", Category: CategoryObservability},
	{Name: "budget", Usage: "/budget [routes]", Description: "Show task budget, reservations and allowed paid routes.", Category: CategoryObservability},
	{Name: "approve", Usage: "/approve [id]", Description: "Approve an action the current user is authorized to approve.", Category: CategoryGovernance, Mutating: true},
	{Name: "deny", Usage: "/deny [id]", Description: "Deny a pending approval associated with this session.", Category: CategoryGovernance, Mutating: true},
	{Name: "approvals", Aliases: []string{"security"}, Usage: "/approvals [high|medium|low|status]", Description: "Set approval strictness for this chat. Medium is recommended.", Category: CategoryGovernance, Mutating: true},
	{Name: "yolo", Usage: "/yolo [on|off|status]", Description: "Auto-approve session actions the current user is already authorized to approve.", Category: CategoryGovernance, Mutating: true},
	{Name: "focus", Usage: "/focus [on|off]", Description: "Reduce UI/tool chatter and emphasize final results.", Category: CategorySession, Mutating: true},
	{Name: "verbose", Usage: "/verbose [on|off]", Description: "Show additional execution/tool detail for this chat.", Category: CategorySession, Mutating: true},
	{Name: "help", Usage: "/help [command]", Description: "List chat commands or explain one command.", Category: CategorySession},
}

func Commands() []CommandSpec {
	out := append([]CommandSpec(nil), commandRegistry...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Category == out[j].Category {
			return out[i].Name < out[j].Name
		}
		return out[i].Category < out[j].Category
	})
	return out
}

func Lookup(name string) (CommandSpec, bool) {
	for _, c := range commandRegistry {
		if c.Name == name {
			return c, true
		}
		for _, a := range c.Aliases {
			if a == name {
				return c, true
			}
		}
	}
	return CommandSpec{}, false
}
