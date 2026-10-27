export function mountConversionDialog(document, { revision, preview, convert, install, download, busy, status, ready = () => true }) {
  const $ = id => document.getElementById(id);
  const dialog = $('conversion'), save = $('conversion-save'), filename = $('conversion-filename');
  let current = null, saving = false;
  function controls() {
    $('conversion-save-fields').hidden = !save.checked;
    save.disabled = saving;
    filename.disabled = saving || !save.checked;
    $('confirm-convert').disabled = saving;
    $('cancel-convert').disabled = saving;
    $('close-convert').disabled = saving;
    $('confirm-convert').textContent = save.checked
      ? (current?.canSaveLocal ? 'Convert and save' : 'Convert and download') : 'Confirm conversion';
  }
  function error(message = '') {
    $('conversion-error').textContent = message;
    $('conversion-error').hidden = !message;
  }
  function cancel() {
    if (saving) return;
    current = null;
    dialog.close();
  }
  save.addEventListener('change', controls);
  $('cancel-convert').addEventListener('click', cancel);
  $('close-convert').addEventListener('click', cancel);
  dialog.addEventListener('cancel', event => {
    if (saving) event.preventDefault();
    else current = null;
  });
  $('conversion-form').addEventListener('submit', async event => {
    event.preventDefault();
    if (!current || saving || !ready()) return;
    error();
    if (current.revision !== revision()) {
      error('The conversion preview is stale. Cancel and preview again.');
      return;
    }
    const name = filename.value;
    if (save.checked && (!name.trim() || /[/\\\x00]/.test(name) || !name.toLowerCase().endsWith('.' + current.format))) {
      error(`Enter a filename ending in .${current.format}, without directory separators.`);
      return;
    }
    const snapshot = current, shouldSave = save.checked;
    saving = true;
    controls();
    busy(true);
    let converted = false;
    try {
      const payload = { revision: snapshot.revision, format: snapshot.format, confirm: true };
      if (shouldSave && snapshot.canSaveLocal) payload.saveFilename = name;
      const next = await convert(payload);
      converted = true;
      current = null;
      await install(next);
      if (shouldSave && !snapshot.canSaveLocal) download(next.source, name);
      dialog.close();
      status(next.savedPath ? `Saved converted copy: ${next.savedPath}`
        : shouldSave ? 'Converted copy downloaded.' : 'Conversion applied.');
    } catch (failure) {
      // Once conversion succeeds, a display/download failure must not resubmit it.
      const message = converted ? `Conversion completed. ${failure.message}` : failure.message;
      error(message);
      status(message, true);
    } finally {
      saving = false;
      busy(false);
      controls();
    }
  });
  return {
    async open(format) {
      if (saving) return;
      current = null;
      const result = await preview(format);
      if (result.revision !== revision()) throw new Error('Document changed; preview the conversion again.');
      current = result;
      $('conversion-preview').textContent = result.text;
      save.checked = false;
      filename.value = result.suggestedFilename;
      $('conversion-save-label').textContent = result.canSaveLocal ? 'Save beside the original file' : 'Download converted copy';
      $('conversion-save-help').textContent = result.canSaveLocal
        ? `Save in ${result.saveDirectory}. Existing files are preserved; choose another name if needed.`
        : 'Browsers hide the original upload directory. Choose a filename; your browser controls the download location.';
      error();
      controls();
      dialog.showModal();
    },
  };
}

export function downloadConvertedCopy(document, text, filename) {
  const url = URL.createObjectURL(new Blob([text], { type: 'application/octet-stream' }));
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
