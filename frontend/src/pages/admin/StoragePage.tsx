import { CloudServerOutlined, DatabaseOutlined, DeleteOutlined, EditOutlined, ExperimentOutlined, MoreOutlined, PlusOutlined, QuestionCircleOutlined, SafetyCertificateOutlined } from "@ant-design/icons";
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
  type TableColumnsType,
  type MenuProps,
} from "antd";
import { useState } from "react";
import { api, APIError } from "../../api/client";
import type { StorageBackend } from "../../api/types";

type StorageType = StorageBackend["type"];
type StorageEditor = { mode: "create" } | { mode: "edit"; backend: StorageBackend };
type StorageFormValues = {
  name: string;
  type: StorageType;
  rootPath: string;
  endpoint: string;
  region: string;
  bucket: string;
  prefix: string;
  forcePathStyle: boolean;
  useCName: boolean;
  accessKeyID: string;
  secretAccessKey: string;
};

const initialValues: StorageFormValues = {
  name: "",
  type: "local",
  rootPath: "/data/altasci-storage",
  endpoint: "",
  region: "us-east-1",
  bucket: "",
  prefix: "network-storage/",
  forcePathStyle: false,
  useCName: false,
  accessKeyID: "",
  secretAccessKey: "",
};

function createPayload(values: StorageFormValues) {
  if (values.type === "local") {
    return { name: values.name.trim(), type: values.type, enabled: true, config: { root_path: values.rootPath.trim() }, secret: null };
  }
  if (values.type === "s3") {
    return {
      name: values.name.trim(),
      type: values.type,
      enabled: true,
      config: { endpoint: values.endpoint.trim(), region: values.region.trim(), bucket: values.bucket.trim(), prefix: values.prefix.trim(), force_path_style: values.forcePathStyle },
      secret: { access_key_id: values.accessKeyID.trim(), secret_access_key: values.secretAccessKey },
    };
  }
  return {
    name: values.name.trim(),
    type: values.type,
    enabled: true,
    config: { endpoint: values.endpoint.trim(), region: values.region.trim(), bucket: values.bucket.trim(), prefix: values.prefix.trim(), use_cname: values.useCName },
    secret: { access_key_id: values.accessKeyID.trim(), access_key_secret: values.secretAccessKey },
  };
}

function updatePayload(values: StorageFormValues, backend: StorageBackend) {
  const payload = createPayload(values);
  return {
    name: payload.name,
    enabled: backend.enabled,
    config: payload.config,
    ...(values.type !== "local" && values.accessKeyID.trim() && values.secretAccessKey ? { secret: payload.secret } : {}),
  };
}

function formValues(backend: StorageBackend): StorageFormValues {
  const text = (key: string, fallback = "") => typeof backend.config[key] === "string" ? String(backend.config[key]) : fallback;
  return {
    ...initialValues,
    name: backend.name,
    type: backend.type,
    rootPath: text("root_path", initialValues.rootPath),
    endpoint: text("endpoint"),
    region: text("region", backend.type === "aliyun_oss" ? "cn-hangzhou" : "us-east-1"),
    bucket: text("bucket"),
    prefix: text("prefix", initialValues.prefix),
    forcePathStyle: backend.config.force_path_style === true,
    useCName: backend.config.use_cname === true,
    accessKeyID: "",
    secretAccessKey: "",
  };
}

function storageError(error: unknown, fallback: string) {
  if (error instanceof APIError && error.code === "STORAGE_BACKEND_IN_USE") return "该存储后端仍被项目或对象记录引用，不能删除。请保留并停用它，以免文件数据失去存储定位。";
  if (error instanceof APIError && error.code === "CONFLICT") return "存储后端名称重复，或当前配置与现有数据冲突。";
  return error instanceof Error ? error.message : fallback;
}

function typeLabel(type: StorageType) {
  if (type === "local") return "本地磁盘";
  if (type === "s3") return "S3 / MinIO";
  return "Alibaba Cloud OSS";
}

