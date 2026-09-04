import { useQueryClient } from "@tanstack/react-query";
import { CloseOutlined, CloudUploadOutlined, ReloadOutlined, StopOutlined } from "@ant-design/icons";
import { App, Badge, Button, Drawer, Progress, Space, Tag, Typography } from "antd";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type PropsWithChildren } from "react";
import { api, APIError } from "../api/client";

type UploadState = "waiting" | "uploading" | "retrying" | "completing" | "completed" | "failed" | "cancelled";
type UploadItem = {
  id: string;
  uploadId?: string;
  projectId: string;
  parentId?: string;
  file: File;
  status: UploadState;
  uploaded: number;
  speed: number;
  error?: string;
};
type UploadContextValue = { items: UploadItem[]; enqueue(files: FileList | File[], projectId: string, parentId?: string): void; cancel(id: string): void; retry(id: string): void; dismiss(id: string): void };
const UploadContext = createContext<UploadContextValue | null>(null);

type CreateResult = {
  upload_id: string;
  upload_type: "local" | "single" | "multipart";
  url?: string;
  method?: string;
  headers?: Record<string, string>;
  part_size?: number;
  part_count?: number;
};

export function UploadProvider({ children }: PropsWithChildren) {
  const [items, setItems] = useState<UploadItem[]>([]);
  const controllers = useRef(new Map<string, AbortController>());
  const queryClient = useQueryClient();
  const { modal } = App.useApp();

  const update = useCallback((id: string, patch: Partial<UploadItem>) => {
    setItems((current) => current.map((item) => item.id === id ? { ...item, ...patch } : item));
  }, []);

  const run = useCallback(async (item: UploadItem) => {
    const controller = new AbortController();
    controllers.current.set(item.id, controller);
    const started = performance.now();
    try {
      update(item.id, { status: "uploading", error: undefined, uploaded: 0 });
      const createSession = (overwrite: boolean) => api.request<CreateResult>(`/api/v1/projects/${item.projectId}/uploads`, {
          method: "POST",
          body: JSON.stringify({ parent_id: item.parentId ?? "", filename: item.file.name, size: item.file.size, mime_type: item.file.type || "application/octet-stream", overwrite }),
          signal: controller.signal,
        });
      let session: CreateResult;
      try { session = await createSession(false); }
      catch (error) {
        if (!(error instanceof APIError) || error.status !== 409 || !await confirmOverwrite(modal, item.file.name)) throw error;
        session = await createSession(true);
      }
      update(item.id, { uploadId: session.upload_id });
      if (session.upload_type === "local" || session.upload_type === "single") {
		const headers = session.upload_type === "local"
			? { "Content-Type": item.file.type || "application/octet-stream", "X-CSRF-Token": await currentCSRF() }
			: session.headers ?? {};
		await uploadBlob(session.url!, session.method ?? "PUT", headers, item.file, session.upload_type === "local", controller.signal, (uploaded) => update(item.id, { uploaded, speed: bytesPerSecond(uploaded, started) }));
        update(item.id, { uploaded: item.file.size, speed: bytesPerSecond(item.file.size, started), status: "completing" });
        await api.request(`/api/v1/uploads/${session.upload_id}/complete`, { method: "POST", body: JSON.stringify({ parts: [] }), signal: controller.signal });
      } else {
        await uploadMultipart(item, session, controller.signal, (uploaded, retrying) => update(item.id, { uploaded, speed: bytesPerSecond(uploaded, started), status: retrying ? "retrying" : "uploading" }));
        update(item.id, { status: "completing", uploaded: item.file.size });
      }
      update(item.id, { status: "completed", uploaded: item.file.size, speed: bytesPerSecond(item.file.size, started) });
      await queryClient.invalidateQueries({ queryKey: ["nodes", item.projectId] });
    } catch (error) {
      if (controller.signal.aborted) update(item.id, { status: "cancelled" });
      else update(item.id, { status: "failed", error: error instanceof Error ? error.message : "上传失败" });
    } finally {
      controllers.current.delete(item.id);
    }
  }, [modal, queryClient, update]);

  const value = useMemo<UploadContextValue>(() => ({
    items,
    enqueue(files, projectId, parentId) {
      const added = Array.from(files).map((file) => ({ id: crypto.randomUUID(), projectId, parentId, file, status: "waiting" as const, uploaded: 0, speed: 0 }));
      setItems((current) => [...current, ...added]);
      for (const item of added) void run(item);
    },
    cancel(id) {
      const item = items.find((entry) => entry.id === id);
      controllers.current.get(id)?.abort();
      if (item?.uploadId) void api.request(`/api/v1/uploads/${item.uploadId}`, { method: "DELETE" }).catch(() => undefined);
      update(id, { status: "cancelled" });
    },
    retry(id) {
      const item = items.find((entry) => entry.id === id);
      if (item) void run({ ...item, uploadId: undefined, status: "waiting", uploaded: 0, speed: 0 });
    },
    dismiss(id) {
      setItems((current) => current.filter((item) => item.id !== id));
    },
  }), [items, run, update]);

  return <UploadContext.Provider value={value}>{children}<UploadDrawer /></UploadContext.Provider>;
}

