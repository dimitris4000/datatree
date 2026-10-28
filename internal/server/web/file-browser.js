export function mountFileBrowser(document, { revision, browse, open, ready, canPick = () => true }) {
  const $ = id => document.getElementById(id);
  const directory = $('file-browser-path'), list = $('file-browser-list'), path = $('local-path');
  let generation = 0, displayedRevision = null, parent = '', current = '', opening = false, selected = null;
  let purpose = 'document', pending = null;
  function error(message = '') {
    $('file-open-error').textContent = message;
    $('file-open-error').hidden = !message;
  }
  async function openPath(value) {
    if (opening || !(purpose === 'document' ? ready() : canPick()) || !value.trim()) return;
    error();
    if (displayedRevision !== revision()) {
      error('Document changed. Close and reopen the file picker.');
      return;
    }
    opening = true;
    path.disabled = true;
    try {
      if (purpose !== 'document') {
        const resolve = pending;
        pending = null;
        resolve?.({ path: value });
        $('open-document').close();
      } else if (await open({ revision: displayedRevision, path: value })) $('open-document').close();
    } catch (failure) { error(failure.message); }
    finally { opening = false; path.disabled = false; }
  }
  function select(entry, button) {
    selected?.setAttribute('aria-pressed', 'false');
    selected = button;
    button.setAttribute('aria-pressed', 'true');
    path.value = entry.path;
    error();
  }
  async function load(value) {
    const token = ++generation, requestedRevision = revision();
    path.value = '';
    selected = null;
    $('file-browser-status').textContent = 'Loading directory…';
    try {
      const result = await browse({ revision: requestedRevision, path: value, showHidden: $('file-browser-hidden').checked });
      if (token !== generation) return;
      if (requestedRevision !== revision() || result.revision !== requestedRevision) {
        $('file-browser-status').textContent = 'Document changed. Close and reopen the file picker.';
        return;
      }
      current = result.path;
      parent = result.parent;
      directory.value = current;
      $('file-browser-up').disabled = parent === current;
      list.replaceChildren();
      const entries = result.entries.filter(entry => entry.directory || purpose !== 'schema' || /\.json$/i.test(entry.name));
      for (const entry of entries) {
        const item = document.createElement('li'), button = document.createElement('button');
        button.type = 'button';
        button.className = entry.directory ? 'browser-entry browser-directory' : 'browser-entry';
        button.textContent = entry.name;
        button.title = entry.path;
        if (entry.directory) {
          button.setAttribute('aria-label', `Open folder ${entry.name}`);
          button.addEventListener('click', () => load(entry.path));
        } else {
          button.setAttribute('aria-pressed', 'false');
          button.addEventListener('click', () => select(entry, button));
          button.addEventListener('dblclick', () => { select(entry, button); return openPath(entry.path); });
        }
        item.append(button);
        list.append(item);
      }
      $('file-browser-status').textContent = result.truncated
        ? `Directory listing limited to ${result.limit || 1000} entries. Enter an exact path below to open another file.`
        : entries.length ? 'Select a file, then choose it. Double-click a file to choose it directly.' : 'No supported files or folders found.';
    } catch (failure) {
      if (token === generation) $('file-browser-status').textContent = failure.message;
    }
  }
  $('file-browser-form').addEventListener('submit', event => { event.preventDefault(); return load(directory.value.trim()); });
  $('file-browser-up').addEventListener('click', () => load(parent));
  $('file-browser-hidden').addEventListener('change', () => load(current));
  $('local-file-form').addEventListener('submit', event => { event.preventDefault(); return openPath(path.value); });
  $('open-document').addEventListener('close', () => {
    generation++; displayedRevision = null; purpose = 'document';
    const resolve = pending; pending = null; resolve?.(null);
  });
  const picker = {
    async show(kind = 'document') {
      purpose = kind;
      if (kind === 'document' && pending) { pending(null); pending = null; }
      displayedRevision = revision();
      $('open-document-title').textContent = kind === 'save' ? 'Save As…' : kind === 'schema' ? 'Choose validation schema' : kind === 'comparison' ? 'Choose comparison file' : 'Open file';
      $('file-browser-open').textContent = kind === 'save' ? 'Save' : kind === 'document' ? 'Open' : 'Choose';
      $('file-browser-help').textContent = kind === 'save' ? 'Browse to a folder, then enter a new file path below. Existing files will not be overwritten.' : kind === 'document'
        ? 'Browse files on the computer running Data Tree. Matching schemas and Git context load automatically.'
        : kind === 'schema' ? 'Choose a JSON Schema on the computer running Data Tree. The working document stays open.'
        : 'Choose a comparison file on the computer running Data Tree. The working document stays open.';
      $('local-path-help').textContent = kind === 'save' ? 'Enter a new path with the working document’s format extension. Relative paths start from the CLI’s working directory.' : kind === 'schema' ? 'JSON Schema, up to 1 MiB. Relative paths start from the CLI’s working directory.'
        : 'JSON, JSONL, YAML and CSV. Relative file paths start from the CLI’s working directory.';
      error();
      path.value = '';
      list.replaceChildren();
      return load('');
    },
    pick(kind) {
      if (pending) pending(null);
      const result = new Promise(resolve => { pending = resolve; });
      $('open-document').showModal();
      void picker.show(kind);
      return result;
    },
  };
  return picker;
}
