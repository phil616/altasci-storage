import {
  CloudServerOutlined,
  DeleteOutlined,
  EditOutlined,
  ExperimentOutlined,
  InfoCircleOutlined,
  MoreOutlined,
  PlusOutlined,
} from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  App,
  Button,
  Descriptions,
  Dropdown,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
  type MenuProps,
  type TableColumnsType,
} from "antd";
import { useState } from "react";
import { api, APIError } from "../../api/client";

type Provider = {
  id: string;
  name: string;
  issuer: string;
  client_id: string;
  scopes: string;
  enabled: boolean;
  auto_create_user: boolean;
  auto_link_verified_email: boolean;
  allowed_email_domains: string[];
  has_client_secret: boolean;
};

type ProviderForm = {
  name: string;
  issuer: string;
  clientID: string;
  clientSecret?: string;
  scopes: string;
  enabled: boolean;
  autoCreateUser: boolean;
  autoLinkVerifiedEmail: boolean;
  allowedEmailDomains: string[];
};

const newProviderValues: ProviderForm = {
  name: "",
  issuer: "",
  clientID: "",
  clientSecret: "",
  scopes: "openid email profile",
  enabled: false,
  autoCreateUser: false,
  autoLinkVerifiedEmail: false,
  allowedEmailDomains: [],
};

function toFormValues(provider: Provider): ProviderForm {
  return {
    name: provider.name,
    issuer: provider.issuer,
    clientID: provider.client_id,
    clientSecret: "",
    scopes: provider.scopes,
    enabled: provider.enabled,
    autoCreateUser: provider.auto_create_user,
    autoLinkVerifiedEmail: provider.auto_link_verified_email,
    allowedEmailDomains: provider.allowed_email_domains ?? [],
  };
}

function payload(values: ProviderForm) {
  return {
    name: values.name.trim(),
    issuer: values.issuer.trim().replace(/\/$/, ""),
    client_id: values.clientID.trim(),
    client_secret: values.clientSecret?.trim() ?? "",
    scopes: values.scopes.trim(),
    enabled: values.enabled,
    auto_create_user: values.autoCreateUser,
    auto_link_verified_email: values.autoLinkVerifiedEmail,
    allowed_email_domains: values.allowedEmailDomains ?? [],
  };
}

function mutationMessage(error: unknown, fallback: string) {
  if (!(error instanceof APIError)) return error instanceof Error ? error.message : fallback;
  if (error.code === "OIDC_PROVIDER_ENABLED") return "请先停用 Provider，再执行删除。";
  if (error.code === "CONFLICT") return "配置与现有数据冲突，请刷新列表后重试。";
  return error.message || fallback;
}

