import { expect, test } from "@playwright/test";

const node = { id: "file", project_id: "project", name: "报告.pdf", node_type: "file", size: 1024, updated_at: "2026-10-02T00:00:00Z" };

test.beforeEach(async ({ page }) => {
  test.skip(Boolean(process.env.E2E_BASE_URL), "Uses local API mocks");
  await page.route("https://api.example.test/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/v1/auth/me") return route.fulfill({ json: { id: "user", role: "admin", write_enabled: true } });
    if (path === "/api/v1/auth/csrf") return route.fulfill({ json: { csrf_token: "csrf" } });
    if (path === "/api/v1/projects/project") return route.fulfill({ json: { id: "project", name: "项目", permission: "admin" } });
    if (path === "/api/v1/projects/project/nodes") return route.fulfill({ json: { items: [node] } });
    if (path === "/api/v1/nodes/file/shares") return route.fulfill({ json: { url: "http://127.0.0.1:4173/s/token", code: "0123", require_code: true } });
    if (path === "/api/v1/public/shares/token/") return route.fulfill({ json: { share: { code_length: 4 }, target: node, grant_required: true } });
    if (path === "/api/v1/public/shares/token/nodes") {
      expect(route.request().headers().authorization).toBe("Bearer grant");
      return route.fulfill({ json: { items: [node] } });
    }
    return route.fulfill({ json: { items: [] } });
  });
});

test("share link checkbox adds the password and opens the file list automatically", async ({ page }) => {
  const attempts: string[] = [];
  await page.route("**/public/shares/token/verify", (route) => {
    attempts.push(route.request().postDataJSON().code);
    return route.fulfill({ json: { grant: "grant" } });
  });
  await page.goto("/projects/project");
  await page.getByRole("button", { name: /操作$/ }).click();
  await page.getByText("创建分享", { exact: true }).click();
  const dialog = page.getByRole("dialog");
  const link = dialog.locator(".ant-typography code");
  await expect(link).toHaveText("http://127.0.0.1:4173/s/token");
  await dialog.getByLabel("携带密码").check();
  await expect(link).toHaveText("http://127.0.0.1:4173/s/token?code=0123");
  const generatedURL = await link.innerText();
  await dialog.getByLabel("携带密码").uncheck();
  await expect(link).toHaveText("http://127.0.0.1:4173/s/token");
  await page.goto(generatedURL);
  await expect(page.locator(".public-enterprise-browser")).toBeVisible();
  await expect(page.getByRole("button", { name: /下载$/ })).toBeVisible();
  await expect(page.locator(".share-code-form")).toHaveCount(0);
  expect(attempts).toEqual(["0123"]);
});

test("invalid link password is tried once and allows manual correction", async ({ page }) => {
  const attempts: string[] = [];
  await page.route("**/public/shares/token/verify", (route) => {
    attempts.push(route.request().postDataJSON().code);
    return attempts.length === 1
      ? route.fulfill({ status: 401, json: { error: { code: "SHARE_CODE_INVALID" } } })
      : route.fulfill({ json: { grant: "grant" } });
  });
  await page.goto("/s/token?code=1111");
  await expect(page.getByRole("alert")).toContainText("The share code is invalid.");
  const inputs = page.locator(".share-code-otp input");
  for (const [index, digit] of [..."0123"].entries()) {
    await expect(inputs.nth(index)).toHaveValue("1");
    await inputs.nth(index).fill(digit);
  }
  expect(attempts).toEqual(["1111"]);
  await page.getByRole("button", { name: "查看分享" }).click();
  await expect(page.locator(".public-enterprise-browser")).toBeVisible();
  expect(attempts).toEqual(["1111", "0123"]);
});

test("multiple selections create one share and saved credentials remain visible", async ({ page }) => {
  const url = "http://127.0.0.1:4173/s/01234-56789-01234-56789-01234-56789";
  const share = { id: "share", target_node_id: "file", target_node_ids: ["file", "second"], url, code: "0123", require_code: true, created_at: "2026-10-02T00:00:00Z" };
  await page.route("**/projects/project/nodes", route => route.fulfill({ json: { items: [node, { ...node, id: "second", name: "第二份.pdf" }] } }));
  await page.route("**/api/v1/shares", route => {
    if (route.request().method() === "POST") {
      expect(route.request().postDataJSON()).toEqual({ require_code: true, node_ids: ["file", "second"] });
      return route.fulfill({ status: 201, json: share });
    }
    return route.fulfill({ json: { items: [share] } });
  });
  await page.goto("/projects/project");
  await page.locator(".ant-table-thead input[type=checkbox]").first().check();
  await page.getByRole("button", { name: "批量分享（2）" }).click();
  await expect(page.getByRole("dialog")).toContainText(url);
  await page.goto("/shares");
  await expect(page.getByText(url, { exact: true })).toBeVisible();
  await expect(page.getByText("0123", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "查看分享信息" }).click();
  await page.getByRole("dialog").getByLabel("携带密码").check();
  await expect(page.getByRole("dialog")).toContainText(url + "?code=0123");
});
