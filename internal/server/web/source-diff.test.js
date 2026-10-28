import test from 'node:test';
import assert from 'node:assert/strict';
import { unifiedDiff } from './source-diff.js';

test('unchanged text has an explicit empty state', () => {
  assert.equal(unifiedDiff('a\n', 'a\n'), 'No changes since this file was opened.');
});
test('unified diff includes context, line ranges, additions and deletions', () => {
  assert.equal(unifiedDiff('a\nb\nc\n', 'a\nB\nc\n'),
    '--- Original\n+++ Working\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n');
});
test('empty files and missing final newlines are represented accurately', () => {
  assert.match(unifiedDiff('', 'a'), /@@ -0,0 \+1,1 @@\n\+a\n\\ No newline at end of file/);
  assert.match(unifiedDiff('a\n', 'a'), /-a\n\+a\n\\ No newline at end of file/);
  assert.match(unifiedDiff('a\n', ''), /@@ -1,1 \+0,0 @@\n-a/);
});
test('distant edits produce separate hunks and HTML stays literal', () => {
  const lines=Array.from({length:30},(_,i)=>String(i));
  const changed=[...lines];changed[1]='<script>';changed[28]='changed';
  const diff=unifiedDiff(lines.join('\n'),changed.join('\n'));
  assert.equal((diff.match(/^@@/gm)||[]).length,2);
  assert.ok(diff.includes('+<script>'));
});
test('large replacements use a bounded comparison and retain all changed lines', () => {
  const diff=unifiedDiff('old\n'.repeat(2000),'new\n'.repeat(2000));
  assert.equal((diff.match(/^-old$/gm)||[]).length,2000);
  assert.equal((diff.match(/^\+new$/gm)||[]).length,2000);
});
