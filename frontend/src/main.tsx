import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntApp, Button, ConfigProvider, Result } from "antd";
import zhCN from "antd/locale/zh_CN";
import { StrictMode, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "react-router-dom";
import { configureAPI } from "./api/client";
import { AuthProvider } from "./auth/AuthProvider";
import { router } from "./routes/router";
import { enterpriseTheme } from "./theme";
import { UploadProvider } from "./upload/UploadManager";
import "antd/dist/reset.css";
import "./styles.css";

type Bootstrap = { apiBaseUrl: string };
const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: 15_000, retry: 1 } } });
const root = createRoot(document.getElementById("root")!);

function providers(children: ReactNode) {
  return (
    <StrictMode>
      <ConfigProvider locale={zhCN} theme={enterpriseTheme}>
        <AntApp>
          {children}
        </AntApp>
      </ConfigProvider>
    </StrictMode>
  );
}

function parseAPIBaseUrl(value: unknown) {
  if (typeof value !== "string") throw new Error("apiBaseUrl 必须是字符串");
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error("apiBaseUrl 不是有效 URL");
  }
  if (parsed.protocol !== "https:") throw new Error("apiBaseUrl 必须使用 HTTPS");
  if (parsed.username || parsed.password || parsed.pathname !== "/" || parsed.search || parsed.hash || parsed.origin !== value) {
    throw new Error("apiBaseUrl 只能填写 HTTPS Origin，不能包含路径、凭据、查询参数或锚点");
  }
  return parsed.origin;
}

async function bootstrap() {
  try {
    const response = await fetch("/config.json", { cache: "no-store" });
    if (!response.ok) throw new Error(`无法加载 /config.json（HTTP ${response.status}）`);
    const config = await response.json() as Bootstrap;
    configureAPI(parseAPIBaseUrl(config.apiBaseUrl));
    root.render(providers(
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <UploadProvider>
            <RouterProvider router={router} />
          </UploadProvider>
        </AuthProvider>
      </QueryClientProvider>,
    ));
  } catch (reason) {
    const detail = reason instanceof Error ? reason.message : "未知配置错误";
    root.render(providers(
      <main className="bootstrap-error">
        <Result
          status="error"
          title="前端运行配置不可用"
          subTitle={`${detail}。请检查部署目录中的 config.json。`}
          extra={<Button type="primary" onClick={() => void bootstrap()}>重新加载</Button>}
        />
      </main>,
    ));
  }
}

void bootstrap();
