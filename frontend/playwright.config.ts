import { defineConfig } from "@playwright/test";

const deployedBaseURL = process.env.E2E_BASE_URL;

export default defineConfig({
  testDir: "./e2e",
  use: { baseURL: deployedBaseURL ?? "http://127.0.0.1:4173", trace: "retain-on-failure" },
  webServer: deployedBaseURL ? undefined : {
    command: "npm run build && npm run preview -- --host 127.0.0.1 --port 4173",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: false,
    env: { VITE_API_BASE_URL: "https://api.example.test" },
  },
  reporter: "list",
});
