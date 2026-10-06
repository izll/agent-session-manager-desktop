import type { EditorView, Panel, ViewUpdate } from '@codemirror/view';
import type { EditorState } from '@codemirror/state';
import {
  SearchQuery,
  closeSearchPanel,
  findNext,
  findPrevious,
  getSearchQuery,
  replaceAll,
  replaceNext,
  setSearchQuery,
} from '@codemirror/search';
import { findKeyAction, matchCounter } from './diffFind';

// The find panel of the Files view's editor.
//
// CodeMirror's own panel has a look and a set of controls of its own — words
// for buttons, checkboxes, a bare × pinned to the corner — while the diff, the
// commit history and a rendered Markdown file share one find bar. This is that
// bar, built for CodeMirror: the same field, counter, arrows and close button,
// with the search itself left to CodeMirror. Its options become the same small
// buttons, and in the edit view a second row replaces.

/** Counting stops here: a count on a huge file is not worth a frozen pane. */
const MAX_COUNTED = 10_000;

type Translate = (key: string) => string;

function element<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Record<string, string>,
  children: (Node | string)[] = [],
): HTMLElementTagNameMap[K] {
  const el = document.createElement(tag);
  for (const [name, value] of Object.entries(attrs)) el.setAttribute(name, value);
  for (const child of children) el.append(child);
  return el;
}

/** How many matches the query has, and which one is selected (-1 for none). */
export function countMatches(state: EditorState, query: SearchQuery): { count: number; at: number } {
  if (!query.valid) return { count: 0, at: -1 };
  const { from, to } = state.selection.main;
  const cursor = query.getCursor(state);
  let count = 0;
  let at = -1;
  for (let next = cursor.next(); !next.done; next = cursor.next()) {
    if (next.value.from === from && next.value.to === to) at = count;
    if (++count >= MAX_COUNTED) break;
  }
  return { count, at };
}

/** `search({ createPanel })` for the Files view, with its words translated. */
export function createFindPanel(translate: Translate) {
  return (view: EditorView): Panel => new FindPanel(view, translate);
}

class FindPanel implements Panel {
  dom: HTMLElement;
  top = true;
  private query: SearchQuery;
  private searchField: HTMLInputElement;
  private replaceField: HTMLInputElement | null = null;
  private counter: HTMLElement;
  private toggles: Record<'caseSensitive' | 'regexp' | 'wholeWord', HTMLButtonElement>;
  private stepButtons: HTMLButtonElement[];

