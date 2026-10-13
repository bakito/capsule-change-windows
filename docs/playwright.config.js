import { defineConfig, devices } from '@playwright/test';

const baseURL = 'http://127.0.0.1:1413/schedule-actions/';

export default defineConfig({
  testDir: './tests',
  workers: 1,
  use: {
    baseURL,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    launchOptions: { executablePath: process.env.CHROMIUM_EXECUTABLE || undefined },
  },
  projects: [
    { name: 'desktop', use: { viewport: { width: 1440, height: 1050 } } },
    { name: 'android', use: { ...devices['Pixel 7'] } },
    { name: 'mobile', use: { ...devices['iPhone 13'], defaultBrowserType: 'chromium' } },
  ],
  webServer: {
    command: `npm run dev -- --renderToMemory --bind 127.0.0.1 --port 1413 --baseURL ${baseURL} --appendPort=false`,
    url: baseURL,
    reuseExistingServer: false,
  },
});
