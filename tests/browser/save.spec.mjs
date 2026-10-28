import {test,expect} from '@playwright/test';
import {mkdtemp,readFile,writeFile,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';

test('Save As creates a destination and Save updates it while preserving the opened baseline',async({page})=>{
 const dir=await mkdtemp(join(tmpdir(),'datatree-save-test-'));
 const path=join(dir,'saved.json');
 try {
  await page.goto('/');
  await expect(page.locator('#revision')).toContainText('Revision 1');
  // Use a small JSON example independent of the performance fixture's format.
  await page.locator('#example').click();
  await expect(page.locator('#revision')).toContainText('Revision 2');
  await page.locator('#save').click();
  await expect(page.locator('#open-document-title')).toHaveText('Save As…');
  await expect(page.locator('#file-browser-help')).toContainText('Existing files will not be overwritten');
  await page.locator('#local-path').fill(path);
  await page.locator('#file-browser-open').click();
  await expect(page.locator('#status')).toContainText(`Saved ${path}`);
  const original=await readFile(path,'utf8');
  await expect(page.locator('#filename')).toHaveText('saved.json');
  const editor=page.locator('#source .cm-content');
  await editor.click();await page.keyboard.press('ControlOrMeta+Home');await page.keyboard.insertText(' ');
  await page.locator('#save').click();
  await expect(page.locator('#status')).toContainText('Apply or discard');
  expect(await readFile(path,'utf8')).toBe(original);
  await page.locator('#apply').click();
  await expect(page.locator('#revision')).toContainText('Revision 3');
  await page.locator('#save').click();
  await expect(page.locator('#status')).toContainText(`Saved ${path}`);
  expect(await readFile(path,'utf8')).toBe(' '+original);
  await page.locator('#source-diff-tab').click();
  await expect(editor).toContainText('--- Original');
  await page.locator('#working-tab').click();
  await writeFile(path,'external edit');
  await page.locator('#save').click();
  await expect(page.locator('#status')).toContainText('file changed on disk');
  expect(await readFile(path,'utf8')).toBe('external edit');
  await page.locator('#save-as').click();
  await page.locator('#local-path').fill(path);
  await page.locator('#file-browser-open').click();
  await expect(page.locator('#status')).toContainText('already exists');
  expect(await readFile(path,'utf8')).toBe('external edit');
 } finally {await rm(dir,{recursive:true,force:true});}
});
