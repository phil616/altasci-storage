import { expect, test } from "@playwright/test";

const localMock = !process.env.E2E_BASE_URL;

const admin = {
  id: "01900000-0000-7000-8000-000000000001",
  email: "admin@example.com",
  role: "admin",
  write_enabled: true,
  status: "active",
  created_at: "2026-09-04T00:00:00Z",
  updated_at: "2026-09-04T00:00:00Z",
};

const member = {
  ...admin,
  id: "01900000-0000-7000-8000-000000000002",
  email: "member@example.com",
  role: "user",
  write_enabled: false,
};

const oidcProvider = {
  id: "01900000-0000-7000-8000-000000000010",
  name: "企业身份认证",
  issuer: "https://id.example.com",
  client_id: "altasci-client",
  scopes: "openid email profile",
  enabled: false,
  auto_create_user: false,
  auto_link_verified_email: true,
  allowed_email_domains: ["example.com"],
  has_client_secret: true,
};

const ossBackend = {
  id: "01900000-0000-7000-8000-000000000020",
  name: "生产 OSS",
  type: "aliyun_oss",
  enabled: true,
  config: {
    region: "cn-hangzhou",
    endpoint: "https://oss-cn-hangzhou.aliyuncs.com",
    bucket: "altasci-storage",
    prefix: "network-storage/",
    use_cname: false,
  },
  has_secret: true,
  project_count: 2,
  blob_count: 5,
  last_test_status: "ok",
  last_test_message: "PUT, HEAD, GET and DELETE succeeded",
};

const settings = {
  "site.public_web_url": "https://web.example.com",
  "site.public_api_url": "https://web-api.example.com",
  "cors.allowed_origins": ["https://web.example.com"],
  "auth.session_idle_timeout": 86400,
  "auth.session_absolute_timeout": 604800,
  "storage.upload_presign_ttl": 900,
  "storage.download_presign_ttl": 300,
  "share.default_expiration": 604800,
  "share.download_presign_ttl": 120,
  "security.trusted_proxy_cidrs": ["127.0.0.1/32"],
  "security.login_rate_limit": { attempts: 10, window_seconds: 600, cooldown_seconds: 900 },
  "security.share_rate_limit": {
    ip_attempts: 5,
    ip_window_seconds: 60,
    ip_ban_seconds: 900,
    escalated_ban_seconds: 3600,
    share_attempts: 50,
    share_window_seconds: 600,
    share_ban_seconds: 900,
  },
};

test.beforeEach(async ({ page }) => {
  test.skip(!localMock, "Mocked UI checks only run against the local Vite server");
  await page.route("https://api.example.test/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    const method = route.request().method();
    if (path === "/api/v1/auth/me") return route.fulfill({ json: admin });
    if (path === "/api/v1/auth/csrf") return route.fulfill({ json: { csrf_token: "csrf" } });
    if (path === "/api/v1/admin/settings") return route.fulfill({ json: settings });
    if (path === "/api/v1/admin/users") return route.fulfill({ json: { items: [admin, member] } });
    if (path === "/api/v1/admin/storage-backends" && method === "GET") return route.fulfill({ json: { items: [ossBackend] } });
    if (path === "/api/v1/admin/oidc-providers" && method === "GET") return route.fulfill({ json: { items: [oidcProvider] } });
    if (path === `/api/v1/admin/oidc-providers/${oidcProvider.id}` && method === "PATCH") return route.fulfill({ json: oidcProvider });
    if (path === `/api/v1/admin/oidc-providers/${oidcProvider.id}` && method === "DELETE") return route.fulfill({ status: 204 });
    if (path === "/api/v1/projects") return route.fulfill({ json: { items: [] } });
    return route.fulfill({ status: 404, json: { error: { code: "NOT_FOUND", message: "Not found", request_id: "test" } } });
  });
});

test("starts with the environment API origin without fetching runtime config", async ({ page }) => {
  const requests: string[] = [];
  page.on("request", request => requests.push(request.url()));
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "项目", exact: true })).toBeVisible();
  expect(requests).toContain("https://api.example.test/api/v1/auth/me");
  expect(requests.some(url => new URL(url).pathname === "/config.json")).toBe(false);
});

