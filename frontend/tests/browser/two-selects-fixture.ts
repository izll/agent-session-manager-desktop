import { mount, tick } from 'svelte';
import Select from '../../src/lib/components/common/Select.svelte';

// Two dropdowns side by side, as in the task dialog's session and tab pickers
// — inside a panel that keeps its clicks to itself, as a dialog does so a
// click inside it is not taken for a click on the backdrop.
document.querySelector('.row')?.addEventListener('click', (e) => e.stopPropagation());
const options = (prefix: string) => Array.from({ length: 12 }, (_, i) => ({ value: `${prefix}${i}`, label: `${prefix} ${i}` }));
for (const [id, prefix] of [['first', 'Session'], ['second', 'Tab']] as const) {
  const target = document.getElementById(id);
  if (!target) throw new Error(`fixture target ${id} is missing`);
  mount(Select, { target, props: { value: `${prefix}0`, searchable: true, options: options(prefix) } });
}
await tick();
document.body.dataset.fixtureReady = 'true';
