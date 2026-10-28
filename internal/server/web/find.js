import { revealPath } from './source-editor.js';

export class FindController {
  constructor({ load, revision, navigate, render, invalidate = () => {} }) {
    Object.assign(this, { load, revision, navigate, render, invalidate });
    this.generation = 0;
    this.clear();
  }
  clear() {
    this.request?.abort();
    this.invalidate();
    this.generation++;
    this.matches = [];
    this.index = -1;
    this.error = '';
    this.truncated = false;
    this.nextCursor = '';
    this.pageIndex = 0;
    this.pageCursors = [''];
    this.options = null;
    this.loading = false;
    this.navigating = false;
    this.searched = false;
    this.render(this);
  }
  async submit(options) {
    this.clear();
    if (options.mode === 'search' && options.text === '') return;
    this.searched = true;
    if (options.mode === 'search' && !options.keys && !options.values) {
      this.error = 'Select keys or values to search.';
      this.render(this);
      return;
    }
    const generation = this.generation, revision = this.revision();
    this.options = {...options};
    this.request = new AbortController();
    this.loading = true;
    this.render(this);
    try {
      const result = await this.load(options, revision, this.request.signal);
      if (generation !== this.generation) return;
      if (revision !== this.revision() || result.revision !== revision)
        throw new Error('Document changed; run Find again.');
      this.matches = result.matches;
      this.truncated = result.truncated;
      this.nextCursor = result.nextCursor || '';
    } catch (error) {
      if (generation === this.generation) this.error = error.message;
    } finally {
      if (generation === this.generation) {
        this.loading = false;
        this.render(this);
      }
    }
    if (generation === this.generation && options.mode === 'query' && this.matches.length === 1)
      await this.choose(0);
  }
  async page(direction) {
    if(this.loading || this.navigating || !this.options || ![-1,1].includes(direction)) return false;
    const index=this.pageIndex+direction;
    if(index<0 || (direction>0 && !this.nextCursor)) return false;
    const cursor=direction>0 ? this.nextCursor : this.pageCursors[index];
    this.request?.abort();this.invalidate();
    const generation=++this.generation,revision=this.revision();
    this.request=new AbortController();this.loading=true;this.error='';this.render(this);
    try {
      const result=await this.load({...this.options,cursor,limit:200},revision,this.request.signal);
      if(generation!==this.generation)return false;
      if(revision!==this.revision() || result.revision!==revision)throw new Error('Document changed; run Find again.');
      this.matches=result.matches;this.index=-1;this.pageIndex=index;
      this.pageCursors[index]=cursor;this.pageCursors.length=index+1;
      this.nextCursor=result.nextCursor || '';this.truncated=result.truncated;
      return true;
    } catch(error) {
      if(generation===this.generation)this.error=error.message;
      return false;
    } finally {
      if(generation===this.generation){this.loading=false;this.render(this);}
    }
  }
  async choose(index) {
    if (this.loading || this.navigating || !this.matches[index]) return false;
    const generation = this.generation, revision = this.revision();
    this.navigating = true;
    this.render(this);
    try {
      const accepted = await this.navigate(this.matches[index].id);
      if (generation !== this.generation || revision !== this.revision()) return false;
      if (accepted) this.index = index;
      return !!accepted;
    } catch (error) {
      if (generation === this.generation) this.error = error.message;
      return false;
    } finally {
      if (generation === this.generation) {
        this.navigating = false;
        this.render(this);
      }
    }
  }
  async move(direction) {
    if (!this.matches.length) return false;
    const index = this.index < 0 ? (direction > 0 ? 0 : this.matches.length - 1) :
      (this.index + direction + this.matches.length) % this.matches.length;
    return this.choose(index);
  }
}

export async function revealResult(id, { ranges, records, expanded, load, current, relayout }) {
  if (!ranges.has(id) || !current()) return false;
  const path = revealPath(ranges, id);
  for (const step of path) {
    const record = records.get(step.id);
    if (!record?.children || (record.offset || 0) !== step.offset) await load(step.id, step.offset);
    if (!current()) return false;
    expanded.add(step.id);
  }
  if (!current()) return false;
  // Prioritize this ancestor chain within the existing 1,000-card limit.
  relayout(new Set([...path.map(step => step.id), id]));
  return true;
}

export function mountFindPanel(document, dependencies) {
  const $ = id => document.getElementById(`find-${id}`);
  let rendered = null;
  const buttons = [];
  const controller = new FindController({
    ...dependencies,
    render: state => {
      const count = state.matches.length;
      const summary = state.index >= 0 ? `${state.index + 1} of ${count} matches` : `${count} match${count === 1 ? '' : 'es'}`;
      $('status').textContent = state.error || (state.loading ? 'Finding…' : state.searched ?
        `${state.pageIndex ? `Page ${state.pageIndex+1} · ` : ''}${summary}${state.truncated ? ' · More results available.' : ''}` : '');
      $('status').classList.toggle('error', !!state.error);
      $('results').hidden = !count;
      $('previous').disabled = $('next').disabled = !count || state.loading || state.navigating;
      $('submit').disabled = state.navigating;
      $('previous-page').hidden = state.pageIndex === 0;
      $('next-page').hidden = !state.nextCursor;
      $('previous-page').disabled = $('next-page').disabled = state.loading || state.navigating;
      if (rendered !== state.matches) {
        rendered = state.matches;
        buttons.length = 0;
        const items = state.matches.map((match, index) => {
          const item = document.createElement('li'), button = document.createElement('button');
          button.type = 'button';
          const path = document.createElement('span'), preview = document.createElement('span');
          path.className = 'find-result-path';
          path.textContent = match.path || match.location;
          preview.className = 'find-result-preview';
          preview.textContent = `${match.kind} · ${match.preview}`;
          button.append(path, preview);
          button.addEventListener('click', () => controller.choose(index));
          item.append(button);
          buttons.push(button);
          return item;
        });
        $('results').replaceChildren(...items);
      }
      buttons.forEach((button, index) => {
        button.setAttribute('aria-current', String(index === state.index));
        button.disabled = state.navigating;
      });
    },
  });
  function modeHelp() {
    const query = $('mode').value === 'query';
    $('options').hidden = query;
    $('text').placeholder = query ? '$.services[0].name' : 'Find keys or values…';
    $('help').textContent = query ?
      "Applied document paths: $.services[0].name · $['a.b'] · $..name · $.services[*].name · $.services[?(@.replicas > 3)]. Apply or discard source drafts before navigating." :
      'Search the applied document, including collapsed branches. Apply or discard source drafts before navigating.';
  }
  $('form').addEventListener('submit', async event => {
    event.preventDefault();
    await controller.submit({ mode: $('mode').value, text: $('text').value,
      keys: $('keys').checked, values: $('values').checked, caseSensitive: $('case').checked });
  });
  $('mode').addEventListener('change', () => { controller.clear(); modeHelp(); });
  $('text').addEventListener('input', () => controller.clear());
  for (const id of ['keys', 'values', 'case']) $(id).addEventListener('change', () => controller.clear());
  $('clear').addEventListener('click', () => { $('text').value = ''; controller.clear(); });
  $('previous').addEventListener('click', () => controller.move(-1));
  $('next').addEventListener('click', () => controller.move(1));
  $('previous-page').addEventListener('click', () => controller.page(-1));
  $('next-page').addEventListener('click', () => controller.page(1));
  modeHelp();
  return { controller };
}