export function OIDCPage() {
  const [form] = Form.useForm<ProviderForm>();
  const [editor, setEditor] = useState<"create" | Provider>();
  const [details, setDetails] = useState<Provider>();
  const { message, modal } = App.useApp();
  const queryClient = useQueryClient();
  const providers = useQuery({ queryKey: ["oidc"], queryFn: () => api.request<{ items: Provider[] }>("/api/v1/admin/oidc-providers") });
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["oidc"] });

  const create = useMutation({
    mutationFn: (values: ProviderForm) => api.request("/api/v1/admin/oidc-providers", { method: "POST", body: JSON.stringify(payload(values)) }),
    onSuccess: () => { closeEditor(); void refresh(); void message.success("OIDC Provider 已创建"); },
    onError: (error) => void message.error(mutationMessage(error, "创建失败")),
  });
  const update = useMutation({
    mutationFn: ({ id, values }: { id: string; values: ProviderForm }) => api.request(`/api/v1/admin/oidc-providers/${id}`, { method: "PATCH", body: JSON.stringify(payload(values)) }),
    onSuccess: () => { closeEditor(); void refresh(); void message.success("OIDC Provider 已更新"); },
    onError: (error) => void message.error(mutationMessage(error, "更新失败")),
  });
  const test = useMutation({
    mutationFn: (id: string) => api.request(`/api/v1/admin/oidc-providers/${id}/test`, { method: "POST", body: "{}" }),
    onSuccess: () => void message.success("Discovery、Issuer 和端点校验通过"),
    onError: (error) => void message.error(mutationMessage(error, "Discovery 测试失败")),
  });
  const toggle = useMutation({
    mutationFn: (provider: Provider) => api.request(`/api/v1/admin/oidc-providers/${provider.id}`, { method: "PATCH", body: JSON.stringify(payload({ ...toFormValues(provider), enabled: !provider.enabled })) }),
    onSuccess: () => void refresh(),
    onError: (error) => void message.error(mutationMessage(error, "状态更新失败")),
  });
  const remove = useMutation({
    mutationFn: (id: string) => api.request(`/api/v1/admin/oidc-providers/${id}`, { method: "DELETE" }),
    onSuccess: () => { void refresh(); void message.success("OIDC Provider 已删除"); },
    onError: (error) => void message.error(mutationMessage(error, "删除失败")),
  });

  function closeEditor() {
    setEditor(undefined);
    form.resetFields();
  }

  function toggleProvider(provider: Provider) {
    if (!provider.enabled && !provider.client_id) {
      void message.warning("请先编辑 Provider 并填写 Client ID，再启用登录。");
      return;
    }
    toggle.mutate(provider);
  }

  function confirmDelete(provider: Provider) {
    modal.confirm({
      title: `删除 ${provider.name}？`,
      content: "删除会移除该 Provider 的 OIDC 身份绑定和未完成登录流程，但不会删除用户账户。被解绑用户需要使用本地密码或其他身份提供方登录。",
      okText: "确认删除",
      cancelText: "取消",
      okButtonProps: { danger: true },
      onOk: () => remove.mutateAsync(provider.id),
    });
  }

  const columns: TableColumnsType<Provider> = [
    { title: "Provider", dataIndex: "name", render: (name: string, provider) => <Space><CloudServerOutlined /><div><Typography.Text strong className="table-primary-text" title={name}>{name}</Typography.Text><Typography.Text className="table-subtitle" type="secondary">{provider.issuer}</Typography.Text></div></Space> },
    { title: "Client ID", dataIndex: "client_id", ellipsis: true, responsive: ["lg"], render: (value: string) => value || <Typography.Text type="secondary">待配置</Typography.Text> },
    { title: "Secret", dataIndex: "has_client_secret", width: 90, responsive: ["md"], render: (saved: boolean) => saved ? <Tag color="success">已保存</Tag> : <Tag>无</Tag> },
    { title: "自动开户", dataIndex: "auto_create_user", width: 100, responsive: ["md"], render: (enabled: boolean) => enabled ? <Tag color="processing">启用</Tag> : "关闭" },
    { title: "启用", dataIndex: "enabled", width: 80, responsive: ["sm"], render: (enabled: boolean, provider) => <Switch checked={enabled} loading={toggle.isPending && toggle.variables?.id === provider.id} onChange={() => toggleProvider(provider)} /> },
    {
      title: "操作",
      width: 116,
      render: (_, provider) => {
        const items: MenuProps["items"] = [
          { key: "details", icon: <InfoCircleOutlined />, label: "接入信息" },
          { key: "edit", icon: <EditOutlined />, label: "编辑配置" },
          { key: "test", icon: <ExperimentOutlined />, label: "测试 Discovery" },
          { type: "divider" },
          { key: "delete", icon: <DeleteOutlined />, label: provider.enabled ? "请先停用后删除" : "删除 Provider", danger: true, disabled: provider.enabled },
        ];
        return (
          <Dropdown
            trigger={["click"]}
            menu={{
              items,
              onClick: ({ key }) => {
                if (key === "details") setDetails(provider);
                else if (key === "edit") setEditor(provider);
                else if (key === "test") test.mutate(provider.id);
                else if (key === "delete") confirmDelete(provider);
              },
            }}
          >
            <Button icon={<MoreOutlined />} loading={test.isPending && test.variables === provider.id}>操作</Button>
          </Dropdown>
        );
      },
    },
  ];

  const editingProvider = editor && editor !== "create" ? editor : undefined;
  const callbackURL = details ? `${api.baseUrl}/api/v1/auth/oidc/${encodeURIComponent(details.id)}/callback` : "";
  const startURL = details ? `${api.baseUrl}/api/v1/auth/oidc/${encodeURIComponent(details.id)}/start` : "";

  return (
    <section>
      <div className="enterprise-page-header">
        <div><Typography.Title level={2}>OpenID Connect</Typography.Title><Typography.Paragraph type="secondary">通过标准 Discovery、Authorization Code 与 PKCE S256 接入企业身份提供方。</Typography.Paragraph></div>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setEditor("create")}>添加 Provider</Button>
      </div>
      <Alert
        type="info"
        showIcon
        title="推荐使用两阶段配置"
        description="先创建一个停用的 Provider 草稿，再从“操作 → 接入信息”复制回调地址到 IdP；随后编辑 Client ID 和 Client Secret，测试 Discovery，最后启用。"
        className="settings-notice"
      />
      {providers.error && <Alert type="error" showIcon title="OIDC Provider 加载失败" description={providers.error.message} className="settings-notice" />}
      <Table<Provider> rowKey="id" loading={providers.isLoading} columns={columns} dataSource={providers.data?.items ?? []} pagination={false} />

      <Modal
        title={editingProvider ? `编辑 ${editingProvider.name}` : "添加 OIDC Provider"}
        open={Boolean(editor)}
        width={720}
        destroyOnHidden
        confirmLoading={create.isPending || update.isPending}
        okText={editingProvider ? "保存修改" : "创建草稿"}
        cancelText="取消"
        onCancel={closeEditor}
        onOk={() => form.submit()}
        afterOpenChange={(visible) => {
          if (visible) form.setFieldsValue(editingProvider ? toFormValues(editingProvider) : newProviderValues);
        }}
      >
        <Alert
          type="info"
          showIcon
          title={editingProvider ? "留空 Client Secret 将保留现有密钥。" : "系统只接受标准 OIDC Discovery。新 Provider 默认停用，创建后会生成专属回调地址。"}
          className="modal-notice"
        />
        <Form<ProviderForm>
          form={form}
          layout="vertical"
          initialValues={newProviderValues}
          onFinish={(values) => editingProvider ? update.mutate({ id: editingProvider.id, values }) : create.mutate(values)}
        >
          <Form.Item name="name" label="显示名称" extra="显示在登录页面上的企业身份登录按钮。" rules={[{ required: true, whitespace: true, message: "请输入名称" }]}><Input placeholder="公司统一身份认证" /></Form.Item>
          <Form.Item name="issuer" label="Issuer" extra="必须与 IdP 的 iss 声明完全一致；系统读取 {issuer}/.well-known/openid-configuration。" rules={[{ required: true }, { type: "url", message: "请输入有效 URL" }, { validator: async (_, value) => { if (value && !value.startsWith("https://")) throw new Error("Issuer 必须使用 HTTPS"); } }]}><Input placeholder="https://id.example.com" /></Form.Item>
          <Form.Item
            name="clientID"
            label="Client ID"
            extra="创建停用草稿时可以留空；启用 Provider 前必须填写。"
            dependencies={["enabled"]}
            rules={[{ validator: async (_, value: string) => { if (form.getFieldValue("enabled") && !value?.trim()) throw new Error("启用 Provider 前必须填写 Client ID"); } }]}
          ><Input autoComplete="off" /></Form.Item>
          <Form.Item name="clientSecret" label="Client Secret" extra={editingProvider?.has_client_secret ? "已保存密钥。留空表示保留；输入新值将覆盖旧密钥。" : "机密客户端通常需要填写；密钥加密保存且不会回显。"}><Input.Password autoComplete="new-password" /></Form.Item>
          <Form.Item name="scopes" label="Scopes" extra="必须包含 openid；需要邮箱关联或自动开户时还应包含 email。" rules={[{ required: true }, { validator: async (_, value: string) => { if (!value?.split(/\s+/).includes("openid")) throw new Error("Scopes 必须包含 openid"); } }]}><Input placeholder="openid email profile" /></Form.Item>
          <Form.Item name="allowedEmailDomains" label="允许自动开户的邮箱域" extra="输入域名后按 Enter；留空表示不按邮箱域限制。"><Select mode="tags" tokenSeparators={[","]} placeholder="example.com" /></Form.Item>
          <Space size="large" wrap align="start">
            <Form.Item name="enabled" valuePropName="checked" label="启用 Provider"><Switch /></Form.Item>
            <Form.Item name="autoLinkVerifiedEmail" valuePropName="checked" label="按已验证邮箱关联用户"><Switch /></Form.Item>
            <Form.Item name="autoCreateUser" valuePropName="checked" label="自动创建只读用户"><Switch /></Form.Item>
          </Space>
        </Form>
      </Modal>

      <Modal title={`${details?.name ?? "OIDC Provider"} · IdP 接入信息`} open={Boolean(details)} width={760} footer={<Button type="primary" onClick={() => setDetails(undefined)}>完成</Button>} onCancel={() => setDetails(undefined)}>
        <Alert type="warning" showIcon title="回调地址必须在 IdP 中精确注册" description="请选择 Web / Confidential Application，禁止使用通配符回调地址。公网 IdP 必须能够通过 HTTPS 访问该 API 地址。" className="modal-notice" />
        {details && (
          <Descriptions bordered column={1} size="small" className="oidc-integration-details">
            <Descriptions.Item label="应用类型">Web / Confidential Application</Descriptions.Item>
            <Descriptions.Item label="Redirect URI / Callback URL"><Typography.Text code copyable={{ text: callbackURL }}>{callbackURL}</Typography.Text></Descriptions.Item>
            <Descriptions.Item label="登录入口"><Typography.Text code copyable={{ text: startURL }}>{startURL}</Typography.Text></Descriptions.Item>
            <Descriptions.Item label="Issuer"><Typography.Text code copyable={{ text: details.issuer }}>{details.issuer}</Typography.Text></Descriptions.Item>
            <Descriptions.Item label="Discovery URL"><Typography.Text code copyable={{ text: `${details.issuer}/.well-known/openid-configuration` }}>{details.issuer}/.well-known/openid-configuration</Typography.Text></Descriptions.Item>
            <Descriptions.Item label="Client ID"><Typography.Text code copyable>{details.client_id || "尚未配置"}</Typography.Text></Descriptions.Item>
            <Descriptions.Item label="授权流程">Authorization Code Flow</Descriptions.Item>
            <Descriptions.Item label="Response Type"><Typography.Text code>code</Typography.Text></Descriptions.Item>
            <Descriptions.Item label="PKCE">S256（由后端生成 verifier 和 challenge）</Descriptions.Item>
            <Descriptions.Item label="Scopes"><Typography.Text code copyable>{details.scopes}</Typography.Text></Descriptions.Item>
            <Descriptions.Item label="Claims">必须返回 sub；邮箱关联或自动开户还需要 email 和 email_verified。</Descriptions.Item>
          </Descriptions>
        )}
      </Modal>
    </section>
  );
}
