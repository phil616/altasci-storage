import { FolderOpenOutlined, PlusOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, App, Avatar, Button, Card, Col, Empty, Form, Input, Modal, Row, Select, Skeleton, Space, Tag, Typography } from "antd";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import type { Project, StorageBackend } from "../api/types";
import { useAuth } from "../auth/AuthProvider";

type ProjectForm = { name: string; description: string; storageBackendID: string };

export function ProjectsPage() {
  const { user } = useAuth();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const [form] = Form.useForm<ProjectForm>();
  const [open, setOpen] = useState(false);
  const canCreate = user?.role === "admin" || user?.write_enabled;
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.request<{ items: Project[] }>("/api/v1/projects") });
  const storage = useQuery({ queryKey: ["available-storage"], queryFn: () => api.request<{ items: StorageBackend[] }>("/api/v1/storage-backends"), enabled: canCreate });
  const create = useMutation({
    mutationFn: (values: ProjectForm) => api.request<Project>("/api/v1/projects", { method: "POST", body: JSON.stringify({ name: values.name.trim(), description: values.description?.trim() ?? "", storage_backend_id: values.storageBackendID }) }),
    onSuccess: (project) => {
      setOpen(false);
      form.resetFields();
      void queryClient.invalidateQueries({ queryKey: ["projects"] });
      void message.success("项目已创建");
      navigate(`/projects/${project.id}`);
    },
    onError: (error) => void message.error(error instanceof Error ? error.message : "项目创建失败"),
  });

  return (
    <section>
      <div className="enterprise-page-header">
        <div><Typography.Title level={2}>项目</Typography.Title><Typography.Paragraph type="secondary">按项目隔离文件、存储后端与成员权限。</Typography.Paragraph></div>
        {canCreate && <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>新建项目</Button>}
      </div>
      {projects.error && <Alert type="error" showIcon title="项目加载失败" description={projects.error.message} className="settings-notice" />}
      {projects.isLoading ? (
        <Row gutter={[16, 16]}>{[1, 2, 3].map((item) => <Col xs={24} md={12} xl={8} key={item}><Card><Skeleton active paragraph={{ rows: 2 }} /></Card></Col>)}</Row>
      ) : projects.data?.items.length ? (
        <Row gutter={[16, 16]}>
          {projects.data.items.map((project) => (
            <Col xs={24} md={12} xl={8} key={project.id}>
              <Card hoverable className="project-enterprise-card" onClick={() => navigate(`/projects/${project.id}`)}>
                <Space align="start" size={14} className="project-card-layout">
                  <Avatar size={46} shape="square" icon={<FolderOpenOutlined />} className="project-avatar" />
                  <div className="project-card-copy">
                    <Space wrap><Typography.Title level={4}>{project.name}</Typography.Title><Tag color={project.permission === "admin" ? "purple" : project.permission === "write" ? "blue" : "default"}>{project.permission}</Tag></Space>
                    <Typography.Paragraph type="secondary" ellipsis={{ rows: 2 }}>{project.description || "暂无项目描述"}</Typography.Paragraph>
                    <Typography.Text type="secondary" className="table-subtitle">更新于 {new Date(project.updated_at).toLocaleString()}</Typography.Text>
                  </div>
                </Space>
              </Card>
            </Col>
          ))}
        </Row>
      ) : <Card><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前没有可访问的项目" /></Card>}

      <Modal title="新建项目" open={open} okText="创建" cancelText="取消" destroyOnHidden confirmLoading={create.isPending} onCancel={() => setOpen(false)} onOk={() => form.submit()}>
        <Form<ProjectForm> form={form} layout="vertical" onFinish={(values) => create.mutate(values)}>
          <Form.Item name="name" label="项目名称" rules={[{ required: true, whitespace: true, message: "请输入项目名称" }]}><Input maxLength={120} showCount /></Form.Item>
          <Form.Item name="description" label="项目描述"><Input.TextArea rows={3} maxLength={500} showCount /></Form.Item>
          <Form.Item name="storageBackendID" label="存储后端" extra="项目创建后不能直接切换存储后端。" rules={[{ required: true, message: "请选择存储后端" }]}>
            <Select loading={storage.isLoading} placeholder="选择已启用的存储后端" options={(storage.data?.items ?? []).map((backend) => ({ value: backend.id, label: `${backend.name} · ${backend.type}` }))} />
          </Form.Item>
        </Form>
      </Modal>
    </section>
  );
}
