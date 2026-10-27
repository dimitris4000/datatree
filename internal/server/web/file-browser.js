export function mountFileBrowser(document, { revision, browse, open, ready }) {
  const $ = id => document.getElementById(id);
  const directory = $('file-browser-path'), list = $('file-browser-list'), path = $('local-path');
  let generation = 0, displayedRevision = null, parent = '', current = '', opening = false, selected = null;
  function error(message = '') {
    $('file-open-error').textContent = message;
    $('file-open-error').hidden = !message;
  }
  async function openPath(value) {
    if (opening || !ready() || !value.trim()) return;
    error();
    if (displayedRevision !== revision()) {
      error('Document changed. Close and reopen the file picker.');
      return;
    }
    opening = true;
    path.disabled = true;
    try {
      if (await open({ revision: displayedRevision, path: value })) $('open-document').close();
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
      path.value = '';
      selected = null;
      list.replaceChildren();
      for (const entry of result.entries) {
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
        : result.entries.length ? 'Select a file, then Open. Double-click a file to open it directly.' : 'No supported files or folders found.';
    } catch (failure) {
      if (token === generation) $('file-browser-status').textContent = failure.message;
    }
  }
  $('file-browser-form').addEventListener('submit', event => { event.preventDefault(); return load(directory.value.trim()); });
  $('file-browser-up').addEventListener('click', () => load(parent));
  $('file-browser-hidden').addEventListener('change', () => load(current));
  $('local-file-form').addEventListener('submit', event => { event.preventDefault(); return openPath(path.value); });
  $('open-document').addEventListener('close', () => { generation++; displayedRevision = null; });
  return {
    async show() {
      displayedRevision = revision();
      error();
      path.value = '';
      list.replaceChildren();
      return load('');
    },
  };
}
