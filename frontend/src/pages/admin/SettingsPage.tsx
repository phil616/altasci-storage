import {
  ApiOutlined,
  DeleteOutlined,
  GlobalOutlined,
  PlusOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  SaveOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  App,
  Button,
  Card,
  Col,
  Collapse,
  Form,
  Input,
  InputNumber,
  Row,
  Space,
  Spin,
  Tooltip,
  Typography,
} from "antd";
import { useEffect, useMemo, useState } from "react";
import { api } from "../../api/client";

type LoginRateLimit = {
  attempts: number;
  window_seconds: number;
  cooldown_seconds: number;
};

type ShareRateLimit = {
  ip_attempts: number;
  ip_window_seconds: number;
  ip_ban_seconds: number;
  escalated_ban_seconds: number;
  share_attempts: number;
  share_window_seconds: number;
  share_ban_seconds: number;
};

type SettingsFormValues = {
  publicWebUrl: string;
  publicApiUrl: string;
  allowedOrigins: string[];
  sessionIdleTimeout: number;
  sessionAbsoluteTimeout: number;
  uploadPresignTTL: number;
  downloadPresignTTL: number;
  shareDefaultExpiration: number;
  shareDownloadPresignTTL: number;
  trustedProxyCIDRs: string[];
  loginRateLimit: LoginRateLimit;
  shareRateLimit: ShareRateLimit;
};

type SettingsPayload = Record<string, unknown>;

const defaults: SettingsFormValues = {
  publicWebUrl: "",
  publicApiUrl: "",
  allowedOrigins: [],
  sessionIdleTimeout: 86_400,
  sessionAbsoluteTimeout: 604_800,
  uploadPresignTTL: 900,
  downloadPresignTTL: 300,
  shareDefaultExpiration: 604_800,
  shareDownloadPresignTTL: 120,
  trustedProxyCIDRs: [],
  loginRateLimit: { attempts: 10, window_seconds: 600, cooldown_seconds: 900 },
  shareRateLimit: {
    ip_attempts: 5,
    ip_window_seconds: 60,
    ip_ban_seconds: 900,
    escalated_ban_seconds: 3_600,
    share_attempts: 50,
    share_window_seconds: 600,
    share_ban_seconds: 900,
  },
};

function value<T>(payload: SettingsPayload, key: string, fallback: T): T {
  return (payload[key] ?? fallback) as T;
}

function fromPayload(payload: SettingsPayload): SettingsFormValues {
  return {
    publicWebUrl: value(payload, "site.public_web_url", defaults.publicWebUrl),
    publicApiUrl: value(payload, "site.public_api_url", defaults.publicApiUrl),
    allowedOrigins: value(payload, "cors.allowed_origins", defaults.allowedOrigins),
    sessionIdleTimeout: value(payload, "auth.session_idle_timeout", defaults.sessionIdleTimeout),
    sessionAbsoluteTimeout: value(payload, "auth.session_absolute_timeout", defaults.sessionAbsoluteTimeout),
    uploadPresignTTL: value(payload, "storage.upload_presign_ttl", defaults.uploadPresignTTL),
    downloadPresignTTL: value(payload, "storage.download_presign_ttl", defaults.downloadPresignTTL),
    shareDefaultExpiration: value(payload, "share.default_expiration", defaults.shareDefaultExpiration),
    shareDownloadPresignTTL: value(payload, "share.download_presign_ttl", defaults.shareDownloadPresignTTL),
    trustedProxyCIDRs: value(payload, "security.trusted_proxy_cidrs", defaults.trustedProxyCIDRs),
    loginRateLimit: value(payload, "security.login_rate_limit", defaults.loginRateLimit),
    shareRateLimit: value(payload, "security.share_rate_limit", defaults.shareRateLimit),
  };
}

function toPayload(values: SettingsFormValues): SettingsPayload {
  return {
    "site.public_web_url": values.publicWebUrl.trim(),
    "site.public_api_url": values.publicApiUrl.trim(),
    "cors.allowed_origins": values.allowedOrigins.map((origin) => origin.trim()),
    "auth.session_idle_timeout": values.sessionIdleTimeout,
    "auth.session_absolute_timeout": values.sessionAbsoluteTimeout,
    "storage.upload_presign_ttl": values.uploadPresignTTL,
    "storage.download_presign_ttl": values.downloadPresignTTL,
    "share.default_expiration": values.shareDefaultExpiration,
    "share.download_presign_ttl": values.shareDownloadPresignTTL,
    "security.trusted_proxy_cidrs": values.trustedProxyCIDRs.map((cidr) => cidr.trim()),
    "security.login_rate_limit": values.loginRateLimit,
    "security.share_rate_limit": values.shareRateLimit,
  };
}

