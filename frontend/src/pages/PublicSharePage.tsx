import { DownloadOutlined, FileOutlined, FolderOpenOutlined, SafetyCertificateOutlined } from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import { Alert, App, Breadcrumb, Button, Card, Empty, Form, Input, Result, Space, Spin, Table, Tag, Typography, type TableColumnsType } from "antd";
import { useState } from "react";
import { useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Node, Share } from "../api/types";

type Meta = { share: Share; target: Node; grant_required: boolean };
type PublicCrumb = { id?: string; name: string };

export function PublicSharePage() {
  const { shareToken = "" } = useParams();
  const { message } = App.useApp();
  const [grant, setGrant] = useState<string>();
  const [crumbs, setCrumbs] = useState<PublicCrumb[]>([]);
  const [verifyError, setVerifyError] = useState("");
  const parent = crumbs.at(-1);
  const meta = useQuery({ queryKey: ["public-share", shareToken], queryFn: () => api.request<Meta>(`/api/v1/public/shares/${shareToken}/`) });
  const authorized = Boolean(meta.data && (!meta.data.grant_required || grant));
  const nodes = useQuery({ queryKey: ["public-nodes", shareToken, parent?.id, grant], enabled: authorized, queryFn: () => api.request<{ items: Node[] }>(`/api/v1/public/shares/${shareToken}/nodes${parent?.id ? `?parent_id=${parent.id}` : ""}`, { headers: grant ? { Authorization: `Bearer ${grant}` } : undefined }) });

  async function verify({ code }: { code: string }) {
    setVerifyError("");
    try {
      const result = await api.request<{ grant: string }>(`/api/v1/public/shares/${shareToken}/verify`, { method: "POST", body: JSON.stringify({ code: code.toUpperCase() }) });
      setGrant(result.grant);
    } catch (error) {
      setVerifyError(error instanceof Error ? error.message : "提取码错误");
    }
  }

  async function download(node: Node) {
    try {
      const headers = grant ? { Authorization: `Bearer ${grant}` } : undefined;
      const result = await api.request<{ delivery: string; url: string }>(`/api/v1/public/shares/${shareToken}/nodes/${node.id}/download`, { method: "POST", body: "{}", headers });
      if (result.delivery === "external") { window.location.assign(result.url); return; }
      const response = await fetch(result.url, { headers, credentials: "omit" });
      if (!response.ok) throw new Error(`下载失败（HTTP ${response.status}）`);
      const blob = await response.blob();
      const href = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = href;
      anchor.download = node.name;
      anchor.click();
      URL.revokeObjectURL(href);
    } catch (error) {
      void message.error(error instanceof Error ? error.message : "下载失败");
    }
  }

  if (meta.isLoading) return <main className="public-page public-centered"><Spin size="large" description="正在验证分享链接" /></main>;
  if (meta.error || !meta.data) return <main className="public-page public-centered"><Card className="public-result-card"><Result status="warning" title="分享不可用" subTitle="链接可能已过期、被撤销或不存在。" /></Card></main>;
  if (meta.data.grant_required && !grant) {
    const codeLength = meta.data.share.code_length === 4 ? 4 : 8;
    const numericCode = codeLength === 4;
    return (
      <main className="public-page public-centered">
        <Card className="share-enterprise-card" variant="borderless">
          <Space direction="vertical" size={6} className="login-heading share-code-heading"><Typography.Text className="brand-wordmark">AltasCI云盘</Typography.Text><Typography.Title level={2}>{meta.data.target.name}</Typography.Title><Typography.Text type="secondary"><SafetyCertificateOutlined /> 此分享需要提取码</Typography.Text></Space>
          <Form className="share-code-form" layout="vertical" size="large" onFinish={(values) => void verify(values)}>
            <Form.Item
              name="code"
              label={numericCode ? "4 位数字提取码" : "8 位提取码（旧版分享）"}
              rules={[
                { required: true, message: "请输入提取码" },
                { len: codeLength, message: `请输入完整的 ${codeLength} 位提取码` },
                ...(numericCode ? [{ pattern: /^\d{4}$/, message: "提取码只能包含 4 位数字" }] : []),
              ]}
            >
              <Input.OTP
                className="share-code-otp"
                length={codeLength}
                size="large"
                type="text"
                inputMode={numericCode ? "numeric" : "text"}
                autoComplete="one-time-code"
                formatter={(value) => numericCode ? value.replace(/\D/g, "") : value.toUpperCase()}
              />
            </Form.Item>
            {verifyError && <Form.Item><Alert type="error" showIcon title={verifyError} /></Form.Item>}
            <Button type="primary" htmlType="submit" block>查看分享</Button>
          </Form>
        </Card>
      </main>
    );
  }

  const columns: TableColumnsType<Node> = [
    { title: "名称", dataIndex: "name", render: (name: string, node) => <Button type="link" className="file-link" icon={node.node_type === "directory" ? <FolderOpenOutlined /> : <FileOutlined />} onClick={() => node.node_type === "directory" && setCrumbs([...crumbs, { id: node.id, name }])}>{name}</Button> },
    { title: "类型", dataIndex: "node_type", width: 100, responsive: ["sm"], render: (type: Node["node_type"]) => type === "directory" ? <Tag color="blue">文件夹</Tag> : <Tag>文件</Tag> },
    { title: "大小", dataIndex: "size", width: 120, responsive: ["md"], render: (size: number, node) => node.node_type === "file" ? formatBytes(size) : "—" },
    { title: "修改时间", dataIndex: "updated_at", width: 190, responsive: ["xl"], render: (date: string) => new Date(date).toLocaleString() },
    { title: "操作", width: 110, render: (_, node) => node.node_type === "file" && <Button icon={<DownloadOutlined />} onClick={() => void download(node)}>下载</Button> },
  ];

  const rootName = meta.data.target.name;
  return (
    <main className="public-page">
      <section className="public-enterprise-browser">
        <div className="enterprise-page-header">
          <div><Typography.Text className="public-eyebrow">ALTASCI SECURE SHARE</Typography.Text><Typography.Title level={2}>{parent?.name ?? rootName}</Typography.Title><Typography.Paragraph type="secondary">{meta.data.share.expires_at ? `有效期至 ${new Date(meta.data.share.expires_at).toLocaleString()}` : "长期有效"}</Typography.Paragraph><Breadcrumb items={[{ title: <Button type="link" size="small" onClick={() => setCrumbs([])}>{rootName}</Button> }, ...crumbs.map((crumb, index) => ({ title: <Button type="link" size="small" onClick={() => setCrumbs(crumbs.slice(0, index + 1))}>{crumb.name}</Button> }))]} /></div>
        </div>
        {nodes.error && <Alert type="error" showIcon title="分享内容加载失败" description={nodes.error.message} className="settings-notice" />}
        <Card styles={{ body: { padding: 0 } }}><Table<Node> rowKey="id" loading={nodes.isLoading} columns={columns} dataSource={nodes.data?.items ?? []} pagination={false} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有可显示的文件" /> }} /></Card>
      </section>
    </main>
  );
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KiB`;
  if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} MiB`;
  return `${(value / 1024 ** 3).toFixed(1)} GiB`;
}
