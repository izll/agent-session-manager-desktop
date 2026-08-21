import { mount } from 'svelte';
import FeedbackFixture from './feedback-fixture.svelte';

const target = document.getElementById('fixture');
if (!target) throw new Error('feedback fixture target is missing');

mount(FeedbackFixture, {
  target,
  props: {
    onFixtureReady: () => { document.body.dataset.fixtureReady = 'true'; },
  },
});