test("structured settings reject a non-origin API URL", async ({ page }) => {
  await page.goto("/admin/settings");

  await expect(page.getByRole("heading", { name: "系统设置" })).toBeVisible();
  await expect(page.getByLabel("公开 Web URL")).toHaveValue("https://web.example.com");
  await page.getByLabel("公开 API URL").fill("https://web-api.example.com/");
  await page.getByRole("button", { name: "保存配置" }).click();

  await expect(page.getByText("必须是无路径、无结尾斜杠的 HTTPS Origin")).toBeVisible();
});

test("advanced settings JSON preview uses a readable code surface", async ({ page }) => {
  await page.goto("/admin/settings");
  await page.getByRole("button", { name: "高级：查看将提交的 JSON" }).click();

  const preview = page.locator("pre.config-preview");
  await expect(preview).toBeVisible();
  await expect(preview).toContainText('"site.public_web_url"');
  const colors = await preview.evaluate((element) => {
    const style = getComputedStyle(element);
    return { color: style.color, backgroundColor: style.backgroundColor };
  });
  expect(colors).toEqual({ color: "rgb(38, 38, 38)", backgroundColor: "rgb(250, 250, 250)" });
});

test("logout reloads the application at the login page", async ({ page }) => {
  let signedOut = false;
  await page.route("https://api.example.test/api/v1/auth/me", (route) => signedOut
    ? route.fulfill({ status: 401, json: { error: { code: "UNAUTHENTICATED", message: "Sign in required", request_id: "test" } } })
    : route.fulfill({ json: admin }));
  await page.route("https://api.example.test/api/v1/auth/logout", (route) => {
    signedOut = true;
    return route.fulfill({ status: 204 });
  });

  await page.goto("/projects");
  await page.getByRole("button", { name: /admin@example.com/ }).click();
  await page.getByRole("menuitem", { name: "退出登录" }).click();

  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { name: /欢迎访问\s*AltasCI云盘/ })).toBeVisible();
});

test("OIDC providers expose editable settings and IdP integration details", async ({ page }) => {
  await page.goto("/admin/oidc");
  await expect(page.getByRole("heading", { name: "OpenID Connect" })).toBeVisible();

  await page.getByRole("button", { name: /操作/ }).click();
  await page.getByRole("menuitem", { name: "编辑配置" }).click();
  await expect(page.getByText("编辑 企业身份认证", { exact: true })).toBeVisible();
  await expect(page.getByLabel("显示名称")).toHaveValue("企业身份认证");
  await expect(page.getByLabel("Client ID")).toHaveValue("altasci-client");
  await expect(page.getByLabel("Client Secret")).toHaveValue("");
  await page.getByRole("dialog").getByRole("button", { name: /取\s*消/ }).click();

  await page.getByRole("button", { name: /操作/ }).click();
  await page.getByRole("menuitem", { name: "接入信息" }).click();
  await expect(page.getByText("Redirect URI / Callback URL", { exact: true })).toBeVisible();
  await expect(page.getByText(`https://api.example.test/api/v1/auth/oidc/${oidcProvider.id}/callback`, { exact: true })).toBeVisible();
  await expect(page.getByText("Authorization Code Flow", { exact: true })).toBeVisible();
  await expect(page.getByText(/email_verified/)).toBeVisible();
});

