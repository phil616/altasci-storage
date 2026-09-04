import {
  DeleteOutlined,
  DownloadOutlined,
  EditOutlined,
  FileOutlined,
  FolderAddOutlined,
  FolderOpenOutlined,
  MoreOutlined,
  ShareAltOutlined,
  TeamOutlined,
  UploadOutlined,
} from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  App,
  Breadcrumb,
  Button,
  Card,
  Dropdown,
  Empty,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Typography,
  type MenuProps,
  type TableColumnsType,
} from "antd";
import { useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Node, Project, Share, User } from "../api/types";
import { useAuth } from "../auth/AuthProvider";
import { useUploads } from "../upload/UploadManager";

type Crumb = { id?: string; name: string };
type Member = { user_id: string; permission: "read" | "write" };
type NodeDialog = { type: "rename" | "move"; node: Node };

export function ProjectPage() {
  const { projectId = "" } = useParams();
  const navigate = useNavigate();
  const { user } = useAuth();
  const uploads = useUploads();
  const queryClient = useQueryClient();
  const { message, modal } = App.useApp();
  const input = useRef<HTMLInputElement>(null);
  const [folderForm] = Form.useForm<{ name: string }>();
  const [nodeForm] = Form.useForm<{ name?: string; parentID?: string }>();
  const [memberForm] = Form.useForm<{ userID: string; permission: "read" | "write" }>();
  const [folderOpen, setFolderOpen] = useState(false);
  const [nodeDialog, setNodeDialog] = useState<NodeDialog>();
  const [memberOpen, setMemberOpen] = useState(false);
  const [crumbs, setCrumbs] = useState<Crumb[]>([{ name: "根目录" }]);
  const parentId = crumbs.at(-1)?.id;
  const project = useQuery({ queryKey: ["project", projectId], queryFn: () => api.request<Project>(`/api/v1/projects/${projectId}`) });
  const nodes = useQuery({ queryKey: ["nodes", projectId, parentId], queryFn: () => api.request<{ items: Node[] }>(`/api/v1/projects/${projectId}/nodes${parentId ? `?parent_id=${parentId}` : ""}`) });
  const isAdmin = project.data?.permission === "admin";
  const members = useQuery({ queryKey: ["project-members", projectId], queryFn: () => api.request<{ items: Member[] }>(`/api/v1/projects/${projectId}/members`), enabled: isAdmin });
  const users = useQuery({ queryKey: ["admin-users"], queryFn: () => api.request<{ items: User[] }>("/api/v1/admin/users"), enabled: isAdmin });
  const canWrite = isAdmin || (project.data?.permission === "write" && user?.write_enabled);
  const refreshNodes = () => queryClient.invalidateQueries({ queryKey: ["nodes", projectId] });
  const refreshMembers = () => queryClient.invalidateQueries({ queryKey: ["project-members", projectId] });
  const createFolder = useMutation({
    mutationFn: (name: string) => api.request(`/api/v1/projects/${projectId}/directories`, { method: "POST", body: JSON.stringify({ parent_id: parentId ?? "", name: name.trim() }) }),
    onSuccess: () => { setFolderOpen(false); folderForm.resetFields(); void refreshNodes(); void message.success("文件夹已创建"); },
    onError: (error) => void message.error(error instanceof Error ? error.message : "文件夹创建失败"),
  });
  const patchNode = useMutation({
    mutationFn: ({ id, body }: { id: string; body: object }) => api.request(`/api/v1/nodes/${id}`, { method: "PATCH", body: JSON.stringify(body) }),
    onSuccess: () => { setNodeDialog(undefined); nodeForm.resetFields(); void refreshNodes(); void message.success("节点已更新"); },
    onError: (error) => void message.error(error instanceof Error ? error.message : "节点更新失败"),
  });
  const deleteNode = useMutation({
    mutationFn: (id: string) => api.request(`/api/v1/nodes/${id}`, { method: "DELETE" }),
    onSuccess: () => { void refreshNodes(); void message.success("节点已移入删除队列"); },
    onError: (error) => void message.error(error instanceof Error ? error.message : "删除失败"),
  });
  const putMember = useMutation({
    mutationFn: ({ userID, permission }: { userID: string; permission: "read" | "write" }) => api.request(`/api/v1/projects/${projectId}/members/${userID}`, { method: "PUT", body: JSON.stringify({ permission }) }),
    onSuccess: () => { setMemberOpen(false); memberForm.resetFields(); void refreshMembers(); void message.success("项目成员已更新"); },
    onError: (error) => void message.error(error instanceof Error ? error.message : "成员更新失败"),
  });
  const removeMember = useMutation({
    mutationFn: (userID: string) => api.request(`/api/v1/projects/${projectId}/members/${userID}`, { method: "DELETE" }),
    onSuccess: () => { void refreshMembers(); void message.success("项目成员已移除"); },
    onError: (error) => void message.error(error instanceof Error ? error.message : "成员移除失败"),
  });
  const deleteProject = useMutation({
    mutationFn: () => api.request(`/api/v1/projects/${projectId}`, { method: "DELETE" }),
    onSuccess: () => { void message.success("项目已进入异步删除队列"); navigate("/projects"); },
    onError: (error) => void message.error(error instanceof Error ? error.message : "项目删除失败"),
  });

  async function download(node: Node) {
    try {
      const result = await api.request<{ delivery: string; url: string }>(`/api/v1/nodes/${node.id}/download`, { method: "POST", body: "{}" });
      window.location.assign(result.url);
    } catch (error) {
      void message.error(error instanceof Error ? error.message : "下载失败");
    }
  }

  async function share(node: Node) {
    try {
      const result = await api.request<Share>(`/api/v1/nodes/${node.id}/shares`, { method: "POST", body: JSON.stringify({ require_code: true }) });
      modal.success({
        title: "分享已创建",
        width: 560,
        content: <Space direction="vertical" size={16} className="full-width"><Typography.Text type="secondary">提取码只会显示这一次，请与分享链接分别传递。</Typography.Text><Typography.Text copyable code>{result.url}</Typography.Text><div className="share-created-code"><Typography.Text type="secondary">4 位数字提取码</Typography.Text><Typography.Title level={3} copyable={{ text: result.code ?? "" }}>{result.code}</Typography.Title></div></Space>,
      });
    } catch (error) {
      void message.error(error instanceof Error ? error.message : "分享创建失败");
    }
  }

  function openNodeDialog(type: NodeDialog["type"], node: Node) {
    setNodeDialog({ type, node });
    if (type === "rename") nodeForm.setFieldsValue({ name: node.name });
    else nodeForm.setFieldsValue({ parentID: node.parent_id ?? "" });
  }

  const columns: TableColumnsType<Node> = [
    {
      title: "名称",
      dataIndex: "name",
      render: (name: string, node) => <Button type="link" className="file-link" icon={node.node_type === "directory" ? <FolderOpenOutlined /> : <FileOutlined />} onClick={() => node.node_type === "directory" && setCrumbs([...crumbs, { id: node.id, name: node.name }])}>{name}</Button>,
    },
    { title: "类型", dataIndex: "node_type", width: 100, responsive: ["sm"], render: (type: Node["node_type"]) => type === "directory" ? <Tag color="blue">文件夹</Tag> : <Tag>文件</Tag> },
    { title: "大小", dataIndex: "size", width: 120, responsive: ["md"], render: (size: number, node) => node.node_type === "file" ? formatBytes(size) : "—" },
    { title: "修改时间", dataIndex: "updated_at", width: 190, responsive: ["xl"], render: (date: string) => new Date(date).toLocaleString() },
    {
      title: "操作",
      width: 116,
      render: (_, node) => {
        const items: MenuProps["items"] = [
          ...(node.node_type === "file" ? [{ key: "download", icon: <DownloadOutlined />, label: "下载" }] : []),
          ...(canWrite ? [
            { key: "rename", icon: <EditOutlined />, label: "重命名" },
            { key: "move", icon: <FolderOpenOutlined />, label: "移动" },
            { key: "share", icon: <ShareAltOutlined />, label: "创建分享" },
            { type: "divider" as const },
            { key: "delete", icon: <DeleteOutlined />, label: "删除", danger: true },
          ] : []),
        ];
        return <Dropdown trigger={["click"]} menu={{ items, onClick: ({ key }) => { if (key === "download") void download(node); else if (key === "rename" || key === "move") openNodeDialog(key, node); else if (key === "share") void share(node); else if (key === "delete") modal.confirm({ title: `删除 ${node.name}？`, content: node.node_type === "directory" ? "目录及全部后代会进入异步删除队列。" : "文件对象会进入异步删除队列。", okText: "删除", cancelText: "取消", okButtonProps: { danger: true }, onOk: () => deleteNode.mutateAsync(node.id) }); } }}><Button icon={<MoreOutlined />}>操作</Button></Dropdown>;
      },
    },
  ];

  const memberColumns: TableColumnsType<Member> = [
    { title: "用户", dataIndex: "user_id", render: (id: string) => <div><Typography.Text>{users.data?.items.find((entry) => entry.id === id)?.email ?? "未知用户"}</Typography.Text><Typography.Text type="secondary" className="table-subtitle">{id}</Typography.Text></div> },
    { title: "项目权限", dataIndex: "permission", width: 120, responsive: ["sm"], render: (permission: Member["permission"]) => <Tag color={permission === "write" ? "blue" : "default"}>{permission === "write" ? "Writer" : "Reader"}</Tag> },
    { title: "操作", width: 100, render: (_, member) => <Popconfirm title="移除项目成员？" okText="移除" cancelText="取消" onConfirm={() => removeMember.mutate(member.user_id)}><Button danger>移除</Button></Popconfirm> },
  ];

  if (project.error) return <Alert type="error" showIcon title="项目加载失败" description={project.error.message} />;

  return (
    <section>
      <div className="enterprise-page-header">
        <div>
          <Typography.Title level={2}>{project.data?.name ?? "项目"}</Typography.Title>
          <Breadcrumb items={crumbs.map((crumb, index) => ({ title: <Button type="link" size="small" onClick={() => setCrumbs(crumbs.slice(0, index + 1))}>{crumb.name}</Button> }))} />
        </div>
        <Space wrap>
          {canWrite && <><Button icon={<FolderAddOutlined />} onClick={() => setFolderOpen(true)}>新建文件夹</Button><Button type="primary" icon={<UploadOutlined />} onClick={() => input.current?.click()}>上传文件</Button><input ref={input} hidden multiple type="file" onChange={(event) => { if (event.target.files) uploads.enqueue(event.target.files, projectId, parentId); event.target.value = ""; }} /></>}
          {isAdmin && <Popconfirm title="删除整个项目？" description="全部节点和对象将进入异步删除队列，此操作不可撤销。" okText="删除项目" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => deleteProject.mutate()}><Button danger icon={<DeleteOutlined />} loading={deleteProject.isPending}>删除项目</Button></Popconfirm>}
        </Space>
      </div>
      {nodes.error && <Alert type="error" showIcon title="目录加载失败" description={nodes.error.message} className="settings-notice" />}
      <Card styles={{ body: { padding: 0 } }}>
        <Table<Node> rowKey="id" loading={project.isLoading || nodes.isLoading} columns={columns} dataSource={nodes.data?.items ?? []} pagination={false} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="此目录还没有文件" /> }} />
      </Card>

      {isAdmin && <Card title={<Space><TeamOutlined />项目成员</Space>} extra={<Button icon={<TeamOutlined />} onClick={() => setMemberOpen(true)}>添加成员</Button>} className="members-enterprise-card"><Alert type="info" showIcon title="Writer 仍需启用全局写权限才能修改文件。" className="modal-notice" /><Table<Member> rowKey="user_id" loading={members.isLoading} columns={memberColumns} dataSource={members.data?.items ?? []} pagination={false} /></Card>}

      <Modal title="新建文件夹" open={folderOpen} okText="创建" cancelText="取消" confirmLoading={createFolder.isPending} destroyOnHidden onCancel={() => setFolderOpen(false)} onOk={() => folderForm.submit()}><Form form={folderForm} layout="vertical" onFinish={({ name }) => createFolder.mutate(name)}><Form.Item name="name" label="文件夹名称" rules={[{ required: true, whitespace: true }]}><Input maxLength={255} autoFocus /></Form.Item></Form></Modal>

      <Modal title={nodeDialog?.type === "rename" ? "重命名" : "移动节点"} open={Boolean(nodeDialog)} okText="保存" cancelText="取消" confirmLoading={patchNode.isPending} destroyOnHidden onCancel={() => setNodeDialog(undefined)} onOk={() => nodeForm.submit()}>
        <Form form={nodeForm} layout="vertical" onFinish={(values) => { if (!nodeDialog) return; patchNode.mutate({ id: nodeDialog.node.id, body: nodeDialog.type === "rename" ? { name: values.name?.trim() } : { parent_id: values.parentID?.trim() ?? "" } }); }}>
          {nodeDialog?.type === "rename" ? <Form.Item name="name" label="新名称" rules={[{ required: true, whitespace: true }]}><Input maxLength={255} autoFocus /></Form.Item> : <Form.Item name="parentID" label="目标目录 ID" extra="留空表示移动到项目根目录。"><Input placeholder="留空移动到根目录" /></Form.Item>}
        </Form>
      </Modal>

      <Modal title="添加项目成员" open={memberOpen} okText="保存" cancelText="取消" confirmLoading={putMember.isPending} destroyOnHidden onCancel={() => setMemberOpen(false)} onOk={() => memberForm.submit()}>
        <Form form={memberForm} layout="vertical" initialValues={{ permission: "read" }} onFinish={(values) => putMember.mutate(values)}>
          <Form.Item name="userID" label="用户" rules={[{ required: true }]}><Select showSearch optionFilterProp="label" options={(users.data?.items ?? []).filter((entry) => entry.role !== "admin").map((entry) => ({ value: entry.id, label: entry.email }))} /></Form.Item>
          <Form.Item name="permission" label="项目权限" rules={[{ required: true }]}><Select options={[{ value: "read", label: "Reader · 只读" }, { value: "write", label: "Writer · 可写" }]} /></Form.Item>
        </Form>
      </Modal>
    </section>
  );
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KiB`;
  if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} MiB`;
  return `${(value / 1024 ** 3).toFixed(1)} GiB`;
}