function mergeDefaults(values?: Partial<SettingsFormValues>): SettingsFormValues {
  return {
    publicWebUrl: values?.publicWebUrl ?? defaults.publicWebUrl,
    publicApiUrl: values?.publicApiUrl ?? defaults.publicApiUrl,
    allowedOrigins: values?.allowedOrigins ?? defaults.allowedOrigins,
    sessionIdleTimeout: values?.sessionIdleTimeout ?? defaults.sessionIdleTimeout,
    sessionAbsoluteTimeout: values?.sessionAbsoluteTimeout ?? defaults.sessionAbsoluteTimeout,
    uploadPresignTTL: values?.uploadPresignTTL ?? defaults.uploadPresignTTL,
    downloadPresignTTL: values?.downloadPresignTTL ?? defaults.downloadPresignTTL,
    shareDefaultExpiration: values?.shareDefaultExpiration ?? defaults.shareDefaultExpiration,
    shareDownloadPresignTTL: values?.shareDownloadPresignTTL ?? defaults.shareDownloadPresignTTL,
    trustedProxyCIDRs: values?.trustedProxyCIDRs ?? defaults.trustedProxyCIDRs,
    loginRateLimit: { ...defaults.loginRateLimit, ...values?.loginRateLimit },
    shareRateLimit: { ...defaults.shareRateLimit, ...values?.shareRateLimit },
  };
}

function validateHTTPSOrigin(raw: unknown) {
  if (typeof raw !== "string" || raw.trim() === "") return Promise.reject(new Error("请输入 HTTPS Origin"));
  try {
    const parsed = new URL(raw);
    if (parsed.protocol !== "https:" || parsed.origin !== raw || parsed.username || parsed.password || parsed.search || parsed.hash) {
      return Promise.reject(new Error("必须是无路径、无结尾斜杠的 HTTPS Origin"));
    }
    return Promise.resolve();
  } catch {
    return Promise.reject(new Error("URL 格式不正确"));
  }
}

function validateCIDR(raw: unknown) {
  if (typeof raw !== "string") return Promise.reject(new Error("请输入 CIDR"));
  if (raw === "0.0.0.0/0" || raw === "::/0") return Promise.reject(new Error("禁止信任整个互联网，请填写反向代理的精确网段"));
  const [address, prefix, extra] = raw.split("/");
  if (!address || prefix === undefined || extra !== undefined || !/^\d+$/.test(prefix)) return Promise.reject(new Error("请输入 CIDR，例如 127.0.0.1/32"));
  const bits = Number(prefix);
  if (address.includes(":")) return bits >= 0 && bits <= 128 ? Promise.resolve() : Promise.reject(new Error("IPv6 前缀必须为 0–128"));
  const octets = address.split(".");
  const validIPv4 = octets.length === 4 && octets.every((octet) => /^\d+$/.test(octet) && Number(octet) <= 255);
  return validIPv4 && bits >= 0 && bits <= 32 ? Promise.resolve() : Promise.reject(new Error("IPv4 CIDR 格式不正确"));
}

function SecondsField({ name, label, min = 10, help }: { name: string | string[]; label: string; min?: number; help?: string }) {
  return (
    <Form.Item name={name} label={label} tooltip={help} rules={[{ required: true, message: `请输入${label}` }]}>
      <InputNumber min={min} precision={0} suffix="秒" className="full-width" />
    </Form.Item>
  );
}

