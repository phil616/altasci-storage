import { expect, test } from "@playwright/test";

test("CLI discovery publishes the API origin used by the production Web build", async ({ request }) => {
  test.skip(Boolean(process.env.E2E_BASE_URL), "Uses the controlled local build configuration");
  const response = await request.get("/.well-known/altasci.json");
  expect(response.status()).toBe(200);
  expect(response.headers()["content-type"]).toContain("application/json");
  expect(await response.json()).toEqual({ api_url: "https://api.example.test" });
});