function configText(backend: StorageBackend | null, key: string, fallback: string) {
  const value = backend?.config[key];
  return typeof value === "string" && value.trim() ? value.trim() : fallback;
}

function OSSConfigurationHelp({ backend, open, onClose }: { backend: StorageBackend | null; open: boolean; onClose(): void }) {
  const origin = window.location.origin;
  const bucket = configText(backend, "bucket", "<your-bucket>");
  const prefix = configText(backend, "prefix", "<your-prefix>").replace(/^\/+|\/+$/g, "");
  const endpoint = configText(backend, "endpoint", "https://oss-cn-hangzhou.aliyuncs.com");
  const objectResource = `acs:oss:*:*:${bucket}/${prefix ? `${prefix}/` : ""}*`;
  const ramPolicy = JSON.stringify({
    Version: "1",
    Statement: [{
      Effect: "Allow",
      Action: ["oss:PutObject", "oss:GetObject", "oss:DeleteObject", "oss:AbortMultipartUpload"],
      Resource: [objectResource],
    }],
  }, null, 2);

  return (
    <Modal title="Alibaba Cloud OSS 外部配置指南" open={open} width={820} footer={null} onCancel={onClose} destroyOnHidden>
      <div className="oss-help-content">
        <Alert
          type="warning"
          showIcon
          title="后台连接测试不会验证浏览器 CORS"
          description="AltasCI 使用预签名 URL，让浏览器直接向 OSS 上传数据。连接测试成功但上传提示网络连接异常时，请优先检查本页的 Bucket CORS、Endpoint、DNS 和 HTTPS 配置。"
          className="modal-notice"
        />

        <Typography.Title level={5}>1. 设置访问控制</Typography.Title>
        <ul className="oss-help-list">
          <li>将 Bucket ACL 设置为<strong>私有（Private）</strong>，不要为了修复上传问题改成公共读或公共读写。</li>
          <li>为 AltasCI 创建独立 RAM 用户和 AccessKey，并按实际 Bucket 与对象前缀授予最小权限。</li>
          <li>CORS 只允许浏览器跨域，不会替代预签名 URL、RAM Policy 或 Bucket ACL。</li>
        </ul>
        <Typography.Paragraph type="secondary">当前存储配置对应的 RAM Policy 参考：</Typography.Paragraph>
        <div className="config-preview-shell">
          <div className="config-preview-toolbar"><Typography.Text type="secondary">RAM Policy</Typography.Text><Typography.Text copyable={{ text: ramPolicy }}>复制 JSON</Typography.Text></div>
          <pre className="config-preview"><code>{ramPolicy}</code></pre>
        </div>

        <Typography.Title level={5}>2. 创建 Bucket CORS 规则</Typography.Title>
        <Typography.Paragraph>
          进入阿里云 OSS 控制台，选择目标 Bucket，然后进入“数据安全 → 跨域设置 → 创建规则”，填写以下内容：
        </Typography.Paragraph>
        <Descriptions bordered size="small" column={1} className="oss-help-details">
          <Descriptions.Item label="来源 Origin"><Typography.Text code copyable={{ text: origin }}>{origin}</Typography.Text><Typography.Text type="secondary"> 当前前端地址，必须包含协议和端口且不能带路径。</Typography.Text></Descriptions.Item>
          <Descriptions.Item label="允许 Methods"><Typography.Text code copyable>GET, HEAD, PUT</Typography.Text></Descriptions.Item>
          <Descriptions.Item label="允许 Headers"><Typography.Text code copyable>*</Typography.Text><Typography.Text type="secondary"> 确保 Content-Type、Content-MD5 和 x-oss-* 等签名请求头可以通过预检。</Typography.Text></Descriptions.Item>
          <Descriptions.Item label="暴露 Headers"><Typography.Text code copyable>ETag, x-oss-request-id, x-oss-hash-crc64ecma</Typography.Text><Typography.Text type="secondary"> ETag 是完成分片上传的必要响应头。</Typography.Text></Descriptions.Item>
          <Descriptions.Item label="缓存时间"><Typography.Text code copyable>600</Typography.Text> 秒</Descriptions.Item>
          <Descriptions.Item label="Vary: Origin"><Typography.Text strong>开启</Typography.Text></Descriptions.Item>
        </Descriptions>
        <Alert
          type="info"
          showIcon
          title="来源填写前端地址，不是 API 或 OSS 地址"
          description="生产环境不要使用 * 作为 Origin。若有多个可信前端域名，请逐一添加精确的 HTTPS Origin。系统设置中的 API CORS 与此处的 OSS Bucket CORS 是两套独立配置，两者都必须正确。"
          className="oss-help-note"
        />

        <Typography.Title level={5}>3. 核对 Endpoint 与网络</Typography.Title>
        <Descriptions bordered size="small" column={1} className="oss-help-details">
          <Descriptions.Item label="当前 Endpoint"><Typography.Text code copyable={{ text: endpoint }}>{endpoint}</Typography.Text></Descriptions.Item>
          <Descriptions.Item label="标准 Endpoint">选择与 Bucket Region 一致、可从用户浏览器访问的公网 HTTPS Endpoint；不要填写带 <Typography.Text code>-internal</Typography.Text> 的内网地址。</Descriptions.Item>
          <Descriptions.Item label="自定义 CNAME">填写完整 HTTPS 域名并打开“使用 CNAME”，同时确保 DNS 和证书有效。若前面使用 CDN，需要由 CDN 返回 CORS 响应头或透传 OSS 的 CORS 响应头。</Descriptions.Item>
        </Descriptions>

        <Typography.Title level={5}>4. 保存后的验证顺序</Typography.Title>
        <ol className="oss-help-list">
          <li>回到存储后端列表执行“测试连接”，确认 RAM 权限、Bucket 和 Region 正确。</li>
          <li>从当前前端域名上传一个小文件，浏览器会先向 OSS 发起跨域预检，再执行预签名 PUT。</li>
          <li>若仍失败，在浏览器开发者工具的 Network 中检查 OSS 的 OPTIONS/PUT 请求；确认响应包含匹配当前 Origin 的 CORS 头，且 PUT 响应暴露 ETag。</li>
        </ol>
        <Typography.Link href="https://help.aliyun.com/zh/oss/user-guide/configure-cross-origin-resource-sharing" target="_blank" rel="noreferrer">查看阿里云 OSS 官方跨域配置文档</Typography.Link>
      </div>
    </Modal>
  );
}

