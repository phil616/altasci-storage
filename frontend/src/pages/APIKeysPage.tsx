import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, App, Button, Card, Checkbox, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, Typography } from "antd";
import { api } from "../api/client";
import type { Project } from "../api/types";
import { useAuth } from "../auth/AuthProvider";

type APIKey = {
  id: string; name: string; token_prefix: string; scopes: string[]; all_projects: boolean;
  project_ids: string[]; created_at: string; expires_at: string; last_used_at: string | null; revoked_at: string | null;
};
type Policy = { name: string; scopes: string[]; all_projects: boolean; project_ids: string[]; expires_at: string };
const scopeOptions = [
  { value: "projects:read", label: "查看项目" }, { value: "projects:create", label: "创建项目" },
  { value: "projects:write", label: "修改项目（管理员）" }, { value: "projects:delete", label: "删除项目（管理员）" },
  { value: "files:read", label: "浏览和下载文件" }, { value: "files:write", label: "上传、覆盖、重命名和移动文件 / 创建目录" },
  { value: "files:delete", label: "删除文件和目录" },
];
function localTime(iso: string) {
  const date = new Date(iso);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}

export function APIKeysPage() {
  const { user } = useAuth();
  const { message } = App.useApp();
  const cache = useQueryClient();
  const [form] = Form.useForm<Policy>();
  const [editing, setEditing] = useState<APIKey | "new" | null>(null);
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const allProjects = Form.useWatch("all_projects", form);
  const keys = useQuery({ queryKey: ["api-keys"], queryFn: () => api.request<{ items: APIKey[] }>("/api/v1/api-keys") });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.request<{ items: Project[] }>("/api/v1/projects") });
  const options = scopeOptions.filter(({ value }) => {
    if (user?.role === "admin") return true;
    if (value === "projects:write" || value === "projects:delete") return false;
    return user?.write_enabled || value.endsWith(":read");
  });
  function open(key: APIKey | "new") {
    form.resetFields();
    form.setFieldsValue(key === "new" ? {
      name: "", scopes: ["projects:read", "files:read"], all_projects: false, project_ids: [],
      expires_at: localTime(new Date(Date.now() + 30 * 86400000).toISOString()),
    } : { ...key, expires_at: localTime(key.expires_at) });
    setEditing(key);
  }
  async function save(values: Policy) {
    if (!editing) return;
    setBusy(true);
    try {
      const body = { ...values, project_ids: values.all_projects ? [] : values.project_ids, expires_at: new Date(values.expires_at).toISOString() };
      const result = await api.request<APIKey & { token?: string }>(editing === "new" ? "/api/v1/api-keys" : `/api/v1/api-keys/${editing.id}`, {
        method: editing === "new" ? "POST" : "PUT", body: JSON.stringify(body),
      });
      setEditing(null);
      if (result.token) setSecret(result.token);
      void cache.invalidateQueries({ queryKey: ["api-keys"] });
      message.success("API 密钥已保存");
    } catch (error) { message.error(error instanceof Error ? error.message : "保存失败"); }
    finally { setBusy(false); }
  }
  async function revoke(key: APIKey) {
    try {
      await api.request(`/api/v1/api-keys/${key.id}`, { method: "DELETE" });
      void cache.invalidateQueries({ queryKey: ["api-keys"] });
      message.success("API 密钥已撤销");
    } catch (error) { message.error(error instanceof Error ? error.message : "撤销失败"); }
  }
  return <Space orientation="vertical" size="large" style={{ width: "100%" }}>
    <div><Typography.Title level={2}>API 密钥</Typography.Title>
      <Typography.Paragraph type="secondary">为自动化程序创建独立密钥。实际权限受您的当前账户权限和密钥授权范围共同限制。</Typography.Paragraph></div>
    {keys.error && <Alert type="error" title={keys.error.message} action={<Button onClick={() => void keys.refetch()}>重试</Button>} />}
    <Card title="我的密钥" extra={<Button type="primary" onClick={() => open("new")}>创建 API 密钥</Button>}>
      <Table<APIKey> rowKey="id" loading={keys.isLoading} dataSource={keys.data?.items ?? []} scroll={{ x: 900 }} columns={[
        { title: "名称", dataIndex: "name", render: (name, key) => <><Typography.Text strong>{name}</Typography.Text><br /><Typography.Text type="secondary">{key.token_prefix}…</Typography.Text></> },
        { title: "权限", render: (_, key) => <Space wrap>{key.scopes.map(scope => <Tag key={scope}>{scopeOptions.find(o => o.value === scope)?.label ?? scope}</Tag>)}</Space> },
        { title: "项目范围", render: (_, key) => key.all_projects ? "所有当前及未来有权访问的项目" : key.project_ids.map(id => projects.data?.items.find(p => p.id === id)?.name ?? id).join("、") },
        { title: "状态 / 有效期", render: (_, key) => <><Tag color={key.revoked_at || Date.parse(key.expires_at) <= Date.now() ? "default" : "green"}>{key.revoked_at ? "已撤销" : Date.parse(key.expires_at) <= Date.now() ? "已过期" : "有效"}</Tag><br />{new Date(key.expires_at).toLocaleString()}</> },
        { title: "最近使用", render: (_, key) => key.last_used_at ? new Date(key.last_used_at).toLocaleString() : "尚未使用" },
        { title: "操作", render: (_, key) => !key.revoked_at && <Space><Button onClick={() => open(key)}>编辑权限</Button><Popconfirm title="撤销后无法恢复，确认撤销？" onConfirm={() => revoke(key)}><Button danger>撤销</Button></Popconfirm></Space> },
      ]} />
    </Card>
    <Modal title={editing === "new" ? "创建 API 密钥" : "编辑 API 权限"} open={editing !== null} onCancel={() => { if (!busy) setEditing(null); }} onOk={() => form.submit()} confirmLoading={busy} destroyOnHidden>
      <Form form={form} layout="vertical" onFinish={save}>
        <Form.Item name="name" label="名称" rules={[{ required: true, whitespace: true, max: 100 }]}><Input maxLength={100} placeholder="例如：每日备份" /></Form.Item>
        <Form.Item name="scopes" label="允许的操作" rules={[{ required: true, type: "array", min: 1 }]}><Select mode="multiple" options={options} /></Form.Item>
        <Form.Item name="all_projects" valuePropName="checked"><Checkbox>所有当前及未来有权访问的项目</Checkbox></Form.Item>
        {!allProjects && <Form.Item name="project_ids" label="指定项目" rules={[{ required: true, type: "array", min: 1 }]}><Select mode="multiple" loading={projects.isLoading} options={projects.data?.items.map(p => ({ value: p.id, label: p.name }))} /></Form.Item>}
        {projects.error && <Alert type="error" title="项目列表加载失败" action={<Button onClick={() => void projects.refetch()}>重试</Button>} />}
        <Form.Item name="expires_at" label="有效期（最长 366 天）" rules={[{ required: true }, { validator: async (_, value: string) => {
          const expiry = new Date(value).getTime();
          if (!Number.isFinite(expiry) || expiry <= Date.now() || expiry > Date.now() + 366 * 86400000) throw new Error("请选择未来 366 天内的时间");
        } }]}><Input type="datetime-local" /></Form.Item>
        <Typography.Paragraph type="secondary">创建项目需要选择所有项目。写入权限包含覆盖文件。用户权限被收回后，密钥的对应操作也会被拒绝。</Typography.Paragraph>
      </Form>
    </Modal>
    <Modal title="请保存 API 密钥" open={!!secret} onCancel={() => setSecret("")} footer={<Button type="primary" onClick={() => setSecret("")}>我已保存</Button>}>
      <Alert type="warning" title="完整密钥只显示这一次，关闭后无法再次查看。" />
      <Typography.Paragraph copyable={{ text: secret }} style={{ marginTop: 16, overflowWrap: "anywhere" }}>{secret}</Typography.Paragraph>
      <Typography.Paragraph>请求头：<Typography.Text code>Authorization: Bearer &lt;密钥&gt;</Typography.Text></Typography.Paragraph>
    </Modal>
  </Space>;
}
