import {
  App,
  Avatar,
  Button,
  Dropdown,
  Layout,
  Menu,
  Select,
  Space,
  Spin,
  Typography,
  type MenuProps,
} from "antd";
import {
  CloudServerOutlined,
  DatabaseOutlined,
  FolderOpenOutlined,
  LogoutOutlined,
  KeyOutlined,
  MenuOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  ShareAltOutlined,
  TeamOutlined,
  UserOutlined,
} from "@ant-design/icons";
import { Navigate, Outlet, useLocation, useNavigate } from "react-router-dom";
import { useAuth } from "../auth/AuthProvider";

const { Header, Content, Sider } = Layout;

export function ProtectedLayout() {
  const auth = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  const { message } = App.useApp();
  if (auth.loading) return <main className="full-page-status"><Spin size="large" description="正在加载会话" /></main>;
  if (!auth.user) return <Navigate to="/login" state={{ from: location }} replace />;
  const items: MenuProps["items"] = [
    { key: "/projects", icon: <FolderOpenOutlined />, label: "项目" },
    { key: "/shares", icon: <ShareAltOutlined />, label: "分享" },
    { key: "/settings/api-keys", icon: <KeyOutlined />, label: "API 密钥" },
    { key: "/settings/profile", icon: <UserOutlined />, label: "个人设置" },
    ...(auth.user.role === "admin" ? [{ key: "/admin/users", icon: <SafetyCertificateOutlined />, label: "管理中心" }] : []),
  ];
  const selected = location.pathname.startsWith("/admin/")
    ? "/admin/users"
    : items.find((item) => item && "key" in item && location.pathname.startsWith(String(item.key)))?.key as string | undefined;
  return (
    <Layout className="app-shell">
      <Header className="app-header">
        <div className="app-header-inner">
          <Button type="text" className="brand-button" onClick={() => navigate("/projects")} aria-label="前往项目列表">AltasCI云盘</Button>
          <Menu mode="horizontal" selectedKeys={selected ? [selected] : []} items={items} onClick={({ key }) => navigate(key)} className="top-menu" />
          <Dropdown
            trigger={["click"]}
            menu={{ items, selectedKeys: selected ? [selected] : [], onClick: ({ key }) => navigate(key) }}
          >
            <Button type="text" className="mobile-nav-button" icon={<MenuOutlined />} aria-label="打开主导航" />
          </Dropdown>
          <Dropdown
            trigger={["click"]}
            menu={{
              items: [
                { key: "email", label: auth.user.email, disabled: true },
                { type: "divider" },
                { key: "logout", icon: <LogoutOutlined />, label: "退出登录", danger: true },
              ],
              onClick: ({ key }) => {
                if (key === "logout") {
                  void auth.logout()
                    .then(() => window.location.replace("/login"))
                    .catch(() => message.error("退出失败，请稍后重试"));
                }
              },
            }}
          >
            <Button type="text" className="account-button" aria-label={`账户菜单：${auth.user.email}`}>
              <Space size={8}><Avatar size="small" icon={<UserOutlined />} /><span className="account-email">{auth.user.email}</span></Space>
            </Button>
          </Dropdown>
        </div>
      </Header>
      <Content className="app-content"><Outlet /></Content>
    </Layout>
  );
}

export function AdminLayout() {
  const { user } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  if (user?.role !== "admin") return <Navigate to="/projects" replace />;
  const items: MenuProps["items"] = [
    { key: "/admin/users", icon: <TeamOutlined />, label: "用户管理" },
    { key: "/admin/storage", icon: <DatabaseOutlined />, label: "存储后端" },
    { key: "/admin/oidc", icon: <CloudServerOutlined />, label: "OpenID Connect" },
    { key: "/admin/settings", icon: <SettingOutlined />, label: "系统设置" },
  ];
  return (
    <Layout className="admin-shell" hasSider>
      <Sider width={224} className="admin-sider">
        <div className="admin-heading">
          <Typography.Text type="secondary">ADMINISTRATION</Typography.Text>
          <Typography.Title level={4}>管理中心</Typography.Title>
        </div>
        <Menu mode="inline" selectedKeys={[location.pathname]} items={items} onClick={({ key }) => navigate(key)} />
      </Sider>
      <Content className="admin-content">
        <nav className="admin-mobile-nav" aria-label="管理中心导航">
          <Typography.Text strong>管理中心</Typography.Text>
          <Select
            aria-label="管理中心导航"
            value={location.pathname}
            options={items.flatMap((item) => item && "key" in item && "label" in item ? [{ value: String(item.key), label: item.label }] : [])}
            onChange={(path) => navigate(path)}
          />
        </nav>
        <Outlet />
      </Content>
    </Layout>
  );
}