  constructor(private view: EditorView, private translate: Translate) {
    this.query = getSearchQuery(view.state);
    const t = translate;

    this.searchField = element('input', {
      type: 'text',
      // CodeMirror focuses and selects the field marked this way on Ctrl+F.
      'main-field': 'true',
      name: 'search',
      placeholder: t('diff.findPlaceholder'),
      title: `${t('notes.nextMatch')}: Enter · ↓ · F3 · Ctrl+G — ${t('notes.previousMatch')}: Shift+Enter · ↑`,
    });
    this.searchField.value = this.query.search;
    this.searchField.addEventListener('input', () => this.commit(true));
    this.searchField.addEventListener('keydown', (e) => this.searchKey(e));

    this.counter = element('span', { class: 'find-count' });

    const toggle = (option: 'caseSensitive' | 'regexp' | 'wholeWord', label: string, title: string) => {
      const button = element('button', { type: 'button', class: 'find-option', name: option, title }, [label]);
      button.addEventListener('click', () => {
        button.setAttribute('aria-pressed', String(button.getAttribute('aria-pressed') !== 'true'));
        this.commit(true);
      });
      return button;
    };
    this.toggles = {
      caseSensitive: toggle('caseSensitive', 'Aa', t('editorSearch.matchCase')),
      regexp: toggle('regexp', '.*', t('editorSearch.regexp')),
      wholeWord: toggle('wholeWord', 'W', t('editorSearch.byWord')),
    };

    const button = (name: string, label: string, title: string, run: () => void) => {
      const el = element('button', { type: 'button', name, title }, [label]);
      el.addEventListener('click', run);
      return el;
    };
    const previous = button('prev', '↑', `${t('notes.previousMatch')} (Shift+Enter · ↑)`, () => findPrevious(this.view));
    const next = button('next', '↓', `${t('notes.nextMatch')} (Enter · ↓ · F3 · Ctrl+G)`, () => findNext(this.view));
    this.stepButtons = [previous, next];
    const close = button('close', '×', `${t('common.close')} (Esc)`, () => this.close());

    const findRow = element('div', { class: 'find-row' }, [
      this.searchField, this.counter,
      this.toggles.caseSensitive, this.toggles.regexp, this.toggles.wholeWord,
      previous, next, close,
    ]);
    const rows: HTMLElement[] = [findRow];

    if (!view.state.readOnly) {
      this.replaceField = element('input', {
        type: 'text',
        name: 'replace',
        placeholder: t('editorSearch.replacePlaceholder'),
      });
      this.replaceField.value = this.query.replace;
      this.replaceField.addEventListener('input', () => this.commit(false));
      this.replaceField.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          replaceNext(this.view);
        } else if (e.key === 'Escape') {
          e.preventDefault();
          this.close();
        }
      });
      rows.push(element('div', { class: 'find-row' }, [
        this.replaceField,
        button('replace', t('editorSearch.replace'), t('editorSearch.replace'), () => replaceNext(this.view)),
        button('replaceAll', t('editorSearch.replaceAll'), t('editorSearch.replaceAll'), () => replaceAll(this.view)),
      ]));
    }

    this.dom = element('div', { class: 'asmgr-find' }, rows);
    this.showOptions(this.query);
    this.showCount();
  }

  /** Opened, the field takes the keyboard with its text selected. */
  mount() {
    this.searchField.select();
  }

  update(update: ViewUpdate) {
    // The query can change from outside: Ctrl+F again over another selection.
    for (const tr of update.transactions) {
      for (const effect of tr.effects) {
        if (effect.is(setSearchQuery) && !effect.value.eq(this.query)) {
          this.query = effect.value;
          this.searchField.value = this.query.search;
          if (this.replaceField) this.replaceField.value = this.query.replace;
          this.showOptions(this.query);
        }
      }
    }
    if (update.docChanged || update.selectionSet || update.transactions.length) this.showCount();
  }

  /** The query as the fields and options say it, sent to CodeMirror. */
  private commit(jump: boolean) {
    const query = new SearchQuery({
      search: this.searchField.value,
      caseSensitive: this.pressed('caseSensitive'),
      regexp: this.pressed('regexp'),
      wholeWord: this.pressed('wholeWord'),
      replace: this.replaceField?.value ?? this.query.replace,
    });
    if (query.eq(this.query)) return;
    this.query = query;
    this.view.dispatch({ effects: setSearchQuery.of(query) });
    // As in the diff's bar, typing goes to the first match from where the
    // reader is — including the one already under the caret.
    if (jump && query.valid) this.selectFrom(query, this.view.state.selection.main.from);
  }

  private selectFrom(query: SearchQuery, from: number) {
    const state = this.view.state;
    let match = query.getCursor(state, from).next();
    if (match.done) match = query.getCursor(state, 0, from).next();
    if (match.done) return;
    this.view.dispatch({
      selection: { anchor: match.value.from, head: match.value.to },
      scrollIntoView: true,
      userEvent: 'select.search',
    });
  }

  private searchKey(event: KeyboardEvent) {
    const action = findKeyAction(event);
    if (action === null) return;
    event.preventDefault();
    if (action === 'close') this.close();
    else if (action === 1) findNext(this.view);
    else findPrevious(this.view);
  }

  private close() {
    closeSearchPanel(this.view);
    this.view.focus();
  }

  private pressed(option: keyof FindPanel['toggles']): boolean {
    return this.toggles[option].getAttribute('aria-pressed') === 'true';
  }

  private showOptions(query: SearchQuery) {
    this.toggles.caseSensitive.setAttribute('aria-pressed', String(query.caseSensitive));
    this.toggles.regexp.setAttribute('aria-pressed', String(query.regexp));
    this.toggles.wholeWord.setAttribute('aria-pressed', String(query.wholeWord));
  }

  private showCount() {
    const { count, at } = countMatches(this.view.state, this.query);
    const shown = count >= MAX_COUNTED ? `${count}+` : count;
    this.counter.textContent = count
      ? `${at >= 0 ? at + 1 : '–'}/${shown}`
      : matchCounter(-1, 0, this.query.search, this.translate('notes.noMatches'));
    for (const button of this.stepButtons) button.disabled = count === 0;
  }
}
