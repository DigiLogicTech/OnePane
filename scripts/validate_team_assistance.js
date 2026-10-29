// Behavioral tests of the shipped request-specific human-input controller.
// These tests use bounded synthetic HTTP results; they do not establish browser,
// installed-platform, real model, or hardware acceptance.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

let submissionSequence = 0;
const context = vm.createContext({
  AbortController,
  crypto: {randomUUID: () => 'submission-' + (++submissionSequence)},
});
const source = fs.readFileSync('internal/webui/static/team-assistance.js', 'utf8');
vm.runInContext(source, context, {filename: 'team-assistance.js'});
const index = fs.readFileSync('internal/webui/static/index.html', 'utf8');
assert.ok(index.indexOf('/team-assistance.js') >= 0 && index.indexOf('/team-assistance.js') < index.indexOf('/team-room.js'), 'shipped Team room did not load assistance module first');
const {controller, validRequest, validAnswer} = context.OnePaneTeamAssistance;
assert.equal(typeof controller, 'function');
const clone = value => JSON.parse(JSON.stringify(value));
const room = () => ({
  id: 'session', workspace_id: 'tenant', task_id: 'root',
  accepted_plan_id: 'plan', execution_mode: 'team', status: 'executing',
  task_state: 'waiting_dependency', task_revision: 9, execution_paused: false,
  members: [{id: 'member', kind: 'agent', name: 'Builder'}],
  assignments: [{task_id: 'child', key: 'build', member_id: 'member'}],
});
const request = () => ({
  request_id: 'request', workspace_id: 'tenant', session_id: 'session',
  plan_id: 'plan', assignment_key: 'build', member_id: 'member',
  member_name: '<img src=x onerror="throw 1">', parent_task_id: 'root',
  task_id: 'child', attempt_id: 'attempt', run_id: 'run', wait_step_id: 'wait',
  notice_message_id: 'notice', author_principal_id: 'system:agent-worker',
  question: '<script>throw new Error("not executable")</script> Which approach?',
  project_id: 'project', project_workspace_id: 'named', artifact_session_id: '',
  task_state: 'waiting_approval', task_revision: 7, worker_revision: 4, created_at: 100,
});
const answer = (r = request(), extra = {}) => ({
  answer_id: 'answer', request_id: r.request_id, author_principal_id: 'human',
  idempotency_key: 'saved-submission', text: 'Use the reviewed approach.',
  task_revision: r.task_revision, worker_revision: r.worker_revision, created_at: 101,
  ...extra,
});
const receipt = (r, a) => ({
  continuation_id: 'continued', request_id: r.request_id, answer_id: a.answer_id,
  actor_principal_id: 'approver', continue_step_id: 'continued-step',
  task_revision: r.task_revision, worker_revision: r.worker_revision, created_at: 102,
});
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return {promise, resolve, reject};
};

// Deliberately small DOM double: the renderer must use textContent for all
// authored text. Any innerHTML write fails instead of pretending to sanitize.
function fakeDocument() {
  const document = {activeElement: null};
  class Element {
    constructor(tag) {
      this.tagName = tag.toUpperCase(); this.children = []; this.parentElement = null;
      this.dataset = {}; this.attributes = {}; this._text = ''; this.value = '';
      this.selectionStart = 0; this.selectionEnd = 0; this.disabled = false;
    }
    set innerHTML(value) { throw Error('Assistance renderer used innerHTML: ' + value); }
    set textContent(value) { this._text = String(value); this.replaceChildren(); }
    get textContent() { return this._text + this.children.map(child => child.textContent).join(''); }
    setAttribute(key, value) { this.attributes[key] = String(value); }
    append(...children) { for (const child of children) { child.parentElement = this; this.children.push(child); } }
    replaceChildren(...children) { for (const child of this.children) child.parentElement = null; this.children = []; this.append(...children); }
    remove() { if (this.parentElement) this.parentElement.children = this.parentElement.children.filter(child => child !== this); this.parentElement = null; }
    contains(target) { return this === target || this.children.some(child => child.contains(target)); }
    querySelectorAll(selector) { return this.children.flatMap(child => [...(child.tagName.toLowerCase() === selector ? [child] : []), ...child.querySelectorAll(selector)]); }
    focus() { document.activeElement = this; }
    setSelectionRange(start, end) { this.selectionStart = start; this.selectionEnd = end; }
    setCustomValidity(message) { this.validationMessage = message; }
    reportValidity() { return !this.validationMessage; }
  }
  document.createElement = tag => new Element(tag);
  return document;
}
const settle = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };

