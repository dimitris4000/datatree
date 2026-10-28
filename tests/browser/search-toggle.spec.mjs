import { test, expect } from '@playwright/test';

test('Search is hidden by default and can be toggled independently', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('#revision')).toContainText('Revision 1');
  const toggle = page.getByRole('button', { name: 'Search', exact: true });
  const panel = page.locator('#find-panel');
  await expect(panel).toBeHidden();
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  await toggle.click();
  await expect(panel).toBeVisible();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await page.locator('#find-text').fill('saved search');
  await toggle.click();
  await expect(panel).toBeHidden();
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  await toggle.click();
  await expect(panel).toBeVisible();
  await expect(page.locator('#find-text')).toHaveValue('saved search');
  await expect(page.locator('#diff-panel')).toBeHidden();
  await expect(page.locator('#validation-panel')).toBeHidden();
});
