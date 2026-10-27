const formats = new Set(['json', 'jsonl', 'ndjson', 'yaml', 'yml', 'csv']);
const extension = file => file.name.split('.').pop().toLowerCase();
const relativePath = file => file.webkitRelativePath || file.name;

export function isDocumentFile(file) {
  return formats.has(extension(file)) && !/\.schema\.json$/i.test(file.name);
}

export function selectUploadDocument(files) {
  const list = Array.from(files);
  if (list.length === 1 && formats.has(extension(list[0]))) return list[0];
  const documents = list.filter(isDocumentFile);
  if (documents.length !== 1) throw new Error('Choose one document and its matching .schema.json file, or choose a folder.');
  return documents[0];
}

async function readText(file, limit, label) {
  try {
    if (file.size > limit) throw new Error(`${label} exceeds ${limit === 1024 * 1024 ? '1 MiB' : '10 MiB'}.`);
    const bytes = await file.arrayBuffer();
    if (bytes.byteLength > limit) throw new Error(`${label} exceeds the size limit.`);
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  } catch (error) { throw new Error(`${file.name}: ${error.message}`); }
}

export async function readFileImport(file, available) {
  const ext = extension(file);
  if (!formats.has(ext)) throw new Error('Choose a JSON, JSONL, YAML or CSV file.');
  const payload = { text: await readText(file, 10 * 1024 * 1024, 'File'), format: ext === 'yml' ? 'yaml' : ext === 'ndjson' ? 'jsonl' : ext, filename: file.name };
  const path = relativePath(file), dot = path.lastIndexOf('.');
  const expected = path.slice(0, dot) + '.schema.json';
  const schemas = Array.from(available).filter(candidate => relativePath(candidate) === expected);
  if (schemas.length > 1) throw new Error(`Multiple matching schemas for ${file.name}.`);
  if (schemas.length) {
    const schema = schemas[0];
    payload.schema = { filename: schema.name, text: await readText(schema, 1024 * 1024, 'Schema') };
  }
  return payload;
}
