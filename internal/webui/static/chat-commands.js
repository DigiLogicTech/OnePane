export const CHAT_COMMANDS = [
  ['queue','Flow','Queue a prompt after the current turn','/queue <prompt>'],
  ['steer','Flow','Guide the active turn at its next safe boundary','/steer <guidance>'],
  ['busy','Flow','Set busy-message behavior','/busy queue|steer|interrupt'],
  ['bg','Flow','Start a child/background session','/bg <prompt>'],
  ['stop','Flow','Stop the active turn safely','/stop'],
  ['retry','Flow','Retry the previous turn when safe','/retry'],
  ['continue','Flow','Continue the previous assistant response','/continue'],
  ['branch','Session','Fork this conversation','/branch [name]'],
  ['new','Session','Create a new chat','/new'],
  ['title','Session','Rename this chat','/title <name>'],
  ['sessions','Session','List sessions','/sessions'],
  ['resume','Session','Resume another session','/resume <session>'],
  ['goal','Session','Set or inspect the standing goal','/goal [objective]'],
  ['subgoal','Session','Add a goal completion criterion','/subgoal <criterion>'],
  ['status','Observability','Show chat/task/model/agent/queue state','/status'],
  ['context','Observability','Show context-window usage','/context'],
  ['compress','Session','Compress old conversational context','/compress'],
  ['usage','Observability','Show token/context/runtime usage','/usage'],
  ['cost','Observability','Show monetary/provider allowance usage','/cost'],
  ['model','Model','Inspect or switch the session model','/model [name|auto]'],
  ['next','Model','One-turn routing override','/next <model-or-agent>'],
  ['agent','Model','Inspect or switch the session agent/runtime','/agent [name|auto]'],
  ['reasoning','Model','Set reasoning effort','/reasoning <level>'],
  ['route','Observability','Show inference route and fallbacks','/route [explain]'],
  ['why','Observability','Explain the latest orchestration decision','/why [blocked|route|last]'],
  ['node','Observability','Show compute state for this chat','/node'],
  ['tools','Execution','Inspect or adjust session tools within policy','/tools'],
  ['skills','Execution','Inspect or adjust session skills','/skills'],
  ['sandbox','Execution','Inspect or request sandbox changes','/sandbox'],
  ['egress','Observability','Show effective network egress','/egress'],
  ['task','Execution','Inspect/control the associated task','/task'],
  ['plan','Execution','Inspect/revise the governing plan','/plan'],
  ['diff','Observability','Show workspace/repository changes','/diff'],
  ['evidence','Governance','Show evidence for the current task','/evidence'],
  ['verify','Governance','Run/inspect independent verification','/verify'],
  ['checkpoint','Governance','Create/list/restore checkpoints','/checkpoint'],
  ['attention','Observability','Show approvals, blocks and unknown outcomes','/attention'],
  ['trace','Observability','Show this turn orchestration trace','/trace'],
  ['budget','Observability','Show budget and allowed routes','/budget'],
  ['approve','Governance','Approve an action you are authorized to approve','/approve [id]'],
  ['deny','Governance','Deny a pending approval','/deny [id]'],
  ['approvals','Governance','Set chat approval strictness (Medium recommended)','/approvals high|medium|low|status'],
  ['yolo','Governance','Auto-approve actions you are already authorized to approve','/yolo on|off|status'],
  ['focus','Session','Reduce tool/execution chatter','/focus on|off'],
  ['verbose','Session','Show additional execution detail','/verbose on|off'],
  ['help','Session','List or explain chat commands','/help [command]']
].map(([name,category,description,usage])=>({name,category,description,usage}));

export function suggestChatCommands(value){
  const v=String(value||'').trimStart();
  if(!v.startsWith('/')) return [];
  const needle=v.slice(1).split(/\s/,1)[0].toLowerCase();
  return CHAT_COMMANDS.filter(c=>c.name.startsWith(needle) || c.description.toLowerCase().includes(needle)).slice(0,10);
}

export function parseChatCommand(value){
  const raw=String(value||'').trim();
  if(!raw.startsWith('/')) return null;
  const m=raw.match(/^\/(\S+)(?:\s+([\s\S]*))?$/);
  if(!m) return {name:'help',args:''};
  return {name:m[1].toLowerCase(),args:(m[2]||'').trim(),raw};
}
