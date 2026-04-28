import { test, expect } from '@playwright/test';

test.describe('Dashboard Smoke Tests', () => {
  // Test authentication fallback when API key is missing
  test('redirects to login when unauthorized', async ({ page }) => {
    await page.goto('/');
    
    // Should end up on the login page
    await expect(page.locator('text=Authentication Required')).toBeVisible();
    await expect(page.locator('input[type="password"]')).toBeVisible();
  });
});
