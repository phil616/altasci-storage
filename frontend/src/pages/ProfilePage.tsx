import { KeyOutlined, SafetyCertificateOutlined, UserOutlined } from "@ant-design/icons";
import { Alert, App, Avatar, Button, Card, Descriptions, Form, Input, Space, Tag, Typography } from "antd";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthProvider";

type PasswordForm = { currentPassword: string; newPassword: string; confirmPassword: string };

export function ProfilePage() {
  const { user } = useAuth();
  const { message } = App.useApp();
  const [form] = Form.useForm<PasswordForm>();

  async function submit(values: PasswordForm) {
    try {
      await api.request("/api/v1/auth/change-password", { method: "POST", body: JSON.stringify({ current_password: values.currentPassword, new_password: values.newPassword }) });
      form.resetFields();
      await message.success("密码已更新，即将返回登录页");
      window.location.assign("/login");
    } catch (error) {
      void message.error(error instanceof Error ? error.message : "密码更新失败");
    }
  }

  return (
    <section>
      <div className="enterprise-page-header"><div><Typography.Title level={2}>个人设置</Typography.Title><Typography.Paragraph type="secondary">查看账户状态并维护本地登录密码。</Typography.Paragraph></div></div>
      <div className="profile-grid">
        <Card title="账户信息">
          <Space size={16} className="profile-identity"><Avatar size={56} icon={<UserOutlined />} /><div><Typography.Title level={4}>{user?.email}</Typography.Title><Space><Tag icon={user?.role === "admin" ? <SafetyCertificateOutlined /> : <UserOutlined />} color={user?.role === "admin" ? "purple" : "default"}>{user?.role === "admin" ? "管理员" : "普通用户"}</Tag><Tag color={user?.status === "active" ? "success" : "error"}>{user?.status === "active" ? "正常" : "已禁用"}</Tag></Space></div></Space>
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label="用户 ID"><Typography.Text copyable code>{user?.id}</Typography.Text></Descriptions.Item>
            <Descriptions.Item label="全局写权限">{user?.write_enabled ? "已启用" : "只读"}</Descriptions.Item>
            <Descriptions.Item label="创建时间">{user?.created_at ? new Date(user.created_at).toLocaleString() : "—"}</Descriptions.Item>
          </Descriptions>
        </Card>
        <Card title={<Space><KeyOutlined />修改密码</Space>}>
          <Alert type="info" showIcon title="密码修改成功后，所有现有会话都会被撤销。" className="modal-notice" />
          <Form<PasswordForm> form={form} layout="vertical" onFinish={(values) => void submit(values)}>
            <Form.Item name="currentPassword" label="当前密码" rules={[{ required: true, message: "请输入当前密码" }]}><Input.Password autoComplete="current-password" /></Form.Item>
            <Form.Item name="newPassword" label="新密码" rules={[{ required: true }, { min: 12, max: 128, message: "密码长度必须为 12–128 个字符" }]}><Input.Password autoComplete="new-password" /></Form.Item>
            <Form.Item name="confirmPassword" label="确认新密码" dependencies={["newPassword"]} rules={[{ required: true }, ({ getFieldValue }) => ({ validator: async (_, fieldValue) => { if (fieldValue !== getFieldValue("newPassword")) throw new Error("两次输入的密码不一致"); } })]}><Input.Password autoComplete="new-password" /></Form.Item>
            <Button type="primary" htmlType="submit">更新密码</Button>
          </Form>
        </Card>
      </div>
    </section>
  );
}
