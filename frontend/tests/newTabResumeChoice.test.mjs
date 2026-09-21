import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const dialog = readFileSync(
  new URL('../src/lib/components/Dialogs/NewTabDialog.svelte', import.meta.url), 'utf8');
const picker = readFileSync(
  new URL('../src/lib/components/Dialogs/ResumeSessionPickerDialog.svelte', import.meta.url), 'utf8');

// The conversation is chosen in the picker, not in a dropdown: a one-line
// control cannot show a prompt and its time, which is what makes one
// conversation tellable from another.
test('the new-tab dialog opens the resume picker rather than listing inline', () => {
  assert.match(dialog, /<ResumeSessionPickerDialog/,
    'the new-tab dialog must reuse the picker that resumes an existing tab');
  assert.match(dialog, /bind:show=\{showResumePicker\}/);
  assert.doesNotMatch(dialog, /options=\{resumeChoices\}/,
    'the inline dropdown must be gone, not merely hidden');
});

// The picker is given the target directly: there is no tab yet, so there is no
// session for it to read the agent and path from.
test('the picker is told which agent, directory and machine to list', () => {
  const mount = dialog.slice(dialog.indexOf('<ResumeSessionPickerDialog'));
  assert.match(mount, /agentOverride=\{selectedAgent\}/);
  assert.match(mount, /pathOverride=\{resumeWorkDir\}/);
  assert.match(mount, /serverId=\{effectiveServerId\}/,
    'a tab bound for a server must list that server, not this computer');
});

// A conversation belongs to one agent, in one directory, on one machine.
// Changing any of those changes which conversations exist, so a choice made
// before must not be carried into a tab it does not belong to — that would
// start the agent on a conversation the picker never offered for it.
test('changing the agent, machine or directory clears the chosen conversation', () => {
  assert.match(dialog, /\$: resumeContext = `\$\{effectiveServerId\}\|\$\{selectedAgent\}\|\$\{resumeWorkDir\}`/);
  const guard = dialog.slice(dialog.indexOf('if (resumeContext !== lastResumeContext)'));
  assert.match(guard.slice(0, 200), /resumeId = ''/,
    'a stale conversation id must be dropped when its context changes');
});

// Submitting must not send an id the field was not showing.
test('only a conversation the user could see is submitted', () => {
  assert.match(dialog, /resumeId: tabType === 'agent' && canResume \? resumeId : ''/);
});

// The picker reports what was chosen by, so the dialog can show it without
// holding a copy of the list.
test('the picker reports the label beside the id', () => {
  assert.match(picker, /dispatch\('select', \{ resumeId: chosen\.id, displayName: chosen\.displayName \}\)/);
  assert.match(picker, /dispatch\('select', \{ resumeId: '', displayName: '' \}\)/,
    'starting fresh must clear the label too, not leave the previous one showing');
});

// The picker serves two callers now. The existing one passes a session; the
// new-tab dialog has none, and must still be able to list.
test('the picker works without a session, for a tab that does not exist yet', () => {
  assert.match(picker, /show && \(session \|\| pathOverride\)/,
    'the picker must load when given a path even with no session');
  assert.match(picker, /const agent = agentOverride \|\| session\?\.agent \|\| ''/);
  assert.match(picker, /App\.GetResumeSessionsOn\(/,
    'the picker must ask the machine the conversations are on');
});