test("OSS storage help explains private access and browser CORS", async ({ page }) => {
  await page.goto("/admin/storage");
  await page.getByRole("button", { name: "OSS 配置帮助" }).click();

  const dialog = page.getByRole("dialog", { name: "Alibaba Cloud OSS 外部配置指南" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText(/私有（Private）/)).toBeVisible();
  await expect(dialog.getByText("http://127.0.0.1:4173", { exact: true })).toBeVisible();
  await expect(dialog.getByText("GET, HEAD, PUT", { exact: true })).toBeVisible();
  await expect(dialog.getByText("ETag, x-oss-request-id, x-oss-hash-crc64ecma", { exact: true })).toBeVisible();
  await expect(dialog.getByText("100 MiB 是分片阈值，不是文件大小上限", { exact: true })).toBeVisible();
  await expect(dialog.locator("pre.config-preview")).toContainText("oss:AbortMultipartUpload");
  await expect(dialog.getByRole("link", { name: "查看阿里云 OSS 官方跨域配置文档" })).toHaveAttribute("href", /help\.aliyun\.com/);
});

test("multipart upload accepts empty presigned headers", async ({ page }) => {
  const projectId = "01900000-0000-7000-8000-000000000030";
  let uploadedParts = 0;
  let completedParts: Array<{ part_number: number; etag: string }> = [];

  await page.route(`https://api.example.test/api/v1/projects/${projectId}`, (route) => route.fulfill({
    json: {
      id: projectId,
      name: "大文件上传测试",
      description: "",
      storage_backend_id: ossBackend.id,
      created_by: admin.id,
      status: "active",
      permission: "admin",
      created_at: "2026-09-04T00:00:00Z",
      updated_at: "2026-09-04T00:00:00Z",
    },
  }));
  await page.route(`https://api.example.test/api/v1/projects/${projectId}/nodes`, (route) => route.fulfill({ json: { items: [] } }));
  await page.route(`https://api.example.test/api/v1/projects/${projectId}/members`, (route) => route.fulfill({ json: { items: [] } }));
  await page.route(`https://api.example.test/api/v1/projects/${projectId}/uploads`, (route) => route.fulfill({
    status: 201,
    json: { upload_id: "multipart-upload", upload_type: "multipart", part_size: 16, part_count: 1 },
  }));
  await page.route("https://api.example.test/api/v1/uploads/multipart-upload/parts/presign", (route) => route.fulfill({
    json: { parts: [{ part_number: 1, url: "https://oss.example.test/part-1", method: "PUT", headers: null }] },
  }));
  await page.route("https://oss.example.test/part-1", (route) => {
    uploadedParts += 1;
    return route.fulfill({
      status: 200,
      headers: {
        ETag: '"etag-1"',
        "Access-Control-Allow-Origin": "http://127.0.0.1:4173",
        "Access-Control-Expose-Headers": "ETag",
      },
    });
  });
  await page.route("https://api.example.test/api/v1/uploads/multipart-upload/complete", (route) => {
    completedParts = (route.request().postDataJSON() as { parts: Array<{ part_number: number; etag: string }> }).parts;
    return route.fulfill({ json: { id: "uploaded-node" } });
  });

  await page.goto(`/projects/${projectId}`);
  await page.locator('input[type="file"]').setInputFiles({
    name: "large-file.bin",
    mimeType: "application/octet-stream",
    buffer: Buffer.from([1]),
  });

  await expect.poll(() => uploadedParts).toBe(1);
  await expect.poll(() => completedParts).toEqual([{ part_number: 1, etag: '"etag-1"' }]);
  await expect(page.getByText("已完成", { exact: true })).toBeVisible();
});

test("storage backends can be edited and explain why referenced entries cannot be deleted", async ({ page }) => {
  let updatePayload: Record<string, unknown> | undefined;
  await page.route(`https://api.example.test/api/v1/admin/storage-backends/${ossBackend.id}`, (route) => {
    if (route.request().method() === "PATCH") {
      updatePayload = route.request().postDataJSON() as Record<string, unknown>;
      return route.fulfill({ json: { ...ossBackend, ...updatePayload } });
    }
    return route.fallback();
  });

  await page.goto("/admin/storage");
  await page.getByRole("button", { name: /操作/ }).click();
  await page.getByRole("menuitem", { name: "编辑配置" }).click();

  const editor = page.getByRole("dialog", { name: "编辑存储后端" });
  await expect(editor.getByLabel("显示名称")).toHaveValue("生产 OSS");
  await expect(editor.getByLabel("Endpoint")).toHaveValue("https://oss-cn-hangzhou.aliyuncs.com");
  await expect(editor.getByLabel("Access Key ID")).toHaveValue("");
  await editor.getByLabel("显示名称").fill("生产 OSS（更新）");
  await editor.getByLabel("对象 Key 前缀").fill("updated-prefix/");
  await editor.getByRole("button", { name: /保\s*存/ }).click();
  await expect.poll(() => updatePayload?.name).toBe("生产 OSS（更新）");
  expect(updatePayload).not.toHaveProperty("secret");
  expect(updatePayload?.config).toMatchObject({ bucket: "altasci-storage", prefix: "updated-prefix/" });

  await page.getByRole("button", { name: /操作/ }).click();
  await page.getByRole("menuitem", { name: "删除" }).click();
  await expect(page.locator(".ant-modal-confirm-title", { hasText: "该存储后端正在使用中" })).toBeVisible();
  await expect(page.getByText(/2 个项目和 5 条对象记录/)).toBeVisible();
});

test("public share uses large four-digit numeric code inputs", async ({ page }) => {
  let submittedCode = "";
  await page.setViewportSize({ width: 375, height: 760 });
  await page.route("https://api.example.test/api/v1/public/shares/share-token/", (route) => route.fulfill({
    json: {
      share: {
        id: "share-id",
        project_id: "project-id",
        target_node_id: "node-id",
        require_code: true,
        code_length: 4,
        expires_at: "2026-09-11T00:00:00Z",
        disabled_at: null,
        created_at: "2026-09-04T00:00:00Z",
      },
      target: { id: "node-id", project_id: "project-id", name: "季度报告.pdf", node_type: "file", size: 1024, mime_type: "application/pdf", created_at: "2026-09-04T00:00:00Z", updated_at: "2026-09-04T00:00:00Z" },
      grant_required: true,
    },
  }));
  await page.route("https://api.example.test/api/v1/public/shares/share-token/verify", (route) => {
    submittedCode = (route.request().postDataJSON() as { code: string }).code;
    return route.fulfill({ json: { grant: "share-grant", expires_at: "2026-09-04T00:30:00Z" } });
  });
  await page.route("https://api.example.test/api/v1/public/shares/share-token/nodes", (route) => route.fulfill({ json: { items: [] } }));

  await page.goto("/s/share-token");
  await expect(page.getByText("AltasCI云盘", { exact: true })).toBeVisible();
  await expect(page.locator(".public-share-logo")).toHaveAttribute("src", "https://cdn.altasci.com/library/logos/altasci-logo-abg.png");
  await expect(page.getByText("ALTASCI SECURE SHARE", { exact: true })).toHaveCount(0);
  await expect(page.getByText("4 位数字提取码", { exact: true })).toBeVisible();
  const inputs = page.locator(".share-code-otp input");
  await expect(inputs).toHaveCount(4);
  await expect(inputs.first()).toHaveAttribute("inputmode", "numeric");
  const firstInputBox = await inputs.first().boundingBox();
  expect(firstInputBox?.width).toBeGreaterThanOrEqual(56);
  expect(firstInputBox?.height).toBeGreaterThanOrEqual(56);

  await inputs.first().fill("A");
  await expect(inputs.first()).toHaveValue("");
  for (const [index, digit] of ["4", "8", "2", "7"].entries()) await inputs.nth(index).fill(digit);
  await expect.poll(() => submittedCode).toBe("4827");
  await expect(page.getByText("AltasCI云盘", { exact: true })).toBeVisible();
  await expect(page.locator(".public-enterprise-browser .public-share-logo")).toBeVisible();
});

test("share code automatic verification runs once and preserves manual fallback", async ({ page }) => {
  const attempts: string[] = [];
  await page.setViewportSize({ width: 375, height: 760 });
  await page.route("https://api.example.test/api/v1/public/shares/share-token/", (route) => route.fulfill({
    json: {
      share: {
        id: "share-id",
        project_id: "project-id",
        target_node_id: "node-id",
        require_code: true,
        code_length: 4,
        expires_at: "2026-09-11T00:00:00Z",
        disabled_at: null,
        created_at: "2026-09-04T00:00:00Z",
      },
      target: { id: "node-id", project_id: "project-id", name: "季度报告.pdf", node_type: "file", size: 1024, mime_type: "application/pdf", created_at: "2026-09-04T00:00:00Z", updated_at: "2026-09-04T00:00:00Z" },
      grant_required: true,
    },
  }));
  await page.route("https://api.example.test/api/v1/public/shares/share-token/verify", (route) => {
    const code = (route.request().postDataJSON() as { code: string }).code;
    attempts.push(code);
    if (attempts.length === 1) return route.fulfill({
      status: 401,
      json: { error: { code: "SHARE_CODE_INVALID", message: "The share code is invalid.", request_id: "test" } },
    });
    return route.fulfill({ json: { grant: "share-grant", expires_at: "2026-09-04T00:30:00Z" } });
  });
  await page.route("https://api.example.test/api/v1/public/shares/share-token/nodes", (route) => route.fulfill({ json: { items: [] } }));

  await page.goto("/s/share-token");
  const inputs = page.locator(".share-code-otp input");
  for (const [index, digit] of ["1", "1", "1", "1"].entries()) await inputs.nth(index).fill(digit);

  await expect.poll(() => attempts).toEqual(["1111"]);
  await expect(page.locator(".share-code-form .ant-alert").getByText("The share code is invalid.", { exact: true })).toBeVisible();

  for (const [index, digit] of ["2", "2", "2", "2"].entries()) await inputs.nth(index).fill(digit);
  await page.waitForTimeout(200);
  expect(attempts).toEqual(["1111"]);

  await page.getByRole("button", { name: "查看分享" }).click();
  await expect.poll(() => attempts).toEqual(["1111", "2222"]);
  await expect(page.locator(".public-enterprise-browser")).toBeVisible();
});

test("mobile navigation remains available when the desktop menu is collapsed", async ({ page }) => {
  await page.setViewportSize({ width: 480, height: 900 });
  await page.goto("/admin/settings");

  await expect(page.getByRole("button", { name: "前往项目列表" })).toContainText("AltasCI云盘");
  await expect(page.locator(".admin-mobile-nav .ant-select")).toBeVisible();
  await page.getByRole("button", { name: "打开主导航" }).click();
  await expect(page.getByRole("menuitem", { name: "项目" })).toBeVisible();
  await expect(page.getByRole("menuitem", { name: "管理中心" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
});

test("wide application header stays aligned with the content container", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 900 });
  await page.goto("/projects");

  const [header, content] = await Promise.all([
    page.locator(".app-header-inner").boundingBox(),
    page.locator(".app-content").boundingBox(),
  ]);
  expect(header).not.toBeNull();
  expect(content).not.toBeNull();
  expect(header?.x).toBe(content?.x);
  expect(header?.width).toBe(content?.width);
});

test("login page presents the seven-day policy without a logo", async ({ page }) => {
  await page.route("https://api.example.test/api/v1/auth/me", (route) => route.fulfill({
    status: 401,
    json: { error: { code: "UNAUTHENTICATED", message: "Sign in required", request_id: "test" } },
  }));

  await page.goto("/login");

  await expect(page.getByRole("heading", { name: /欢迎访问\s*AltasCI云盘/ })).toBeVisible();
  await expect(page.getByText("只允许受限用户访问数据。", { exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "登录", exact: true })).toBeVisible();
  await expect(page.getByRole("complementary", { name: "产品说明" })).toBeVisible();
  await expect(page.getByRole("checkbox", { name: /7 天内保持登录/ })).not.toBeChecked();
  await expect(page.getByText(/不勾选时关闭浏览器后需重新登录/)).toBeVisible();
  const introduction = page.getByRole("complementary", { name: "产品说明" });
  const loginForm = page.locator(".login-form-panel");
  const [introductionBox, loginBox] = await Promise.all([introduction.boundingBox(), loginForm.boundingBox()]);
  expect(introductionBox?.x).toBeLessThan(loginBox?.x ?? 0);
  expect(Math.abs((introductionBox?.width ?? 0) - (loginBox?.width ?? 0))).toBeLessThanOrEqual(1);
  await expect(introduction).toHaveCSS("background-color", "rgb(20, 20, 20)");
  await expect(page.getByText("只允许受限用户访问数据。", { exact: true })).toHaveCSS("color", "rgba(255, 255, 255, 0.72)");
  await expect(page.getByRole("button", { name: /登\s*录/ })).toHaveCSS("background-color", "rgb(20, 20, 20)");
  await expect(page.locator(".brand-mark, .brand-mark-small")).toHaveCount(0);
  await expect(page.locator('link[rel="icon"]')).toHaveAttribute("href", "https://cdn.altasci.com/library/logos/altasci-slogo-abg.ico");
});

test("login page remains usable at the minimum supported width", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 760 });
  await page.route("https://api.example.test/api/v1/auth/me", (route) => route.fulfill({
    status: 401,
    json: { error: { code: "UNAUTHENTICATED", message: "Sign in required", request_id: "test" } },
  }));

  await page.goto("/login");

  await expect(page.getByLabel("邮箱")).toBeVisible();
  await expect(page.getByLabel("密码")).toBeVisible();
  await expect(page.getByRole("button", { name: /登\s*录/ })).toBeVisible();
  const [formBox, introductionBox, submitBox] = await Promise.all([
    page.locator(".login-form-panel").boundingBox(),
    page.locator(".login-info-panel").boundingBox(),
    page.getByRole("button", { name: /登\s*录/ }).boundingBox(),
  ]);
  expect(formBox?.y).toBeLessThan(introductionBox?.y ?? 0);
  expect(submitBox?.y ?? 760).toBeLessThan(760);
  expect((submitBox?.y ?? 760) + (submitBox?.height ?? 0)).toBeLessThanOrEqual(760);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
});

