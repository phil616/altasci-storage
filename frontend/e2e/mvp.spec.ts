import { expect, test } from "@playwright/test";

const enabled = Boolean(process.env.E2E_BASE_URL && process.env.E2E_ADMIN_EMAIL && process.env.E2E_ADMIN_PASSWORD);
test.skip(!enabled, "Set E2E_BASE_URL, E2E_ADMIN_EMAIL and E2E_ADMIN_PASSWORD for a deployed test environment");

test("administrator login and project browser", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("邮箱").fill(process.env.E2E_ADMIN_EMAIL!);
  await page.getByLabel("密码").fill(process.env.E2E_ADMIN_PASSWORD!);
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(/\/projects$/);
  await expect(page.getByRole("heading", { name: "项目" })).toBeVisible();
  await page.getByRole("menuitem", { name: /管理中心/ }).click();
  await expect(page.getByRole("heading", { name: "用户管理" })).toBeVisible();
  await page.getByRole("menuitem", { name: /系统设置/ }).click();
  await expect(page.getByRole("heading", { name: "系统设置" })).toBeVisible();
  await expect(page.getByText("公开地址与 CORS")).toBeVisible();
});
