export class DiffController {
  constructor({ revision, load, upload, navigate, render, revert = async () => false, invalidate = () => {} }) {
    Object.assign(this, { revision, load, upload, navigate, render, revert, invalidate });
    this.visible = false;
    this.baseline = 'original';
    this.file = null;
    this.result = null;
    this.index = -1;
    this.error = '';
    this.loading = this.uploading = this.navigating = this.reverting = false;
    this.generation = 0;
    this.render(this);
  }
  invalidateDocument(imported = false) {
    this.invalidate();
    this.generation++;
    this.result = null;
    this.index = -1;
    this.error = '';
    this.loading = this.navigating = false;
    if (imported) { this.baseline = 'original'; this.file = null; }
    this.render(this);
  }
  async setVisible(value) {
    this.visible = value;
    if (!value) this.invalidateDocument();
    else await this.refresh();
  }
  async refresh(kind = this.baseline) {
    if (!this.visible) return;
    if (kind === 'file' && !this.file) {
      this.error = 'Choose a comparison file first.';
      this.render(this);
      return;
    }
    this.invalidate();
    const generation = ++this.generation, revision = this.revision();
    this.loading = true;
    this.navigating = false;
    this.error = '';
    this.render(this);
    try {
      const result = await this.load(kind, revision);
      if (generation !== this.generation) return;
      if (this.revision() !== revision || result.revision !== revision)
        throw new Error('Document changed; refresh Changes.');
      this.result = result;
      this.baseline = kind;
      if (kind === 'file') this.file = result.baseline;
      this.index = -1;
    } catch (error) {
      if (generation === this.generation) this.error = error.message;
    } finally {
      if (generation === this.generation) { this.loading = false; this.render(this); }
    }
  }
  async replace(read) {
    // Serialize uploads: comparison files do not change the document revision.
    if (this.uploading || this.reverting) return;
    this.invalidate();
    this.generation++;
    const revision = this.revision();
    this.uploading = true;
    this.loading = this.navigating = false;
    this.error = '';
    this.render(this);
    try {
      const payload = await read();
      if (this.revision() !== revision) throw new Error('Document changed; choose the comparison file again.');
      const result = await this.upload(payload, revision);
      if (this.revision() !== revision) return;
      if (result.revision !== revision) throw new Error('Document changed; refresh Changes.');
      // A completed upload may retain its file while closed, but never restores
      // hidden tree highlights. Invalidate reads sent while it was pending.
      this.generation++;
      this.file = result.baseline;
      this.baseline = 'file';
      this.result = this.visible ? result : null;
      this.index = -1;
      this.loading = false;
    } catch (error) {
      if (this.revision() === revision) this.error = error.message;
    } finally {
      this.uploading = false;
      this.render(this);
    }
  }
  async choose(index) {
    if (this.loading || this.uploading || this.navigating || this.reverting || !this.result?.changes[index]) return false;
    if (this.result.revision !== this.revision()) return false;
    const generation = this.generation;
    this.navigating = true;
    this.error = '';
    this.render(this);
    try {
      const accepted = await this.navigate(this.result.changes[index].target);
      if (generation !== this.generation) return false;
      if (accepted) this.index = index;
      return !!accepted;
    } catch (error) {
      if (generation === this.generation) this.error = error.message;
      return false;
    } finally {
      if (generation === this.generation) { this.navigating = false; this.render(this); }
    }
  }
  async revertChange(index) {
    if (this.loading || this.uploading || this.navigating || this.reverting || !this.result?.changes[index]) return false;
    if (this.result.revision !== this.revision()) return false;
    const result = this.result, revision = result.revision;
    this.reverting = true;
    this.error = '';
    this.render(this);
    try {
      return !!await this.revert({ baseline: result.baseline.kind, baselineToken: result.baseline.token, changeIndex: index }, revision);
    } catch (error) {
      if (this.result === result) this.error = error.message;
      return false;
    } finally {
      this.reverting = false;
      this.render(this);
    }
  }
  async move(direction) {
    const count = this.result?.changes.length || 0;
    if (!count) return false;
    return this.choose(this.index < 0 ? (direction > 0 ? 0 : count - 1) : (this.index + direction + count) % count);
  }
}