export function SettingsPage() {
  const [form] = Form.useForm<SettingsFormValues>();
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [dirty, setDirty] = useState(false);
  const watched = Form.useWatch([], form) as SettingsFormValues | undefined;
  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.request<SettingsPayload>("/api/v1/admin/settings"),
  });

  useEffect(() => {
    if (settings.data) {
      form.setFieldsValue(fromPayload(settings.data));
      setDirty(false);
    }
  }, [form, settings.data]);

  useEffect(() => {
    if (!dirty) return;
    const preventClose = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", preventClose);
    return () => window.removeEventListener("beforeunload", preventClose);
  }, [dirty]);

  const save = useMutation({
    mutationFn: (values: SettingsFormValues) => api.request<void>("/api/v1/admin/settings", { method: "PATCH", body: JSON.stringify(toPayload(values)) }),
    onSuccess: (_, values) => {
      queryClient.setQueryData(["settings"], toPayload(values));
      setDirty(false);
      void message.success("系统配置已保存");
    },
    onError: (error) => void message.error(error instanceof Error ? error.message : "配置保存失败"),
  });

  const preview = useMemo(() => JSON.stringify(toPayload(mergeDefaults(watched)), null, 2), [watched]);
  const reset = () => {
    if (settings.data) form.setFieldsValue(fromPayload(settings.data));
    setDirty(false);
  };

  return (
    <section className="settings-page">
      <div className="enterprise-page-header">
        <div>
          <Space align="center"><SettingOutlined className="page-title-icon" /><Typography.Title level={2}>系统设置</Typography.Title></Space>
          <Typography.Paragraph type="secondary">管理公开地址、跨域信任、会话、分享、存储签名和安全限流。</Typography.Paragraph>
        </div>
        <Space>
          {dirty && <Typography.Text type="warning">有未保存的更改</Typography.Text>}
          <Button icon={<ReloadOutlined />} disabled={!dirty || save.isPending} onClick={reset}>重置</Button>
          <Button type="primary" icon={<SaveOutlined />} loading={save.isPending} disabled={!dirty} onClick={() => form.submit()}>保存配置</Button>
        </Space>
      </div>

      <Alert
        type="info"
        showIcon
        className="settings-notice"
        title="公开 URL 仅接受精确 HTTPS Origin"
        description="不要填写路径、结尾斜杠或通配符。CORS 修改立即生效；修改公开 Web/API URL 后请重启后端，以更新 OIDC 回调和安全跳转地址。"
      />

      {settings.error && <Alert type="error" showIcon title="配置加载失败" description={settings.error.message} className="settings-notice" />}
      <Spin spinning={settings.isLoading} description="正在加载系统配置">
        <Form<SettingsFormValues>
          form={form}
          layout="vertical"
          requiredMark="optional"
          onValuesChange={() => setDirty(true)}
          onFinish={(values) => save.mutate(values)}
        >
          <Row gutter={[16, 16]}>
            <Col xs={24} xl={12}>
              <Card title={<Space><GlobalOutlined />公开地址与 CORS</Space>} className="settings-card">
                <Form.Item name="publicWebUrl" label="公开 Web URL" extra="浏览器访问前端的唯一规范 Origin" rules={[{ validator: (_, fieldValue) => validateHTTPSOrigin(fieldValue) }]}>
                  <Input placeholder="https://drive.example.com" />
                </Form.Item>
                <Form.Item name="publicApiUrl" label="公开 API URL" extra="前端和 OIDC Provider 访问 API 的 Origin" rules={[{ validator: (_, fieldValue) => validateHTTPSOrigin(fieldValue) }]}>
                  <Input placeholder="https://drive-api.example.com" />
                </Form.Item>
                <Form.List
                  name="allowedOrigins"
                  rules={[{
                    validator: async (_, origins: string[] | undefined) => {
                      if (!origins?.length) throw new Error("至少保留一个允许的前端 Origin");
                      if (!origins.includes(form.getFieldValue("publicWebUrl"))) {
                        throw new Error("CORS Origins 必须包含公开 Web URL，否则前端将无法登录");
                      }
                    },
                  }]}
                >
                  {(fields, { add, remove }, { errors }) => (
                    <div>
                      <div className="form-list-heading"><Typography.Text strong>允许的 CORS Origins</Typography.Text><Button type="dashed" size="small" icon={<PlusOutlined />} onClick={() => add("")}>添加</Button></div>
                      {fields.map(({ key, ...field }) => (
                        <Space key={key} align="start" className="dynamic-form-row">
                          <Form.Item {...field} rules={[{ validator: (_, fieldValue) => validateHTTPSOrigin(fieldValue) }]}>
                            <Input prefix={<ApiOutlined />} placeholder="https://drive.example.com" />
                          </Form.Item>
                          <Tooltip title="删除 Origin"><Button danger size="small" icon={<DeleteOutlined />} aria-label="删除 Origin" onClick={() => remove(field.name)} /></Tooltip>
                        </Space>
                      ))}
                      <Form.ErrorList errors={errors} />
                    </div>
                  )}
                </Form.List>
              </Card>
            </Col>

            <Col xs={24} xl={12}>
              <Card title={<Space><SafetyCertificateOutlined />会话与可信代理</Space>} className="settings-card">
                <Row gutter={12}>
                  <Col xs={24} md={12}><SecondsField name="sessionIdleTimeout" label="会话空闲超时" min={300} /></Col>
                  <Col xs={24} md={12}><SecondsField name="sessionAbsoluteTimeout" label="会话绝对超时" min={3_600} /></Col>
                </Row>
                <Form.List name="trustedProxyCIDRs">
                  {(fields, { add, remove }) => (
                    <div>
                      <div className="form-list-heading"><div><Typography.Text strong>可信代理 CIDRs</Typography.Text><Typography.Text type="secondary" className="field-caption">仅填写直接连接后端的反向代理网络，不要使用 0.0.0.0/0。</Typography.Text></div><Button type="dashed" size="small" icon={<PlusOutlined />} onClick={() => add("")}>添加</Button></div>
                      {fields.map(({ key, ...field }) => (
                        <Space key={key} align="start" className="dynamic-form-row">
                          <Form.Item {...field} rules={[{ validator: (_, fieldValue) => validateCIDR(fieldValue) }]}>
                            <Input placeholder="127.0.0.1/32" />
                          </Form.Item>
                          <Tooltip title="删除 CIDR"><Button danger size="small" icon={<DeleteOutlined />} aria-label="删除 CIDR" onClick={() => remove(field.name)} /></Tooltip>
                        </Space>
                      ))}
                    </div>
                  )}
                </Form.List>
              </Card>
            </Col>

            <Col xs={24} xl={12}>
              <Card title="存储与分享有效期" className="settings-card">
                <Row gutter={12}>
                  <Col xs={24} md={12}><SecondsField name="uploadPresignTTL" label="上传预签名有效期" /></Col>
                  <Col xs={24} md={12}><SecondsField name="downloadPresignTTL" label="下载预签名有效期" /></Col>
                  <Col xs={24} md={12}><SecondsField name="shareDefaultExpiration" label="分享默认有效期" /></Col>
                  <Col xs={24} md={12}><SecondsField name="shareDownloadPresignTTL" label="分享下载签名有效期" /></Col>
                </Row>
              </Card>
            </Col>

            <Col xs={24} xl={12}>
              <Card title="登录限流" className="settings-card">
                <Row gutter={12}>
                  <Col xs={24} md={8}><Form.Item name={["loginRateLimit", "attempts"]} label="窗口内尝试次数" rules={[{ required: true }]}><InputNumber min={1} max={100} precision={0} className="full-width" /></Form.Item></Col>
                  <Col xs={24} md={8}><SecondsField name={["loginRateLimit", "window_seconds"]} label="统计窗口" /></Col>
                  <Col xs={24} md={8}><SecondsField name={["loginRateLimit", "cooldown_seconds"]} label="封禁时间" /></Col>
                </Row>
              </Card>
            </Col>

            <Col span={24}>
              <Card title="公开分享限流" className="settings-card">
                <Row gutter={12}>
                  <Col xs={24} sm={12} lg={6}><Form.Item name={["shareRateLimit", "ip_attempts"]} label="单 IP 尝试次数" rules={[{ required: true }]}><InputNumber min={1} precision={0} className="full-width" /></Form.Item></Col>
                  <Col xs={24} sm={12} lg={6}><SecondsField name={["shareRateLimit", "ip_window_seconds"]} label="单 IP 统计窗口" /></Col>
                  <Col xs={24} sm={12} lg={6}><SecondsField name={["shareRateLimit", "ip_ban_seconds"]} label="单 IP 封禁时间" /></Col>
                  <Col xs={24} sm={12} lg={6}>
                    <Form.Item
                      name={["shareRateLimit", "escalated_ban_seconds"]}
                      label="升级封禁时间"
                      dependencies={[["shareRateLimit", "ip_ban_seconds"]]}
                      rules={[
                        { required: true, message: "请输入升级封禁时间" },
                        {
                          validator: async (_, fieldValue: number | undefined) => {
                            const base = form.getFieldValue(["shareRateLimit", "ip_ban_seconds"]);
                            if (fieldValue !== undefined && typeof base === "number" && fieldValue < base) {
                              throw new Error("不得短于单 IP 封禁时间");
                            }
                          },
                        },
                      ]}
                    >
                      <InputNumber min={10} precision={0} suffix="秒" className="full-width" />
                    </Form.Item>
                  </Col>
                  <Col xs={24} sm={12} lg={8}><Form.Item name={["shareRateLimit", "share_attempts"]} label="单分享尝试次数" rules={[{ required: true }]}><InputNumber min={1} precision={0} className="full-width" /></Form.Item></Col>
                  <Col xs={24} sm={12} lg={8}><SecondsField name={["shareRateLimit", "share_window_seconds"]} label="单分享统计窗口" /></Col>
                  <Col xs={24} sm={12} lg={8}><SecondsField name={["shareRateLimit", "share_ban_seconds"]} label="单分享封禁时间" /></Col>
                </Row>
              </Card>
            </Col>
          </Row>
        </Form>
      </Spin>

      <Collapse
        className="settings-preview"
        items={[{
          key: "json",
          label: "高级：查看将提交的 JSON",
          children: (
            <div className="config-preview-shell">
              <div className="config-preview-toolbar">
                <Typography.Text type="secondary">只读预览</Typography.Text>
                <Typography.Text copyable={{ text: preview }}>复制 JSON</Typography.Text>
              </div>
              <pre className="config-preview"><code>{preview}</code></pre>
            </div>
          ),
        }]}
      />
    </section>
  );
}
