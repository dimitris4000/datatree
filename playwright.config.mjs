import { defineConfig } from '@playwright/test';
const port=Number(process.env.DATATREE_BROWSER_PORT||18917);
export default defineConfig({
 testDir:'./tests/browser',testMatch:'*.spec.mjs',workers:1,fullyParallel:false,
 timeout:180000,expect:{timeout:60000},reporter:'list',
 outputDir:process.env.DATATREE_BROWSER_ARTIFACTS||'/tmp/datatree-browser-artifacts',
 use:{baseURL:`http://127.0.0.1:${port}`,browserName:'chromium',headless:true,viewport:{width:1440,height:900},trace:'off',screenshot:'off',video:'off'},
 webServer:{command:'node tests/browser/server.mjs',url:`http://127.0.0.1:${port}`,timeout:180000,reuseExistingServer:false,gracefulShutdown:{signal:'SIGTERM',timeout:5000}},
});
