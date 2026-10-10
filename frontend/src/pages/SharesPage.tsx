import { DeleteOutlined, ShareAltOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, App, Button, Popconfirm, Space, Table, Tag, Typography, type TableColumnsType } from "antd";
import { api } from "../api/client";
import { ShareCreatedContent } from "../components/ShareCreatedContent";
import type { Share } from "../api/types";

export function SharesPage() {
  const { message, modal } = App.useApp();
  const queryClient = useQueryClient();
  const shares = useQuery({ queryKey: ["shares"], queryFn: () => api.request<{ items: Share[] }>("/api/v1/shares") });
  const revoke = useMutation({
    mutationFn: (id: string) => api.request(`/api/v1/shares/${id}`, { method: "DELETE" }),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ["shares"] }); void message.success("分享已撤销"); },
    onError: (error) => void message.error(error instanceof Error ? error.message : "撤销失败"),
  });
  const columns: TableColumnsType<Share> = [
    { title: "分享内容", dataIndex: "target_node_id", render: (id: string, share) => <Space><ShareAltOutlined /><Typography.Text>{(share.target_node_ids?.length ?? 1) > 1 ? `${share.target_node_ids!.length} 个项目` : id}</Typography.Text></Space> },
    { title: "分享链接", render: (_, share) => share.url ? <Space direction="vertical"><Typography.Text copyable code>{share.url}</Typography.Text><Button size="small" onClick={() => modal.info({ title: "分享链接和密码", width: 560, content: <ShareCreatedContent share={share} /> })}>查看分享信息</Button></Space> : <Typography.Text type="secondary">旧版链接未保存，请重新分享</Typography.Text> },
    { title: "创建时间", dataIndex: "created_at", width: 190, responsive: ["xl"], render: (date: string) => new Date(date).toLocaleString() },
    { title: "过期时间", dataIndex: "expires_at", width: 190, responsive: ["md"], render: (date: string | null) => date ? new Date(date).toLocaleString() : "永不过期" },
    { title: "提取码", width: 150, render: (_, share) => !share.require_code ? <Tag>不需要</Tag> : share.code ? <Typography.Text copyable code>{share.code}</Typography.Text> : <Typography.Text type="secondary">旧版密码无法恢复</Typography.Text> },
    { title: "状态", dataIndex: "disabled_at", width: 90, responsive: ["sm"], render: (disabled: string | null) => disabled ? <Tag color="error">已撤销</Tag> : <Tag color="success">有效</Tag> },
    { title: "操作", width: 100, render: (_, share) => !share.disabled_at && <Popconfirm title="撤销分享？" description="撤销后新的公开访问将立即被拒绝。" okText="撤销" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => revoke.mutate(share.id)}><Button danger icon={<DeleteOutlined />}>撤销</Button></Popconfirm> },
  ];
  return (
    <section>
      <div className="enterprise-page-header"><div><Typography.Title level={2}>我的分享</Typography.Title><Typography.Paragraph type="secondary">分享内容随目标节点实时更新，撤销后立即阻止新的公开访问。</Typography.Paragraph></div></div>
      {shares.error && <Alert type="error" showIcon title="分享列表加载失败" description={shares.error.message} className="settings-notice" />}
      <Table<Share> rowKey="id" loading={shares.isLoading} columns={columns} dataSource={shares.data?.items ?? []} scroll={{ x: 1000 }} pagination={{ pageSize: 20 }} />
    </section>
  );
}
