export function canValidateDocument(state) {
  return !!state.root && ['json', 'jsonl', 'yaml', 'csv'].includes(state.format);
}

export class ValidationController {
  constructor({ revision, attach, clear, validate, navigate, render, available = () => true }) {
    Object.assign(this, { revision, attach, clear, validate, navigate, render, available });
    this.visible = false;
    this.schema = this.result = null;
    this.index = -1;
    this.stale = this.loading = this.uploading = this.navigating = false;
    this.error = '';
    this.generation = this.documentEpoch = 0;
    this.render(this);
  }
  install(schema) {
    this.generation++; this.documentEpoch++;
    this.loading = this.navigating = false;
    this.error = ''; this.index = -1;
    if (schema?.token !== this.schema?.token) this.result = null;
    this.schema = schema || null;
    this.stale = !!this.result && this.result.revision !== this.revision();
    this.render(this);
  }
  async setVisible(visible) {
    this.visible = visible;
    if (!visible) { this.generation++; this.loading = this.navigating = false; }
    this.render(this);
  }
  async replace(read) {
    if (this.uploading) return;
    const epoch = this.documentEpoch, revision = this.revision();
    this.uploading = true; this.generation++; this.loading = false; this.error = '';
    this.render(this);
    try {
      if (!this.available()) throw new Error('Open a document first.');
      const payload = await read();
      if (!payload) return;
      if (epoch !== this.documentEpoch || revision !== this.revision()) return;
      if (payload.text !== undefined && new TextEncoder().encode(payload.text).length > 1024 * 1024) throw new Error('Schema exceeds 1 MiB.');
      const response = await this.attach(payload, revision);
      if (epoch !== this.documentEpoch || revision !== this.revision()) return;
      if (response.revision !== revision) throw new Error('Document changed; attach the schema again.');
      this.schema = response.schema; this.result = null; this.stale = false; this.index = -1;
    } catch (error) { if (epoch === this.documentEpoch) this.error = error.message; }
    finally { this.uploading = false; this.render(this); }
  }
  async clearSchema() {
    if (this.uploading) return;
    const epoch = this.documentEpoch, revision = this.revision();
    this.uploading = true; this.generation++; this.loading = false; this.error = ''; this.render(this);
    try {
      const response = await this.clear(revision);
      if (epoch !== this.documentEpoch || revision !== this.revision()) return;
      if (response.revision !== revision) throw new Error('Document changed; refresh Validation.');
      this.schema = this.result = null; this.stale = false; this.index = -1;
    } catch (error) { if (epoch === this.documentEpoch) this.error = error.message; }
    finally { this.uploading = false; this.render(this); }
  }
  async run() {
    if (this.loading || this.uploading || !this.schema) return;
    const generation = ++this.generation, revision = this.revision(), token = this.schema.token;
    this.loading = true; this.navigating = false; this.error = ''; this.render(this);
    try {
      if (!this.available()) throw new Error('Open a document first.');
      const result = await this.validate(revision, token);
      if (generation !== this.generation) return;
      if (revision !== this.revision() || result.revision !== revision || result.schema.token !== token)
        throw new Error('Document or schema changed; run Validate again.');
      this.result = result; this.stale = false; this.index = -1;
    } catch (error) { if (generation === this.generation) this.error = error.message; }
    finally { if (generation === this.generation) { this.loading = false; this.render(this); } }
  }
  async choose(index) {
    if (this.loading || this.uploading || this.navigating || this.stale || !this.result?.diagnostics[index] || this.result.revision !== this.revision()) return false;
    const generation = this.generation;
    this.navigating = true; this.error = ''; this.render(this);
    try {
      const accepted = await this.navigate(this.result.diagnostics[index].target);
      if (generation !== this.generation) return false;
      if (accepted) this.index = index;
      return !!accepted;
    } catch (error) { if (generation === this.generation) this.error = error.message; return false; }
    finally { if (generation === this.generation) { this.navigating = false; this.render(this); } }
  }
  async move(direction) {
    const count = this.result?.diagnostics.length || 0;
    if (!count) return false;
    return this.choose(this.index < 0 ? (direction > 0 ? 0 : count - 1) : (this.index + direction + count) % count);
  }
}

