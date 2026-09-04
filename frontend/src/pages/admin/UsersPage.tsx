import { CheckCircleOutlined, KeyOutlined, LogoutOutlined, MoreOutlined, PlusOutlined, SafetyCertificateOutlined, StopOutlined, UserOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, App, Button, Dropdown, Form, Input, Modal, Space, Switch, Table, Tag, Typography, type MenuProps, type TableColumnsType } from "antd";
import { useState } from "react";
import { api } from "../../api/client";
import type { User } from "../../api/types";

type CreateUserForm = { email: string; password: string; confirmPassword: string; writeEnabled: boolean };
type PasswordForm = { password: string; confirmPassword: string };

export function UsersPage() {
  const [createForm] = Form.useForm<CreateUserForm>();
  const [passwordForm] = Form.useForm<PasswordForm>();
  const [transferForm] = Form.useForm<{ currentPassword: string }>();
  const [createOpen, setCreateOpen] = useState(false);
  const [resetTarget, setResetTarget] = useState<User>();
  const [transferTarget, setTransferTarget] = useState<User>();
  const { message, modal } = App.useApp();
  const queryClient = useQueryClient();
  const users = useQuery({ queryKey: ["admin-users"], queryFn: () => api.request<{ items: User[] }>("/api/v1/admin/users") });
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["admin-users"] });
  const patch = useMutation({
    mutationFn: ({ id, body }: { id: string; body: object }) => api.request(`/api/v1/admin/users/${id}`, { method: "PATCH", body: JSON.stringify(body) }),
    onSuccess: () => void refresh(),
    onError: (error) => void message.error(error instanceof Error ? error.message : "用户更新失败"),
  });
  const create = useMutation({
    mutationFn: (values: CreateUserForm) => api.request("/api/v1/admin/users", { method: "POST", body: JSON.stringify({ email: values.email.trim(), password: values.password, write_enabled: values.writeEnabled }) }),
    onSuccess: () => { setCreateOpen(false); createForm.resetFields(); void refresh(); void message.success("用户已创建"); },
    onError: (error) => void message.error(error instanceof Error ? error.message : "用户创建失败"),
  });
  const action = useMutation({
    mutationFn: ({ id, action: command, body = {} }: { id: string; action: string; body?: object }) => api.request(`/api/v1/admin/users/${id}/${command}`, { method: "POST", body: JSON.stringify(body) }),
    onSuccess: (_, variables) => { void refresh(); void message.success(variables.action === "reset-password" ? "密码已重置，原会话已失效" : "用户会话已撤销"); },
    onError: (error) => void message.error(error instanceof Error ? error.message : "操作失败"),
  });
  const transfer = useMutation({
    mutationFn: ({ id, password }: { id: string; password: string }) => api.request("/api/v1/admin/transfer", { method: "POST", body: JSON.stringify({ new_admin_id: id, current_password: password }) }),
    onSuccess: () => window.location.assign("/login"),
    onError: (error) => void message.error(error instanceof Error ? error.message : "管理员转移失败"),
  });

  const columns: TableColumnsType<User> = [
    { title: "用户", dataIndex: "email", render: (email: string, user) => <Space><UserOutlined /><div><Typography.Text strong>{email}</Typography.Text><Typography.Text type="secondary" className="table-subtitle">{user.id}</Typography.Text></div></Space> },
    { title: "角色", dataIndex: "role", width: 100, responsive: ["md"], render: (role: User["role"]) => role === "admin" ? <Tag color="purple" icon={<SafetyCertificateOutlined />}>管理员</Tag> : <Tag>普通用户</Tag> },
    { title: "全局写权限", dataIndex: "write_enabled", width: 120, responsive: ["lg"], render: (enabled: boolean, user) => <Switch checked={enabled} disabled={user.role === "admin"} loading={patch.isPending && patch.variables?.id === user.id} onChange={(checked) => patch.mutate({ id: user.id, body: { write_enabled: checked } })} /> },
    { title: "状态", dataIndex: "status", width: 100, responsive: ["sm"], render: (status: User["status"]) => status === "active" ? <Tag color="success">正常</Tag> : <Tag color="error">已禁用</Tag> },
    {
      title: "操作",
      width: 116,
      render: (_, user) => {
        const items: MenuProps["items"] = [
          ...(user.role !== "admin" ? [{ key: "status", icon: user.status === "active" ? <StopOutlined /> : <CheckCircleOutlined />, label: user.status === "active" ? "禁用用户" : "启用用户", danger: user.status === "active" }] : []),
          { key: "sessions", icon: <LogoutOutlined />, label: "强制下线" },
          { key: "password", icon: <KeyOutlined />, label: "重置密码" },
          ...(user.role !== "admin" ? [{ type: "divider" as const }, { key: "transfer", icon: <SafetyCertificateOutlined />, label: "转移管理员" }] : []),
        ];
        return (
          <Dropdown
            trigger={["click"]}
            menu={{
              items,
              onClick: ({ key }) => {
                if (key === "status") {
                  const disabling = user.status === "active";
                  modal.confirm({
                    title: disabling ? "禁用用户？" : "启用用户？",
                    content: disabling ? "禁用后将同时撤销该用户的全部会话。" : "启用后该用户可以重新登录。",
                    okText: disabling ? "禁用" : "启用",
                    cancelText: "取消",
                    okButtonProps: { danger: disabling },
                    onOk: () => patch.mutateAsync({ id: user.id, body: { status: disabling ? "disabled" : "active" } }),
                  });
                } else if (key === "sessions") {
                  modal.confirm({
                    title: "强制用户下线？",
                    content: `将撤销 ${user.email} 的全部现有会话。`,
                    okText: "强制下线",
                    cancelText: "取消",
                    onOk: () => action.mutateAsync({ id: user.id, action: "revoke-sessions" }),
                  });
                } else if (key === "password") setResetTarget(user);
                else if (key === "transfer") setTransferTarget(user);
              },
            }}
          >
            <Button icon={<MoreOutlined />}>操作</Button>
          </Dropdown>
        );
      },
    },
  ];

  return (
    <section>
      <div className="enterprise-page-header"><div><Typography.Title level={2}>用户管理</Typography.Title><Typography.Paragraph type="secondary">普通用户需要同时具备全局写权限和项目 Writer 权限才能修改文件。</Typography.Paragraph></div><Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>创建用户</Button></div>
      {users.error && <Alert type="error" showIcon title="用户列表加载失败" description={users.error.message} className="settings-notice" />}
      <Table<User> rowKey="id" loading={users.isLoading} columns={columns} dataSource={users.data?.items ?? []} pagination={{ pageSize: 20, showSizeChanger: true }} />

      <Modal title="创建用户" open={createOpen} okText="创建" cancelText="取消" confirmLoading={create.isPending} destroyOnHidden onCancel={() => setCreateOpen(false)} onOk={() => createForm.submit()}>
        <Form<CreateUserForm> form={createForm} layout="vertical" initialValues={{ writeEnabled: false }} onFinish={(values) => create.mutate(values)}>
          <Form.Item name="email" label="邮箱" rules={[{ required: true }, { type: "email", message: "邮箱格式不正确" }]}><Input autoComplete="off" /></Form.Item>
          <Form.Item name="password" label="初始密码" rules={[{ required: true }, { min: 12, max: 128, message: "密码长度必须为 12–128 个字符" }]}><Input.Password autoComplete="new-password" /></Form.Item>
          <Form.Item name="confirmPassword" label="确认密码" dependencies={["password"]} rules={[{ required: true }, ({ getFieldValue }) => ({ validator: async (_, fieldValue) => { if (fieldValue !== getFieldValue("password")) throw new Error("两次输入的密码不一致"); } })]}><Input.Password autoComplete="new-password" /></Form.Item>
          <Form.Item name="writeEnabled" label="全局写权限" valuePropName="checked"><Switch checkedChildren="允许" unCheckedChildren="只读" /></Form.Item>
        </Form>
      </Modal>

      <Modal title={`重置 ${resetTarget?.email ?? "用户"} 的密码`} open={Boolean(resetTarget)} okText="重置密码" cancelText="取消" confirmLoading={action.isPending} destroyOnHidden onCancel={() => setResetTarget(undefined)} onOk={() => passwordForm.submit()}>
        <Alert type="warning" showIcon title="保存后该用户的所有现有会话都会失效。" className="modal-notice" />
        <Form<PasswordForm> form={passwordForm} layout="vertical" onFinish={(values) => { if (!resetTarget) return; action.mutate({ id: resetTarget.id, action: "reset-password", body: { password: values.password } }, { onSuccess: () => { setResetTarget(undefined); passwordForm.resetFields(); } }); }}>
          <Form.Item name="password" label="新密码" rules={[{ required: true }, { min: 12, max: 128, message: "密码长度必须为 12–128 个字符" }]}><Input.Password autoComplete="new-password" /></Form.Item>
          <Form.Item name="confirmPassword" label="确认新密码" dependencies={["password"]} rules={[{ required: true }, ({ getFieldValue }) => ({ validator: async (_, fieldValue) => { if (fieldValue !== getFieldValue("password")) throw new Error("两次输入的密码不一致"); } })]}><Input.Password autoComplete="new-password" /></Form.Item>
        </Form>
      </Modal>

      <Modal title="转移唯一管理员" open={Boolean(transferTarget)} okText="确认转移" cancelText="取消" okButtonProps={{ danger: true }} confirmLoading={transfer.isPending} destroyOnHidden onCancel={() => setTransferTarget(undefined)} onOk={() => transferForm.submit()}>
        <Alert type="error" showIcon title={`管理员身份将转移给 ${transferTarget?.email ?? "目标用户"}`} description="当前管理员将降级为普通用户，双方所有会话都会失效。该操作需要验证当前管理员密码。" className="modal-notice" />
        <Form form={transferForm} layout="vertical" onFinish={(values) => { if (transferTarget) transfer.mutate({ id: transferTarget.id, password: values.currentPassword }); }}><Form.Item name="currentPassword" label="当前管理员密码" rules={[{ required: true }]}><Input.Password autoComplete="current-password" /></Form.Item></Form>
      </Modal>
    </section>
  );
}