test("remember login is opt-in and sent explicitly", async ({ page }) => {
  const payloads: Array<{ remember?: boolean }> = [];
  await page.route("https://api.example.test/api/v1/auth/me", (route) => route.fulfill({
    status: 401,
    json: { error: { code: "UNAUTHENTICATED", message: "Sign in required", request_id: "test" } },
  }));
  await page.route("https://api.example.test/api/v1/auth/login", (route) => {
    payloads.push(route.request().postDataJSON() as { remember?: boolean });
    if (payloads.length === 1) return route.fulfill({
      status: 401,
      json: { error: { code: "INVALID_CREDENTIALS", message: "Invalid credentials", request_id: "test" } },
    });
    return route.fulfill({ json: { user: admin, csrf_token: "csrf" } });
  });

  await page.goto("/login");
  await page.getByLabel("邮箱").fill("admin@example.com");
  await page.getByLabel("密码").fill("password-for-contract-test");
  await page.getByRole("button", { name: /登\s*录/ }).click();
  await expect.poll(() => payloads.length).toBe(1);
  expect(payloads[0].remember).toBe(false);

  await page.getByRole("checkbox", { name: /7 天内保持登录/ }).check();
  await page.getByRole("button", { name: /登\s*录/ }).click();
  await expect.poll(() => payloads.length).toBe(2);
  expect(payloads[1].remember).toBe(true);
});

