import { expect, test } from "@playwright/test";

test("API key creation, one-time disclosure, policy update and revocation", async ({ page }) => {
  test.skip(!!process.env.E2E_BASE_URL, "Uses local API fixtures");
  const secret = "altasci_test-secret-only-shown-once";
  let keys: Record<string, unknown>[] = [];
  await page.route("https://api.example.test/**", async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === "/api/v1/auth/me") return route.fulfill({ json: { id: "user", email: "user@example.test", role: "user", status: "active", write_enabled: true } });
    if (path === "/api/v1/auth/csrf") return route.fulfill({ json: { csrf_token: "csrf" } });
    if (path === "/api/v1/projects") return route.fulfill({ json: { items: [{ id: "project", name: "备份项目", permission: "write" }] } });
    if (path === "/api/v1/api-keys" && request.method() === "GET") return route.fulfill({ json: { items: keys } });
    if (path === "/api/v1/api-keys" && request.method() === "POST") {
      expect(request.headers()["x-csrf-token"]).toBe("csrf");
      const body = request.postDataJSON();
      expect(body.scopes).toEqual(["projects:read", "files:read"]);
      expect(body.all_projects).toBe(false);
      expect(body.project_ids).toEqual(["project"]);
      keys = [{ ...body, id: "key", token_prefix: "altasci_test", created_at: new Date().toISOString(), last_used_at: null, revoked_at: null }];
      return route.fulfill({ status: 201, json: { ...keys[0], token: secret } });
    }
    if (path === "/api/v1/api-keys/key" && request.method() === "PUT") {
      expect(request.postDataJSON().all_projects).toBe(true);
      expect(request.postDataJSON().project_ids).toEqual([]);
      keys = [{ ...keys[0], ...request.postDataJSON() }];
      return route.fulfill({ json: keys[0] });
    }
    if (path === "/api/v1/api-keys/key" && request.method() === "DELETE") {
      keys = [{ ...keys[0], revoked_at: new Date().toISOString() }];
      return route.fulfill({ status: 204 });
    }
    return route.fulfill({ status: 404 });
  });
  await page.goto("/settings/api-keys");
  await expect(page.getByRole("heading", { name: "API 密钥" })).toBeVisible();
  await page.getByRole("button", { name: "创建 API 密钥", exact: true }).click();
  await page.getByLabel("名称", { exact: true }).fill("每日备份");
  await page.getByLabel("指定项目").click();
  await page.getByText("备份项目", { exact: true }).click();
  await page.getByLabel("名称", { exact: true }).click();
  await page.getByRole("button", { name: /确.*定|OK/ }).click();
  await expect(page.getByText(secret, { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "我已保存" }).click();
  await expect(page.getByText(secret, { exact: true })).not.toBeVisible();
  await page.getByRole("button", { name: "编辑权限" }).click();
  await page.getByLabel("名称", { exact: true }).fill("更新的备份");
  await page.getByLabel("所有当前及未来有权访问的项目").check();
  await page.getByRole("button", { name: /确.*定|OK/ }).click();
  await expect(page.getByText("更新的备份", { exact: true })).toBeVisible();
  await expect(page.getByText(secret, { exact: true })).not.toBeVisible();
  await page.getByRole("button", { name: /^撤\s*销$/ }).click();
  await page.getByRole("button", { name: /确.*定|OK/ }).click();
  await expect(page.getByText("已撤销", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "编辑权限" })).toHaveCount(0);
});
