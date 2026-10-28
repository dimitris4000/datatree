import {test,expect} from '@playwright/test';

// Chromium's IME protocol exercises actual composition events and CM handling.
// OS input-method UI and assistive technology still require manual checks.
test('source editor supports composition and labelled draft discard',async({page})=>{
 await page.goto('/');
 await expect(page.locator('#revision')).toContainText('Revision 1');
 const source=page.getByRole('textbox',{name:'Document source',exact:true});
 await expect(source).toBeVisible();await source.focus();
 await expect(source).toBeFocused();
 const client=await page.context().newCDPSession(page);
 await client.send('Input.imeSetComposition',{text:'か',selectionStart:1,selectionEnd:1});
 await client.send('Input.imeSetComposition',{text:'漢字',selectionStart:2,selectionEnd:2});
 await client.send('Input.insertText',{text:'漢字😀'});
 await expect(source).toContainText('漢字😀');
 await expect(page.locator('#draft-state')).toHaveText('Unapplied changes');
 expect(await page.locator('#source-pane .cm-line').count()).toBeLessThan(500);
 await page.keyboard.press('Escape');await page.keyboard.press('Tab');
 await expect(page.getByRole('button',{name:'Discard draft',exact:true})).toBeFocused();
 await page.keyboard.press('Enter');
 await expect(page.locator('#draft-state')).toHaveText('No changes');
 await expect(source).not.toContainText('漢字😀');
 await expect(page.locator('#revision')).toContainText('Revision 1');
 await client.detach();
});
