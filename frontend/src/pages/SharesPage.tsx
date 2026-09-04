import { DeleteOutlined, ShareAltOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, App, Button, Popconfirm, Space, Table, Tag, Typography, type TableColumnsType } from "antd";
import { api } from "../api/client";
import type { Share } from "../api/types";

export function SharesPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const shares = useQuery({ queryKey: ["shares"], queryFn: () => api.request<{ items: Share[] }>("/api/v1/shares") });
  const revoke = useMutation({
    mutationFn: (id: string) => api.request(`/api/v1/shares/${id}`, { method: "DELETE" }),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ["shares"] }); void message.success("分享已撤销"); },
    onError: (error) => void message.error(error instanceof Error ? error.message : "撤销失败"),
  });
  const columns: TableColumnsType<Share> = [
    { title: "目标节点", dataIndex: "target_node_id", render: (id: string) => <Space><ShareAltOutlined /><Typography.Text copyable code>{id}</Typography.Text></Space> },
    { title: "创建时间", dataIndex: "created_at", width: 190, responsive: ["xl"], render: (date: string) => new Date(date).toLocaleString() },
    { title: "过期时间", dataIndex: "expires_at", width: 190, responsive: ["md"], render: (date: string | null) => date ? new Date(date).toLocaleString() : "永不过期" },
    { title: "提取码", dataIndex: "require_code", width: 90, responsive: ["lg"], render: (required: boolean) => required ? <Tag color="processing">需要</Tag> : <Tag>不需要</Tag> },
    { title: "状态", dataIndex: "disabled_at", width: 90, responsive: ["sm"], render: (disabled: string | null) => disabled ? <Tag color="error">已撤销</Tag> : <Tag color="success">有效</Tag> },
    { title: "操作", width: 100, render: (_, share) => !share.disabled_at && <Popconfirm title="撤销分享？" description="撤销后新的公开访问将立即被拒绝。" okText="撤销" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => revoke.mutate(share.id)}><Button danger icon={<DeleteOutlined />}>撤销</Button></Popconfirm> },
  ];
  return (
    <section>
      <div className="enterprise-page-header"><div><Typography.Title level={2}>我的分享</Typography.Title><Typography.Paragraph type="secondary">分享内容随目标节点实时更新，撤销后立即阻止新的公开访问。</Typography.Paragraph></div></div>
      {shares.error && <Alert type="error" showIcon title="分享列表加载失败" description={shares.error.message} className="settings-notice" />}
      <Table<Share> rowKey="id" loading={shares.isLoading} columns={columns} dataSource={shares.data?.items ?? []} pagination={{ pageSize: 20 }} />
    </section>
  );
}
