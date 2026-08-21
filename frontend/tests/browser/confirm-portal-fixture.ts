import { mount } from 'svelte';
import ConfirmDialog from '../../src/lib/components/Dialogs/ConfirmDialog.svelte';

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
document.getElementById('dialog-trigger')?.focus();
mount(ConfirmDialog, {
  target,
  props: {
    show: true,
    title: 'Unsaved file',
    message: 'Discard the editor buffer?',
    confirmText: 'Discard',
    cancelText: 'Keep editing',
    variant: 'warning',
  },
  events: {
    confirm: () => { document.body.dataset.confirmed = 'true'; },
    cancel: () => { document.body.dataset.cancelled = 'true'; },
  },
});