export function StoragePage() {
  const [form] = Form.useForm<StorageFormValues>();
  const selectedType = Form.useWatch("type", form) ?? "local";
  const [editor, setEditor] = useState<StorageEditor | null>(null);
  const [helpOpen, setHelpOpen] = useState(false);
  const [helpBackend, setHelpBackend] = useState<StorageBackend | null>(null);
  const { message, modal } = App.useApp();
  const queryClient = useQueryClient();
  const list = useQuery({ queryKey: ["storage"], queryFn: () => api.request<{ items: StorageBackend[] }>("/api/v1/admin/storage-backends") });
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["storage"] });
  const create = useMutation({
    mutationFn: (values: StorageFormValues) => api.request("/api/v1/admin/storage-backends", { method: "POST", body: JSON.stringify(createPayload(values)) }),
    onSuccess: () => {
      setEditor(null);
      form.resetFields();
      void refresh();
      void message.success("存储后端已创建");
    },
    onError: (error) => void message.error(storageError(error, "创建失败")),
  });
  const test = useMutation({
    mutationFn: (id: string) => api.request(`/api/v1/admin/storage-backends/${id}/test`, { method: "POST", body: "{}" }),
    onSuccess: () => { void refresh(); void message.success("连接及读写测试通过"); },
    onError: (error) => void message.error(error instanceof Error ? error.message : "连接测试失败"),
  });
  const toggle = useMutation({
    mutationFn: (backend: StorageBackend) => api.request(`/api/v1/admin/storage-backends/${backend.id}`, { method: "PATCH", body: JSON.stringify({ name: backend.name, enabled: !backend.enabled, config: backend.config }) }),
    onSuccess: () => void refresh(),
    onError: (error) => void message.error(storageError(error, "状态更新失败")),
  });
  const save = useMutation({
    mutationFn: ({ backend, values }: { backend: StorageBackend; values: StorageFormValues }) => api.request(`/api/v1/admin/storage-backends/${backend.id}`, { method: "PATCH", body: JSON.stringify(updatePayload(values, backend)) }),
    onSuccess: () => {
      setEditor(null);
      form.resetFields();
      void refresh();
      void message.success("存储后端已更新");
    },
    onError: (error) => void message.error(storageError(error, "更新失败")),
  });
  const remove = useMutation({
    mutationFn: (id: string) => api.request(`/api/v1/admin/storage-backends/${id}`, { method: "DELETE" }),
    onSuccess: () => { void refresh(); void message.success("存储后端已删除"); },
    onError: (error) => void message.error(storageError(error, "删除失败")),
  });
  const showOSSHelp = (backend: StorageBackend | null = null) => {
    setHelpBackend(backend);
    setHelpOpen(true);
  };
  const openCreate = () => {
    form.resetFields();
    form.setFieldsValue(initialValues);
    setEditor({ mode: "create" });
  };
  const openEdit = (backend: StorageBackend) => {
    form.resetFields();
    form.setFieldsValue(formValues(backend));
    setEditor({ mode: "edit", backend });
  };
  const submitEditor = (values: StorageFormValues) => {
    if (values.type !== "local" && Boolean(values.accessKeyID.trim()) !== Boolean(values.secretAccessKey)) {
      void message.error("更新凭据时必须同时填写 Access Key ID 和 Secret；全部留空则保留现有凭据。");
      return;
    }
    if (editor?.mode === "create") create.mutate(values);
    else if (editor?.mode === "edit") save.mutate({ backend: editor.backend, values });
  };

  const columns: TableColumnsType<StorageBackend> = [
    {
      title: "名称",
      dataIndex: "name",
      render: (name: string, backend) => <Space><DatabaseOutlined /><div><Typography.Text strong>{name}</Typography.Text><Typography.Text type="secondary" className="table-subtitle">{backend.id}</Typography.Text></div></Space>,
    },
    { title: "类型", dataIndex: "type", width: 170, responsive: ["md"], render: (type: StorageType) => <Tag icon={type === "local" ? <DatabaseOutlined /> : <CloudServerOutlined />} color={type === "local" ? "default" : "blue"}>{typeLabel(type)}</Tag> },
    { title: "凭据", dataIndex: "has_secret", width: 100, responsive: ["lg"], render: (hasSecret: boolean, backend) => backend.type === "local" ? "不需要" : hasSecret ? <Tag color="success">已保存</Tag> : <Tag color="error">缺失</Tag> },
    { title: "连接状态", dataIndex: "last_test_status", width: 120, responsive: ["sm"], render: (status: string | null) => status === "ok" ? <Tag color="success">正常</Tag> : status === "failed" ? <Tag color="error">失败</Tag> : <Tag>未测试</Tag> },
    { title: "启用", dataIndex: "enabled", width: 80, responsive: ["sm"], render: (enabled: boolean, backend) => <Switch checked={enabled} loading={toggle.isPending && toggle.variables?.id === backend.id} onChange={() => toggle.mutate(backend)} /> },
    {
      title: "操作",
      key: "actions",
      width: 116,
      render: (_, backend) => {
        const items: MenuProps["items"] = [
          { key: "edit", icon: <EditOutlined />, label: "编辑配置" },
          ...(backend.type === "aliyun_oss" ? [{ key: "oss-help", icon: <QuestionCircleOutlined />, label: "OSS 外部配置" }] : []),
          { key: "test", icon: <ExperimentOutlined />, label: "测试连接" },
          { type: "divider" },
          { key: "delete", icon: <DeleteOutlined />, label: "删除", danger: true },
        ];
        return (
          <Dropdown
            trigger={["click"]}
            menu={{
              items,
              onClick: ({ key }) => {
                if (key === "edit") openEdit(backend);
                else if (key === "oss-help") showOSSHelp(backend);
                else if (key === "test") test.mutate(backend.id);
                else if (key === "delete") {
                  const projectCount = backend.project_count ?? 0;
                  const blobCount = backend.blob_count ?? 0;
                  if (projectCount > 0 || blobCount > 0) {
                    modal.warning({
                      title: "该存储后端正在使用中",
                      content: `当前仍有 ${projectCount} 个项目和 ${blobCount} 条对象记录引用此后端。为避免文件无法定位，只能停用，不能删除。`,
                      okText: "知道了",
                    });
                  } else {
                    modal.confirm({
                      title: "删除存储后端？",
                      content: "该条目当前没有项目或对象引用。删除后无法恢复，但不会删除远端 Bucket 中的数据。",
                      okText: "删除",
                      cancelText: "取消",
                      okButtonProps: { danger: true },
                      onOk: () => remove.mutateAsync(backend.id),
                    });
                  }
                }
              },
            }}
          >
            <Button icon={<MoreOutlined />} loading={test.isPending && test.variables === backend.id}>操作</Button>
          </Dropdown>
        );
      },
    },
  ];

  return (
    <section>
      <div className="enterprise-page-header">
        <div><Typography.Title level={2}>存储后端</Typography.Title><Typography.Paragraph type="secondary">统一管理本地磁盘、S3 兼容存储和 Alibaba Cloud OSS。凭据加密保存且永不回显。</Typography.Paragraph></div>
        <Space wrap>
          <Button icon={<QuestionCircleOutlined />} onClick={() => showOSSHelp()}>OSS 配置帮助</Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>添加存储后端</Button>
        </Space>
      </div>
      <Alert
        type="info"
        showIcon
        icon={<SafetyCertificateOutlined />}
        title="对象存储需要单独配置浏览器跨域访问"
        description="预签名上传由浏览器直接连接 OSS/S3；如果连接测试成功但上传提示网络连接异常，请检查 Bucket CORS。"
        action={<Button size="small" onClick={() => showOSSHelp()}>查看 OSS 配置</Button>}
        className="settings-notice"
      />
      {list.error && <Alert type="error" showIcon title="存储后端加载失败" description={list.error.message} className="settings-notice" />}
      <Table<StorageBackend>
        rowKey="id"
        loading={list.isLoading}
        columns={columns}
        dataSource={list.data?.items ?? []}
        pagination={false}
        expandable={{
          expandedRowRender: (backend) => (
            <Descriptions size="small" column={{ xs: 1, sm: 2, lg: 3 }}>
              {Object.entries(backend.config).map(([key, fieldValue]) => <Descriptions.Item key={key} label={key}><Typography.Text code>{String(fieldValue)}</Typography.Text></Descriptions.Item>)}
              <Descriptions.Item label="引用项目">{backend.project_count ?? 0}</Descriptions.Item>
              <Descriptions.Item label="对象记录">{backend.blob_count ?? 0}</Descriptions.Item>
              {backend.last_test_message && <Descriptions.Item label="最近测试" span={3}>{backend.last_test_message}</Descriptions.Item>}
            </Descriptions>
          ),
        }}
      />

      <OSSConfigurationHelp backend={helpBackend} open={helpOpen} onClose={() => setHelpOpen(false)} />

      <Modal title={editor?.mode === "edit" ? "编辑存储后端" : "添加存储后端"} open={editor !== null} width={680} destroyOnHidden confirmLoading={create.isPending || save.isPending} okText="保存" cancelText="取消" onCancel={() => setEditor(null)} onOk={() => form.submit()}>
        <Alert type="info" showIcon title={editor?.mode === "edit" ? "存储类型不可修改；Access Key 和 Secret 全部留空时保留现有凭据。" : "Secret 只在本次创建时提交，保存后无法从浏览器读取。"} className="modal-notice" />
        <Form<StorageFormValues> form={form} layout="vertical" initialValues={initialValues} onFinish={submitEditor}>
          <Form.Item name="name" label="显示名称" rules={[{ required: true, whitespace: true, message: "请输入名称" }]}><Input placeholder="生产对象存储" /></Form.Item>
          <Form.Item name="type" label="存储类型" rules={[{ required: true }]}>
            <Select disabled={editor?.mode === "edit"} onChange={(next: StorageType) => form.setFieldsValue({ region: next === "aliyun_oss" ? "cn-hangzhou" : "us-east-1", endpoint: next === "aliyun_oss" ? "https://oss-cn-hangzhou.aliyuncs.com" : "" })} options={[{ value: "local", label: "本地磁盘" }, { value: "s3", label: "S3 / MinIO" }, { value: "aliyun_oss", label: "Alibaba Cloud OSS" }]} />
          </Form.Item>
          {selectedType === "local" ? (
            <Form.Item name="rootPath" label="绝对根目录" extra="Backend 进程必须具有该目录的读写权限。" rules={[{ required: true, pattern: /^\//, message: "请输入绝对路径" }]}><Input placeholder="/data/altasci-storage" /></Form.Item>
          ) : (
            <>
              <Space align="start" className="full-width form-space-equal">
                <Form.Item name="region" label="Region" rules={[{ required: true, whitespace: true }]}><Input placeholder={selectedType === "s3" ? "us-east-1" : "cn-hangzhou"} /></Form.Item>
                <Form.Item name="bucket" label="Bucket" rules={[{ required: true, whitespace: true }]}><Input /></Form.Item>
              </Space>
              <Form.Item name="endpoint" label="Endpoint" extra={selectedType === "s3" ? "AWS S3 可留空；MinIO 等兼容存储填写 HTTPS Endpoint。" : "OSS Endpoint 或自定义 CNAME。"}><Input placeholder={selectedType === "s3" ? "https://s3.example.com" : "https://oss-cn-hangzhou.aliyuncs.com"} /></Form.Item>
              <Form.Item name="prefix" label="对象 Key 前缀"><Input placeholder="network-storage/" /></Form.Item>
              <Space align="start" className="full-width form-space-equal">
                <Form.Item name="accessKeyID" label="Access Key ID" rules={editor?.mode === "create" ? [{ required: true, whitespace: true }] : []}><Input autoComplete="off" placeholder={editor?.mode === "edit" ? "留空保留现有凭据" : undefined} /></Form.Item>
                <Form.Item name="secretAccessKey" label={selectedType === "s3" ? "Secret Access Key" : "Access Key Secret"} rules={editor?.mode === "create" ? [{ required: true, message: "请输入 Secret" }] : []}><Input.Password autoComplete="new-password" placeholder={editor?.mode === "edit" ? "留空保留现有凭据" : undefined} /></Form.Item>
              </Space>
              {selectedType === "s3" && <Form.Item name="forcePathStyle" label="地址风格" valuePropName="checked"><Switch checkedChildren="Path Style" unCheckedChildren="Virtual Host" /></Form.Item>}
              {selectedType === "aliyun_oss" && <Form.Item name="useCName" label="自定义域名" valuePropName="checked"><Switch checkedChildren="使用 CNAME" unCheckedChildren="标准 Endpoint" /></Form.Item>}
            </>
          )}
        </Form>
      </Modal>
    </section>
  );
}