test("admin data tables keep primary actions usable on a narrow screen", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 760 });
  await page.goto("/admin/users");

  await expect(page.getByRole("heading", { name: "用户管理" })).toBeVisible();
  await expect(page.getByText("member@example.com")).toBeVisible();
  await expect(page.getByRole("button", { name: /操作/ }).last()).toBeVisible();
  const [adminContent, table] = await Promise.all([
    page.locator(".admin-content").boundingBox(),
    page.locator(".ant-table-wrapper").boundingBox(),
  ]);
  expect(adminContent?.width ?? 0).toBeGreaterThanOrEqual(296);
  expect(table?.width ?? 0).toBeGreaterThanOrEqual(296);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
});

test("primary application pages avoid document-level overflow at the minimum width", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 760 });
  const pages = [
    ["/projects", "项目"],
    ["/shares", "我的分享"],
    ["/settings/profile", "个人设置"],
    ["/admin/users", "用户管理"],
    ["/admin/storage", "存储后端"],
    ["/admin/oidc", "OpenID Connect"],
    ["/admin/settings", "系统设置"],
  ] as const;

  for (const [path, heading] of pages) {
    await page.goto(path);
    await expect(page.getByRole("heading", { name: heading, exact: true })).toBeVisible();
    const fitsViewport = await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth);
    expect(fitsViewport, `${path} should not create document-level horizontal scrolling`).toBe(true);
  }
});
