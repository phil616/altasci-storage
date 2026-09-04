import { useQuery } from "@tanstack/react-query";
import {
  CheckCircleFilled,
  LockOutlined,
  MailOutlined,
} from "@ant-design/icons";
import { Alert, Button, Card, Checkbox, ConfigProvider, Divider, Form, Input, Space, Typography, type ThemeConfig } from "antd";
import { useState } from "react";
import { Navigate, useLocation, useNavigate } from "react-router-dom";
import { api, APIError } from "../api/client";
import { useAuth } from "../auth/AuthProvider";

const loginTheme: ThemeConfig = {
  token: {
    colorPrimary: "#141414",
    colorInfo: "#141414",
    colorLink: "#141414",
    colorPrimaryHover: "#262626",
    colorPrimaryActive: "#000000",
  },
  components: {
    Button: { primaryShadow: "none" },
  },
};

export function LoginPage() {
  const auth = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [remember, setRemember] = useState(false);
  const providers = useQuery({ queryKey: ["public-oidc-providers"], queryFn: () => api.request<{ items: Array<{ id: string; name: string }> }>("/api/v1/auth/oidc/providers"), retry: false });
  if (auth.user) return <Navigate to="/projects" replace />;
  async function submit(values: { email: string; password: string }) {
    setBusy(true); setError("");
    try {
      await auth.login(values.email, values.password, remember);
      const destination = (location.state as { from?: { pathname?: string } } | null)?.from?.pathname ?? "/projects";
      navigate(destination, { replace: true });
    } catch (reason) {
      setError(reason instanceof APIError ? `${reason.message}${reason.requestId ? `（请求 ${reason.requestId}）` : ""}` : "登录失败");
    } finally { setBusy(false); }
  }
  return (
    <ConfigProvider theme={loginTheme}>
      <main className="login-page">
        <div className="login-container">
          <Card className="login-card" variant="borderless">
            <div className="login-grid">
              <aside className="login-info-panel" aria-label="产品说明">
                <Typography.Title level={1}>欢迎访问<br></br>AltasCI云盘</Typography.Title>
                <Typography.Paragraph>只允许受限用户访问数据。</Typography.Paragraph>
                <div className="login-security-note"><CheckCircleFilled /> 请使用已获授权的企业账户登录</div>
              </aside>

              <section className="login-form-panel" aria-labelledby="login-title">
                <Space direction="vertical" size={4} className="login-heading">
                  <Typography.Title level={2} id="login-title">登录</Typography.Title>
                  <Typography.Text type="secondary">使用您的企业账号继续访问</Typography.Text>
                </Space>
                <Form layout="vertical" size="large" onFinish={(values) => void submit(values)} requiredMark={false}>
                  <Form.Item name="email" label="邮箱" rules={[{ required: true, message: "请输入邮箱" }, { type: "email", message: "邮箱格式不正确" }]}>
                    <Input prefix={<MailOutlined />} autoComplete="username" placeholder="name@example.com" />
                  </Form.Item>
                  <Form.Item name="password" label="密码" rules={[{ required: true, message: "请输入密码" }]}>
                    <Input.Password prefix={<LockOutlined />} autoComplete="current-password" placeholder="请输入密码" />
                  </Form.Item>
                  {error && <Form.Item><Alert type="error" showIcon title={error} /></Form.Item>}
                  <div className="login-session-policy">
                    <Checkbox checked={remember} onChange={(event) => setRemember(event.target.checked)}>
                      <span className="login-session-copy">
                        <Typography.Text strong>7 天内保持登录</Typography.Text>
                        <Typography.Text type="secondary">默认不启用；不勾选时关闭浏览器后需重新登录。会话仍受管理员安全策略限制。</Typography.Text>
                      </span>
                    </Checkbox>
                  </div>
                  <Button type="primary" htmlType="submit" loading={busy} block>登录</Button>
                </Form>
                {providers.data?.items.length ? (
                  <div className="oidc-options">
                    <Divider plain>企业身份登录</Divider>
                    <Space direction="vertical" className="full-width">
                      {providers.data.items.map((provider) => (
                        <Button block key={provider.id} onClick={() => window.location.assign(`${api.baseUrl}/api/v1/auth/oidc/${provider.id}/start${remember ? "?remember=true" : ""}`)}>{provider.name}</Button>
                      ))}
                    </Space>
                  </div>
                ) : null}
              </section>
            </div>
          </Card>
          <footer className="login-footer">
            <Typography.Text type="secondary">© {new Date().getFullYear()} AltasCI企业云盘</Typography.Text>
          </footer>
        </div>
      </main>
    </ConfigProvider>
  );
}