function kindLabel(value) {
  return `${value.kind}${value.tag && !value.tag.startsWith('!!') ? ` ${value.tag}` : ''}`;
}
export function mountDiffPanel(document, dependencies) {
  const $ = id => document.getElementById(`diff-${id}`);
  const emptyMarks = {};
  let rendered = null, highlighted = null;
  const buttons = [];
  const controller = new DiffController({
    ...dependencies,
    render: state => {
      $('panel').hidden = !state.visible;
      $('toggle').setAttribute('aria-expanded', String(state.visible));
      $('baseline').value = state.baseline;
      $('baseline').disabled = $('file').disabled = state.uploading || state.reverting;
      $('refresh').disabled = state.loading || state.uploading || state.reverting;
      const baseline = state.result?.baseline || (state.baseline === 'file' ? state.file : null);
      $('filename').textContent = baseline ? `${baseline.kind === 'original' ? 'Original' : 'Compare file'}: ${baseline.filename || 'Untitled'} (${baseline.format.toUpperCase()})` : 'Original import';
      $('status').textContent = state.error || (state.reverting ? 'Reverting change…' : state.uploading ? 'Reading and comparing file…' : state.loading ? 'Comparing…' :
        state.result?.truncated ? 'Showing the first 200 changes; counts and tree indicators include all changes.' :
        state.result && !state.result.changes.length ? 'No structural changes.' : 'Comparing the applied document; apply or discard source drafts before navigating.');
      $('status').classList.toggle('error', !!state.error);
      const counts = state.result?.counts;
      $('counts').textContent = counts ? `${counts.added} added · ${counts.removed} removed · ${counts.changed} changed${state.index >= 0 ? ` · Change ${state.index + 1} of ${state.result.changes.length} shown` : ''}` : '';
      const disabled = state.loading || state.uploading || state.navigating || state.reverting;
      $('previous').disabled = $('next').disabled = disabled || !state.result?.changes.length;
      const changes = state.result?.changes || null;
      if (changes !== rendered) {
        rendered = changes;
        buttons.length = 0;
        const items = (changes || []).map((change, index) => {
          const item = document.createElement('li'), header = document.createElement('div');
          const jump = document.createElement('button'), revert = document.createElement('button');
          const value = change.after || change.before;
          item.className = `diff-entry diff-${change.status}`;
          header.className = 'diff-entry-actions';
          jump.type = revert.type = 'button';
          jump.className = 'diff-jump';
          jump.textContent = `${change.status[0].toUpperCase() + change.status.slice(1)} · ${value.path || value.location}`;
          jump.title = 'Reveal this change in the tree';
          revert.textContent = 'Revert';
          revert.className = 'diff-revert';
          revert.setAttribute('aria-label', `Revert change at ${value.path || value.location}`);
          revert.title = change.status === 'added' ? 'Remove this added entry' : change.status === 'removed' ? 'Restore this removed entry' : 'Restore the baseline value';
          jump.addEventListener('click', () => controller.choose(index));
          revert.addEventListener('click', () => controller.revertChange(index));
          header.append(jump, revert);
          const values = document.createElement('div');
          values.className = 'diff-values';
          for (const [name, side] of [['Before', change.before], ['After', change.after]]) {
            const block = document.createElement('div'), label = document.createElement('span'), text = document.createElement('pre');
            label.className = 'diff-value-label';
            label.textContent = `${name}${side ? ` (${kindLabel(side)})` : ''}`;
            text.className = 'diff-value';
            text.tabIndex = 0;
            text.setAttribute('aria-label', `${name} value`);
            text.textContent = side ? side.preview : name === 'Before' ? 'Absent' : 'Removed';
            block.append(label, text); values.append(block);
          }
          item.append(header, values);
          buttons.push({ jump, revert, item });
          return item;
        });
        $('results').replaceChildren(...items);
      }
      buttons.forEach(({ jump, revert, item }, index) => {
        jump.disabled = revert.disabled = disabled;
        item.setAttribute('aria-current', String(index === state.index));
      });
      const marks = state.visible && state.result ? state.result.marks : emptyMarks;
      if (marks !== highlighted) { highlighted = marks; dependencies.highlight(marks); }
    },
  });
  $('toggle').addEventListener('click', () => controller.setVisible(!controller.visible));
  $('refresh').addEventListener('click', () => controller.refresh());
  $('baseline').addEventListener('change', () => controller.refresh($('baseline').value));
  $('previous').addEventListener('click', () => controller.move(-1));
  $('next').addEventListener('click', () => controller.move(1));
  $('file').addEventListener('change', async event => {
    const file = event.target.files[0];
    if (!file) return;
    await controller.replace(async () => {
      if (file.size > 10 * 1024 * 1024) throw new Error('Comparison file exceeds 10 MiB.');
      const ext = file.name.split('.').pop().toLowerCase();
      const format = ext === 'yml' ? 'yaml' : ext === 'ndjson' ? 'jsonl' : ext;
      if (!['json', 'yaml', 'jsonl', 'csv'].includes(format)) throw new Error('Choose a JSON, JSONL, YAML or CSV comparison file.');
      return { filename: file.name, format, text: new TextDecoder('utf-8', { fatal: true }).decode(await file.arrayBuffer()) };
    });
    event.target.value = '';
  });
  return { controller };
}