function diagnosticContent(document, error) {
  const block = document.createElement('div'), message = document.createElement('p'), constraint = document.createElement('small');
  message.textContent = error.message;
  constraint.textContent = `Constraint: ${error.schemaPath || error.keyword}`;
  block.append(message, constraint);
  if (error.suggestions?.length) {
    const label = document.createElement('p'), values = document.createElement('pre');
    label.textContent = 'Allowed by this constraint (other constraints still apply):';
    values.textContent = error.suggestions.join('\n'); values.tabIndex = 0;
    block.append(label, values);
  }
  if (error.suggestionsTruncated) {
    const notice = document.createElement('p'); notice.textContent = 'Allowed-value previews are truncated (up to 50 values, 1,024 bytes each).'; block.append(notice);
  }
  return block;
}

export function mountValidationPanel(document, dependencies) {
  const $ = id => document.getElementById(`validation-${id}`), emptyMarks = {};
  let controller, highlighted;
  const renderInspector = state => {
    const container = $('inspector');
    const errors = state.visible && !state.stale && !state.loading && state.result?.revision === dependencies.revision()
      ? state.result.diagnostics.filter(e => e.target === dependencies.selected()) : [];
    container.hidden = !errors.length;
    const title = document.createElement('h2'); title.textContent = 'Schema validation';
    container.replaceChildren(...(errors.length ? [title, ...errors.map(e => diagnosticContent(document, e))] : []));
  };
  controller = new ValidationController({ ...dependencies, render: state => {
    $('panel').hidden = !state.visible;
    $('toggle').setAttribute('aria-expanded', String(state.visible));
    $('filename').textContent = state.schema ? `Schema: ${state.schema.filename}` : 'No schema attached. Choose or paste a JSON Schema.';
    $('clear').disabled = !state.schema || state.uploading;
    $('attach').disabled = $('file').disabled = state.uploading;
    $('run').disabled = !state.schema || state.loading || state.uploading;
    const inactive = state.loading || state.uploading || state.stale || state.navigating;
    $('previous').disabled = $('next').disabled = inactive || !state.result?.diagnostics.length;
    const result = state.result;
    $('status').textContent = state.error || (state.uploading ? 'Updating schema…' : state.loading ? 'Validating applied document…' : state.stale ? 'Results are stale. Press Validate to check the applied document.' :
      result ? !result.complete ? 'Validation incomplete: some content cannot be validated.' : result.valid ? 'Valid against the attached schema.' : `Validation errors${result.truncated ? ': showing the first 200; tree indicators include all errors' : `: ${result.diagnostics.length}`}.` :
      'Press Validate to check the applied document. Drafts are not validated.');
    $('status').classList.toggle('error', !!state.error || !!result && !state.stale && !result.valid);
    const items = (result?.diagnostics || []).map((error, index) => {
      const item = document.createElement('li'), jump = document.createElement('button');
      jump.type = 'button';
      jump.textContent = `Document ${error.documentIndex + 1} · ${error.instancePath || '/ (root)'} · ${error.location}`;
      jump.disabled = inactive;
      jump.addEventListener('click', () => controller.choose(index));
      item.setAttribute('aria-current', String(index === state.index));
      item.append(jump, diagnosticContent(document, error)); return item;
    });
    $('results').replaceChildren(...items);
    const marks = state.visible && !state.stale && !state.loading && result ? result.marks : emptyMarks;
    if (marks !== highlighted) { highlighted = marks; dependencies.highlight(marks); }
    renderInspector(state);
  }});
  $('toggle').addEventListener('click', () => controller.setVisible(!controller.visible));
  $('run').addEventListener('click', () => controller.run());
  $('clear').addEventListener('click', () => controller.clearSchema());
  $('attach').addEventListener('click', () => controller.replace(async () => ({ filename: 'Pasted schema', text: $('text').value })));
  $('previous').addEventListener('click', () => controller.move(-1));
  $('next').addEventListener('click', () => controller.move(1));
  $('file').addEventListener('click', () => controller.replace(() => dependencies.pickFile()));
  return { controller, selectionChanged: () => renderInspector(controller) };
}