function confirmOverwrite(modal: ReturnType<typeof App.useApp>["modal"], filename: string) {
  return new Promise<boolean>((resolve) => {
    modal.confirm({
      title: "文件已存在",
      content: `是否用新文件覆盖 ${filename}？旧对象将在上传完成后异步删除。`,
      okText: "覆盖",
      cancelText: "取消",
      okButtonProps: { danger: true },
      onOk: () => resolve(true),
      onCancel: () => resolve(false),
    });
  });
}

function uploadBlob(url: string, method: string, headers: Record<string, string>, body: Blob, withCredentials: boolean, signal: AbortSignal, progress: (uploaded: number) => void) {
	return new Promise<void>((resolve, reject) => {
		const request = new XMLHttpRequest();
		request.open(method, url);
		request.withCredentials = withCredentials;
		for (const [key, value] of Object.entries(headers)) request.setRequestHeader(key, value);
		request.upload.onprogress = (event) => progress(event.loaded);
		request.onerror = () => reject(new Error("上传数据时网络连接失败；请联系管理员检查对象存储的 CORS、Endpoint 和 HTTPS 配置"));
		request.onabort = () => reject(new DOMException("Upload cancelled", "AbortError"));
		request.onload = () => request.status >= 200 && request.status < 300 ? resolve() : reject(new Error(`上传数据失败 (${request.status})`));
		const abort = () => request.abort();
		signal.addEventListener("abort", abort, { once: true });
		request.onloadend = () => signal.removeEventListener("abort", abort);
		request.send(body);
	});
}

async function currentCSRF() {
	return api.ensureCSRF();
}

async function uploadMultipart(item: UploadItem, session: CreateResult, signal: AbortSignal, progress: (uploaded: number, retrying: boolean) => void) {
  const partSize = session.part_size!;
  const count = session.part_count!;
  const numbers = Array.from({ length: count }, (_, index) => index + 1);
  let uploaded = 0;
  const completed: Array<{ part_number: number; etag: string }> = [];
  // Presign and consume at most 100 parts at a time. This keeps thousands of
  // short-lived signed URLs from being minted up front for very large files.
  for (let offset = 0; offset < numbers.length; offset += 100) {
    const result = await api.request<{ parts: Array<{ part_number: number; url: string; method: string; headers: Record<string, string> }> }>(`/api/v1/uploads/${session.upload_id}/parts/presign`, {
      method: "POST", body: JSON.stringify({ part_numbers: numbers.slice(offset, offset + 100) }), signal,
    });
    let cursor = 0;
    async function worker() {
      while (cursor < result.parts.length) {
      const target = result.parts[cursor++];
      const number = target.part_number;
      const index = number - 1;
      const blob = item.file.slice(index * partSize, Math.min(item.file.size, (index + 1) * partSize));
      let response: Response | undefined;
      for (let attempt = 0; attempt < 3; attempt++) {
        if (attempt) progress(uploaded, true);
        try { response = await fetch(target.url, { method: target.method, headers: target.headers, body: blob, signal }); }
        catch (error) { if (signal.aborted || attempt === 2) throw error; }
		if (response?.ok) break;
      }
      if (!response?.ok) throw new Error(`分片 ${number} 上传失败`);
      const etag = response.headers.get("ETag");
      if (!etag) throw new Error("对象存储未暴露 ETag，请检查 Bucket CORS");
      completed.push({ part_number: number, etag });
      uploaded += blob.size;
      progress(uploaded, false);
      }
    }
    await Promise.all(Array.from({ length: Math.min(4, result.parts.length) }, () => worker()));
  }
  completed.sort((a, b) => a.part_number - b.part_number);
  await api.request(`/api/v1/uploads/${session.upload_id}/complete`, { method: "POST", body: JSON.stringify({ parts: completed }), signal });
}

