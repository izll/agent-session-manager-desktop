import { mount } from 'svelte';
import Select from '../../src/lib/components/common/Select.svelte';

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');

mount(Select, {
  target,
  props: {
    value: 'alpha',
    searchable: true,
    options: [
      { value: 'alpha', label: 'Alpha' },
      { value: 'beta', label: 'Beta' },
      { value: 'gamma', label: 'Gamma' },
    ],
  },
  events: {
    change: (event: CustomEvent<string>) => {
      document.body.dataset.selected = event.detail;
    },
  },
});