function fixture() {
  const calls = [], rows = [request()];
  let saved = null, alive = true, changes = 0;
  let handler = async (path, options) => {
    if (path.endsWith('/answers')) {
      const body = JSON.parse(options.body);
      saved = answer(rows[0], {text: body.text, idempotency_key: body.idempotency_key});
      return clone(saved);
    }
    if (path.endsWith('/answer')) return {request: clone(rows[0]), answer: clone(saved || answer(rows[0]))};
    if (path.endsWith('/continue')) {
      const result = receipt(rows[0], saved || answer(rows[0]));
      rows.length = 0;
      return result;
    }
    return {requests: clone(rows)};
  };
  const c = controller({
    api: async (path, options = {}) => {
      calls.push({path, options, body: options.body ? JSON.parse(options.body) : undefined});
      return handler(path, options);
    },
    taskID: 'root', workspaceID: 'tenant', alive: () => alive,
    onChange: () => { changes++; },
  });
  c.updateRoom(room());
  return {
    c, calls, rows,
    handler(next) { handler = next; },
    setAlive(value) { alive = value; },
    changes: () => changes,
    mutations: () => calls.filter(call => call.options.method === 'POST'),
  };
}

async function main() {
  {
    // Execute the actual central helper: status-aware controller behavior must
    // not depend on a mock API that invents status properties for production.
    const foundation = fs.readFileSync('internal/webui/static/app-foundation.js', 'utf8');
    const start = foundation.indexOf('async function apiRequest(');
    const end = foundation.indexOf('\napi.json=', start);
    assert.ok(start >= 0 && end > start, 'production apiRequest boundary missing');
    let response = {ok: true, status: 200, text: async () => '{"saved":true}'}, csrfReads = 0;
    const calls = [];
    const apiContext = vm.createContext({
      fetch: async (path, options) => { calls.push({path, options}); return response; },
      csrfCookie: () => { csrfReads++; return 'existing-csrf-token'; },
    });
    vm.runInContext(foundation.slice(start, end), apiContext, {filename: 'production-apiRequest.js'});
    const actual = apiContext.apiRequest;
    assert.deepEqual(clone(await actual('/v1/team-sessions/session/assistance')), {saved: true});
    assert.equal(calls[0].options.credentials, 'same-origin');
    assert.equal(calls[0].options.method, 'GET');
    assert.equal(Object.hasOwn(calls[0].options.headers, 'X-OnePane-CSRF'), false);
    assert.equal(csrfReads, 0, 'read acquired mutation CSRF');
    const signal = new AbortController().signal;
    await actual('/v1/team-sessions/session/assistance/request/answers', {method: 'post', body: '{"text":"Reviewed"}', headers: {'X-Existing': 'kept'}, signal});
    const posted = calls.at(-1);
    assert.equal(posted.options.method, 'POST');
    assert.equal(posted.options.credentials, 'same-origin');
    assert.equal(posted.options.headers['X-OnePane-CSRF'], 'existing-csrf-token');
    assert.equal(posted.options.headers['content-type'], 'application/json');
    assert.equal(posted.options.headers['X-Existing'], 'kept');
    assert.equal(posted.options.body, '{"text":"Reviewed"}');
    assert.equal(posted.options.signal, signal);
    assert.equal(csrfReads, 1);
    response = {ok: false, status: 404, text: async () => '{"error":"No saved reply"}'};
    await assert.rejects(actual('/v1/team-sessions/session/assistance/request/answer'), error => error.status === 404 && error.message === 'No saved reply');
    response = {ok: false, status: 403, text: async () => '{"message":"Access revoked"}'};
    await assert.rejects(actual('/v1/team-sessions/session/assistance/request/answer'), error => error.status === 403 && error.message === 'Access revoked');
    response = {ok: false, status: 503, text: async () => 'Service unavailable'};
    await assert.rejects(actual('/v1/team-sessions/session/assistance'), error => error.status === 503 && error.message === 'Service unavailable');
    response = {ok: true, status: 204, text: async () => ''};
    assert.deepEqual(clone(await actual('/v1/team-sessions/session/assistance')), {});
  }
  assert.equal(validRequest(request(), room()), true, 'authored markup is inert data');
  assert.equal(validAnswer(answer(), request()), true);
  const invalidRequests = [
    {...request(), workspace_id: 'other'}, {...request(), session_id: 'other'},
    {...request(), parent_task_id: 'other'}, {...request(), plan_id: 'other'},
    {...request(), assignment_key: 'other'}, {...request(), member_id: 'other'},
    {...request(), task_id: 'other'}, {...request(), author_principal_id: 'human'},
    {...request(), request_id: 'x'.repeat(257)}, {...request(), request_id: 'x\u0000'},
    {...request(), question: '😀'.repeat(129)}, {...request(), question: '  '},
    {...request(), question: '\ud800'}, {...request(), member_name: 'x'.repeat(257)},
    {...request(), task_revision: Number.MAX_SAFE_INTEGER + 1},
    {...request(), worker_revision: 0}, {...request(), task_state: 'running'},
    {...request(), project_id: '', project_workspace_id: 'named'},
    {...request(), unexpected: 'private surprise'},
  ];
  for (const r of invalidRequests) assert.equal(validRequest(r, room()), false, 'invalid request admitted: ' + JSON.stringify(r));
  for (const a of [
    {...answer(), request_id: 'other'}, {...answer(), task_revision: 8},
    {...answer(), worker_revision: 5}, {...answer(), text: '😀'.repeat(1025)},
    {...answer(), text: ' '}, {...answer(), idempotency_key: 'x'.repeat(129)},
    {...answer(), unexpected: 'field'},
  ]) assert.ok(!validAnswer(a, request()), 'invalid retained answer admitted');

  {
    const f = fixture();
    assert.equal(await f.c.refresh(), true);
    const entry = f.c.state.requests[0];
    assert.equal(entry.request.question, request().question, 'controller interpreted markup');
    assert.equal(f.c.setDraft('request', 'Unsaved reviewed answer'), true);
    f.rows[0] = {...f.rows[0], task_revision: 8, worker_revision: 5, task_state: 'paused'};
    await f.c.refresh();
    assert.equal(f.c.state.requests[0].draft, 'Unsaved reviewed answer', 'poll/hold lost draft');
    assert.equal(f.c.state.requests[0].request.task_revision, 8);
    assert.equal(f.c.setDraft('request', '😀'.repeat(1025)), false, 'UTF-8 byte budget bypassed');
    assert.equal(f.c.state.requests[0].draft, 'Unsaved reviewed answer');
    assert.equal(f.c.setDraft('foreign', 'forged draft'), false);
    f.c.updateRoom({...room(), accepted_plan_id: 'replacement'});
    assert.equal(f.c.state.requests.length, 0, 'plan replacement retained private draft');
    f.c.updateRoom({...room(), workspace_id: 'foreign'});
    assert.equal(f.c.state.room, null, 'foreign Workspace installed');
  }

  {
    const f = fixture();
    await f.c.refresh();
    f.c.setDraft('request', '<b>Human-authored answer</b>');
    assert.equal(await f.c.continueRequest('request'), false, 'unanswered request continued');
    assert.equal(await f.c.save('request'), true);
    assert.equal(f.mutations().length, 1, 'reply automatically continued execution');
    const write = f.mutations()[0];
    assert.equal(write.path, '/v1/team-sessions/session/assistance/request/answers');
    assert.deepEqual(write.body, {
      task_id: 'child', attempt_id: 'attempt', run_id: 'run', wait_step_id: 'wait',
      expected_task_revision: 7, expected_worker_revision: 4,
      text: '<b>Human-authored answer</b>', idempotency_key: write.body.idempotency_key,
    });
    assert.ok(write.body.idempotency_key);
    assert.equal(Object.hasOwn(write.body, 'author_principal_id'), false);
    assert.equal(f.c.state.requests[0].answer.text, write.body.text);
    assert.equal(await f.c.save('request'), false, 'final answer submitted again');
    assert.equal(await f.c.continueRequest('request'), true);
    assert.deepEqual(f.mutations()[1].body, {
      task_id: 'child', attempt_id: 'attempt', run_id: 'run', wait_step_id: 'wait',
      expected_task_revision: 7, expected_worker_revision: 4, answer_id: 'answer',
    });
    assert.equal(f.c.state.requests.length, 0);
    assert.equal(await f.c.continueRequest('request'), false, 'committed continuation replayed');
  }

  {
    const f = fixture();
    await f.c.refresh();
    const pending = deferred();
    f.handler(async (path, opts) => {
      if (path.endsWith('/answers')) {
        await pending.promise;
        const body = JSON.parse(opts.body);
        return answer(request(), {text: body.text, idempotency_key: body.idempotency_key});
      }
      return {requests: clone(f.rows)};
    });
    f.c.setDraft('request', 'First answer');
    const saving = f.c.save('request');
    assert.equal(await f.c.save('request'), false, 'duplicate in-flight reply dispatched');
    assert.equal(f.mutations().length, 1);
    f.c.setDraft('request', 'Newer draft typed while saving');
    await f.c.refresh();
    pending.resolve();
    assert.equal(await saving, true);
    assert.equal(f.c.state.requests[0].draft, 'Newer draft typed while saving', 'save completion erased newer draft');
    const continuation = deferred();
    f.handler(() => continuation.promise);
    const continuing = f.c.continueRequest('request');
    assert.equal(await f.c.continueRequest('request'), false, 'duplicate in-flight Continue dispatched');
    continuation.resolve(receipt(request(), answer()));
    assert.equal(await continuing, true);
    assert.equal(f.mutations().length, 2);
  }

  for (const hold of ['execution_paused', 'pause_draining', 'pause_reconciliation_required']) {
    const f = fixture(); await f.c.refresh(); await f.c.loadAnswer('request');
    f.c.updateRoom({...room(), [hold]: true});
    assert.equal(await f.c.continueRequest('request'), false, hold + ' released by Continue');
    assert.equal(f.mutations().length, 0);
  }

  for (const value of [
    {requests: [request(), request()]}, {requests: Array.from({length: 9}, request)},
    {requests: [{...request(), attempt_id: ''}]}, {requests: [{...request(), workspace_id: 'foreign'}]},
    {requests: [request()], extra: 'unreviewed'}, {requests: null},
  ]) {
    const f = fixture(); await f.c.refresh(); f.c.setDraft('request', 'Sensitive draft');
    f.handler(async () => value); await f.c.refresh();
    assert.equal(f.c.state.requests.length, 0, 'malformed list retained private input');
    assert.ok(f.c.state.error);
  }

  {
    const f = fixture(); await f.c.refresh(); f.c.setDraft('request', 'Private draft');
    f.handler(async () => { throw Error('403 access revoked'); });
    await f.c.refresh();
    assert.equal(f.c.state.requests.length, 0, 'denied list retained private input');
    assert.ok(f.c.state.error);
  }
  for (const change of ['foreign_answer', 'foreign_owner', 'denied']) {
    const f = fixture(); await f.c.refresh(); f.c.setDraft('request', 'Private draft');
    f.handler(async () => {
      if (change === 'denied') throw Error('Access revoked');
      return {
        request: {...request(), ...(change === 'foreign_owner' ? {attempt_id: 'replacement'} : {})},
        answer: {...answer(), ...(change === 'foreign_answer' ? {request_id: 'foreign'} : {})},
      };
    });
    assert.equal(await f.c.loadAnswer('request'), false);
    assert.equal(f.c.state.requests.length, 0, 'invalid/denied answer retained private input');
  }

  for (const end of ['scope', 'plan', 'stop', 'detached']) {
    const f = fixture(); await f.c.refresh(); f.c.setDraft('request', 'Private draft');
    const pending = deferred(); f.handler(() => pending.promise);
    const reading = f.c.refresh();
    if (end === 'scope') f.c.updateRoom(null);
    if (end === 'plan') f.c.updateRoom({...room(), accepted_plan_id: 'replacement'});
    if (end === 'stop') f.c.stop();
    if (end === 'detached') { f.setAlive(false); f.c.stop(); }
    const changes = f.changes();
    pending.resolve({requests: [request()]});
    assert.equal(await reading, false);
    assert.equal(f.c.state.requests.length, 0, 'late read revived closed/replaced input: ' + end);
    assert.equal(f.changes(), changes, 'late read emitted a stale projection');
  }
  {
    const f = fixture(); await f.c.refresh();
    f.c.setDraft('request', 'Keep this newer observation');
    const pending = deferred();
    f.handler(path => path.endsWith('/assistance') ? pending.promise : {request: {...request(), task_revision: 8, worker_revision: 5, task_state: 'paused'}, answer: answer()});
    const polling = f.c.refresh();
    assert.equal(await f.c.loadAnswer('request'), true);
    pending.resolve({requests: [request()]});
    assert.equal(await polling, false, 'old list replaced a newer retained-answer observation');
    assert.equal(f.c.state.requests[0].request.task_revision, 8);
    assert.equal(f.c.state.requests[0].request.task_state, 'paused');
    assert.equal(f.c.state.requests[0].draft, 'Keep this newer observation');
    f.handler(() => ({requests: [request()]}));
    assert.equal(await f.c.refresh(), false, 'later stale list regressed retained revisions');
    assert.equal(f.c.state.requests[0].request.task_revision, 8);
  }
  for (const action of ['save', 'loadAnswer', 'continueRequest']) {
    const f = fixture(); await f.c.refresh();
    if (action === 'save') f.c.setDraft('request', 'Private answer');
    if (action === 'continueRequest') await f.c.loadAnswer('request');
    const pending = deferred(); f.handler(() => pending.promise);
    const writing = f.c[action]('request');
    const signal = f.calls.at(-1).options.signal;
    f.c.stop(); assert.equal(signal.aborted, true, 'dispose did not cancel pending operation');
    pending.resolve(action === 'loadAnswer' ? {request: request(), answer: answer()} : action === 'save' ? answer() : receipt(request(), answer()));
    assert.equal(await writing, false);
    assert.equal(f.c.state.requests.length, 0, 'late mutation restored private data');
    assert.equal(f.c.state.room, null);
    const count = f.calls.length; await f.c.refresh();
    assert.equal(f.calls.length, count, 'disposed controller still polled');
  }
  for (const action of ['save', 'loadAnswer', 'continueRequest']) {
    const f = fixture(); await f.c.refresh();
    if (action === 'save') f.c.setDraft('request', 'Original owner answer');
    if (action === 'continueRequest') await f.c.loadAnswer('request');
    const pending = deferred();
    f.handler(async path => path.endsWith('/assistance') ? {requests: [{...request(), attempt_id: 'replacement', run_id: 'replacement-run', wait_step_id: 'replacement-wait'}]} : pending.promise);
    const writing = f.c[action]('request');
    await f.c.refresh();
    pending.resolve(action === 'loadAnswer' ? {request: request(), answer: answer()} : action === 'save' ? answer() : receipt(request(), answer()));
    assert.equal(await writing, false, 'late response admitted after owner replacement');
    assert.equal(f.c.state.requests[0].request.attempt_id, 'replacement');
    assert.equal(f.c.state.requests[0].answer, null, 'late response contaminated replacement owner');
    assert.equal(f.c.state.requests[0].draft, '', 'replacement owner inherited original private draft');
  }
  {
    const f = fixture(); await f.c.refresh();
    const pending = deferred(); f.handler(() => pending.promise);
    const first = f.c.refresh();
    const before = f.calls.length;
    assert.equal(await f.c.refresh(), false, 'duplicate poll admitted');
    assert.equal(f.calls.length, before);
    pending.resolve({requests: clone(f.rows)}); assert.equal(await first, true);
  }
  {
    const f = fixture(); await f.c.refresh();
    f.c.setDraft('request', 'Retry the exact original submission');
    f.handler(async () => { throw Error('Response interrupted after possible commit'); });
    assert.equal(await f.c.save('request'), false);
    const first = clone(f.mutations()[0].body);
    assert.equal(await f.c.save('request'), false);
    assert.deepEqual(f.mutations()[1].body, first, 'unchanged retry lost immutable submission identity');
    assert.equal(f.c.state.requests[0].answer, null, 'failed save fabricated a retained answer');
    assert.equal(f.c.state.requests[0].draft, first.text, 'failed save erased authored draft');
    assert.equal(await f.c.continueRequest('request'), false, 'unconfirmed reply enabled Continue');
    assert.equal(f.mutations().length, 2);
  }
  {
    const f = fixture(); await f.c.refresh();
    f.c.setDraft('request', 'Retain draft after a real missing-answer response');
    f.handler(async () => { throw Object.assign(Error('Missing'), {status: 404}); });
    assert.equal(await f.c.loadAnswer('request'), false);
    assert.equal(f.c.state.requests.length, 1, '404 incorrectly erased the current question');
    assert.equal(f.c.state.requests[0].draft, 'Retain draft after a real missing-answer response');
    assert.equal(f.c.state.requests[0].answer, null);
    assert.match(f.c.state.requests[0].error, /No saved reply yet/);
    assert.equal(f.c.state.busy, false);
    f.handler(async () => { throw Object.assign(Error('Denied'), {status: 403}); });
    await f.c.loadAnswer('request');
    assert.equal(f.c.state.requests.length, 0, '403 retained private question/draft');
  }
  {
    const f = fixture(); await f.c.refresh();
    f.c.setDraft('request', 'Same authored text after revisions advance');
    f.handler(async path => {
      if (path.endsWith('/assistance')) return {requests: clone(f.rows)};
      throw Error('Submission was not confirmed');
    });
    await f.c.save('request');
    const first = clone(f.mutations()[0].body);
    f.rows[0] = {...f.rows[0], task_revision: 8, worker_revision: 5};
    await f.c.refresh(); await f.c.save('request');
    const retry = f.mutations()[1].body;
    assert.equal(retry.expected_task_revision, 8);
    assert.equal(retry.expected_worker_revision, 5);
    assert.notEqual(retry.idempotency_key, first.idempotency_key, 'changed-owner retry retained stale submission identity');
    assert.equal(retry.text, first.text);
    assert.equal(f.c.state.requests[0].draft, first.text);
  }
  for (const replacement of ['assignment', 'member']) {
    const f = fixture(); await f.c.refresh(); f.c.setDraft('request', 'Private original-owner draft');
    const pending = deferred(); f.handler(() => pending.promise);
    const reading = f.c.refresh();
    const next = room();
    if (replacement === 'assignment') next.assignments[0] = {...next.assignments[0], task_id: 'replacement-child'};
    else {
      next.members[0] = {...next.members[0], id: 'replacement-member'};
      next.assignments[0] = {...next.assignments[0], member_id: 'replacement-member'};
    }
    f.c.updateRoom(next);
    assert.equal(f.c.state.requests.length, 0, 'same-plan binding change retained private data');
    pending.resolve({requests: [request()]});
    assert.equal(await reading, false);
    assert.equal(f.c.state.requests.length, 0, 'late old binding restored private data');
  }
  for (const replacement of ['assignment', 'member']) {
    const f = fixture(), sharedRoom = room();
    f.c.updateRoom(sharedRoom); await f.c.refresh();
    f.c.setDraft('request', 'Private original binding draft');
    const pending = deferred(); f.handler(() => pending.promise);
    const reading = f.c.refresh();
    if (replacement === 'assignment') sharedRoom.assignments[0].task_id = 'replacement-child';
    else {
      sharedRoom.members[0].id = 'replacement-member';
      sharedRoom.assignments[0].member_id = 'replacement-member';
    }
    assert.equal(f.c.state.room.assignments[0].task_id, 'child', 'external room mutation changed retained Task snapshot');
    assert.equal(f.c.state.room.members[0].id, 'member', 'external room mutation changed retained member snapshot');
    assert.equal(f.c.state.room.assignments[0].member_id, 'member', 'external room mutation changed retained assignment snapshot');
    f.c.updateRoom(sharedRoom);
    assert.equal(f.c.state.requests.length, 0, 'in-place binding replacement retained private draft');
    pending.resolve({requests: [request()]});
    assert.equal(await reading, false);
    assert.equal(f.c.state.requests.length, 0, 'in-place binding replacement revived late original-owner read');
  }
  {
    const f = fixture(); await f.c.refresh();
    f.c.setDraft('request', 'Keep this draft under the revised pause');
    const pending = deferred();
    f.handler(path => path.endsWith('/assistance') ? {requests: clone(f.rows)} : pending.promise);
    const loading = f.c.loadAnswer('request');
    f.rows[0] = {...f.rows[0], task_revision: 8, worker_revision: 5, task_state: 'paused'};
    assert.equal(await f.c.refresh(), true);
    pending.resolve({request: request(), answer: answer()});
    assert.equal(await loading, false, 'late saved-answer read replaced newer owner revisions/state');
    assert.equal(f.c.state.requests.length, 1, 'stale observation erased otherwise authorized current draft');
    const entry = f.c.state.requests[0];
    assert.equal(entry.request.task_revision, 8);
    assert.equal(entry.request.worker_revision, 5);
    assert.equal(entry.request.task_state, 'paused');
    assert.equal(entry.draft, 'Keep this draft under the revised pause');
    assert.equal(entry.answer, null);
    assert.match(entry.error, /Task changed while loading/);
    assert.equal(f.c.state.busy, false);
    assert.equal(await f.c.continueRequest('request'), false);
    f.handler(async () => ({request: clone(f.rows[0]), answer: answer()}));
    assert.equal(await f.c.loadAnswer('request'), true, 'fresh retained answer could not be reloaded');
    assert.equal(f.c.state.requests[0].answer.answer_id, 'answer');
    assert.equal(f.c.state.requests[0].request.task_revision, 8);
    assert.equal(f.c.state.requests[0].request.worker_revision, 5);
    assert.equal(f.c.state.requests[0].request.task_state, 'paused');
    assert.equal(await f.c.continueRequest('request'), false, 'loading saved reply released a newer pause');
  }
  {
    const f = fixture(); await f.c.refresh(); await f.c.loadAnswer('request');
    const read = deferred(), approval = deferred();
    f.handler(path => path.endsWith('/assistance') ? read.promise : approval.promise);
    const polling = f.c.refresh(), continuing = f.c.continueRequest('request');
    approval.resolve(receipt(request(), answer()));
    assert.equal(await continuing, true);
    read.resolve({requests: [request()]});
    assert.equal(await polling, false, 'pre-Continue poll revived completed request');
    assert.equal(f.c.state.requests.length, 0);
  }

  {
    const document = fakeDocument();
    const mountedContext = vm.createContext({document, AbortController, crypto: {randomUUID: () => 'mounted-submission'}});
    vm.runInContext(source, mountedContext, {filename: 'team-assistance.js'});
    const host = document.createElement('main'), rows = [request()], calls = [];
    let pendingSave = null;
    const api = async (path, options = {}) => {
      calls.push({path, options});
      if (path.endsWith('/answers')) {
        await pendingSave.promise;
        const body = JSON.parse(options.body);
        return answer(rows[0], {text: body.text, idempotency_key: body.idempotency_key});
      }
      if (path.endsWith('/continue')) return receipt(rows[0], answer(rows[0]));
      if (path.endsWith('/answer')) throw Object.assign(Error('Missing'), {status: 404});
      return {requests: clone(rows)};
    };
    const mounted = mountedContext.OnePaneTeamAssistance.mount(host, {}, {api, taskID: 'root', workspaceID: 'tenant', alive: () => true});
    assert.equal(calls.length, 0, 'mount dispatched an unsolicited read or mutation');
    mounted.draw({room: room()}); await settle();
    assert.equal(calls.length, 1);
    assert.ok(calls.every(call => !call.options.method), 'initial render mutated execution');
    assert.match(host.textContent, /Needs your input/);
    assert.ok(host.textContent.includes(request().question), 'authored question was altered or omitted');
    assert.ok(host.textContent.includes(request().member_name));
    assert.equal(host.querySelectorAll('script').length, 0, 'authored text created script elements');
    const button = text => host.querySelectorAll('button').find(node => node.textContent === text);
    assert.ok(button('Save reply'));
    assert.ok(button('Check for saved reply'));
    assert.equal(button('Continue original task'), undefined, 'unsaved reply showed Continue');
    let input = host.querySelectorAll('textarea')[0];
    input.value = '<img src=x onerror="throw 1"> Retained draft'; input.oninput();
    input.focus(); input.setSelectionRange(3, 12);
    mounted.draw({room: room()}); await settle();
    assert.equal(host.querySelectorAll('textarea')[0], input, 'ordinary poll replaced active editor');
    assert.equal(document.activeElement, input);
    rows[0] = {...rows[0], task_revision: 8};
    mounted.draw({room: room()}); await settle();
    const revised = host.querySelectorAll('textarea')[0];
    assert.notEqual(revised, input, 'revision change did not refresh card metadata');
    assert.equal(revised.value, input.value, 'revision rebuild lost draft');
    assert.equal(document.activeElement, revised, 'revision rebuild lost focus');
    assert.equal(revised.selectionStart, 3); assert.equal(revised.selectionEnd, 12);
    pendingSave = deferred();
    button('Save reply').onclick(); await settle();
    assert.equal(host.querySelectorAll('textarea')[0].disabled, true);
    assert.ok(host.querySelectorAll('button').every(node => node.disabled), 'pending action left duplicate controls enabled');
    assert.equal(calls.filter(call => call.options.method === 'POST').length, 1);
    pendingSave.resolve(); await settle();
    assert.match(host.textContent, /Saved human reply/);
    assert.ok(host.textContent.includes(input.value), 'saved human input missing from text rendering');
    assert.equal(host.querySelectorAll('textarea').length, 0);
    assert.ok(button('Continue original task'));
    assert.equal(calls.filter(call => call.path.endsWith('/continue')).length, 0, 'saving clicked Continue implicitly');
    mounted.draw({room: {...room(), execution_paused: true}}); await settle();
    assert.equal(button('Continue original task').disabled, true, 'paused Team retained an enabled Continue');
    mounted.dispose();
    assert.equal(host.children.length, 0, 'disposed mount retained private rendering');
    const before = calls.length; mounted.draw({room: room()}); await settle();
    assert.equal(calls.length, before, 'disposed mount refreshed');
  }
  console.log('Team assistance controller and mounted renderer: scope, UTF-8 bounds, drafts/focus, explicit approval, duplicate suppression, denial, escaping and disposal PASS');
}

main().catch(error => {console.error(error); process.exitCode = 1;});