function bytesPerSecond(bytes: number, started: number) { return bytes / Math.max(0.001, (performance.now() - started) / 1000); }
function formatBytes(value: number) { if (value < 1024) return `${value} B`; if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KiB`; if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} MiB`; return `${(value / 1024 ** 3).toFixed(1)} GiB`; }

function UploadDrawer() {
  const context = useContext(UploadContext);
  const [open, setOpen] = useState(false);
  const previousCount = useRef(0);
  useEffect(() => {
    if (context && context.items.length > previousCount.current) setOpen(true);
    previousCount.current = context?.items.length ?? 0;
  }, [context]);
  if (!context || context.items.length === 0) return null;
  const active = context.items.filter((item) => !["completed", "failed", "cancelled"].includes(item.status)).length;
  return <>
    {!open && <Badge count={active} size="small" className="upload-trigger-badge"><Button type="primary" shape="circle" size="large" icon={<CloudUploadOutlined />} aria-label="打开上传管理器" onClick={() => setOpen(true)} /></Badge>}
    <Drawer title={<Space><CloudUploadOutlined />上传管理器 {active > 0 && <Tag color="processing">{active} 个进行中</Tag>}</Space>} open={open} width={440} onClose={() => setOpen(false)}>
      <Space direction="vertical" size={12} className="full-width">
        {context.items.slice().reverse().map((item) => {
          const terminal = ["completed", "failed", "cancelled"].includes(item.status);
          const percent = item.file.size === 0 ? 100 : Math.min(100, Math.round(item.uploaded / item.file.size * 100));
          return <div className="upload-enterprise-row" key={item.id}>
            <div className="upload-row-heading"><div><Typography.Text strong ellipsis={{ tooltip: item.file.name }}>{item.file.name}</Typography.Text><Typography.Text type="secondary" className="table-subtitle">{formatBytes(item.uploaded)} / {formatBytes(item.file.size)}{item.speed ? ` · ${formatBytes(item.speed)}/s` : ""}</Typography.Text></div><UploadStatus status={item.status} /></div>
            <Progress percent={percent} size="small" status={item.status === "failed" ? "exception" : item.status === "completed" ? "success" : item.status === "cancelled" ? "normal" : "active"} />
            {item.error && <Typography.Text type="danger" className="upload-error">{item.error}</Typography.Text>}
            <Space size="small">
              {item.status === "failed" && <Button size="small" icon={<ReloadOutlined />} onClick={() => context.retry(item.id)}>重试</Button>}
              {!terminal && <Button size="small" danger icon={<StopOutlined />} onClick={() => context.cancel(item.id)}>取消</Button>}
              {terminal && <Button size="small" icon={<CloseOutlined />} onClick={() => context.dismiss(item.id)}>移除</Button>}
            </Space>
          </div>;
        })}
      </Space>
    </Drawer>
  </>;
}

function UploadStatus({ status }: { status: UploadState }) {
  const labels: Record<UploadState, string> = { waiting: "等待中", uploading: "上传中", retrying: "重试中", completing: "正在完成", completed: "已完成", failed: "失败", cancelled: "已取消" };
  const color = status === "completed" ? "success" : status === "failed" ? "error" : status === "cancelled" ? "default" : "processing";
  return <Tag color={color}>{labels[status]}</Tag>;
}

export function useUploads() {
  const value = useContext(UploadContext);
  if (!value) throw new Error("UploadProvider is missing");
  return value;
}
