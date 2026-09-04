export type User = {
  id: string;
  email: string;
  role: "admin" | "user";
  write_enabled: boolean;
  status: "active" | "disabled";
  created_at: string;
  updated_at: string;
};

export type Project = {
  id: string;
  name: string;
  description: string;
  storage_backend_id: string;
  permission: "admin" | "read" | "write";
  status: string;
  created_at: string;
  updated_at: string;
};

export type Node = {
  id: string;
  project_id: string;
  parent_id: string | null;
  node_type: "file" | "directory";
  name: string;
  size: number;
  mime_type: string;
  created_at: string;
  updated_at: string;
};

export type StorageBackend = {
  id: string;
  name: string;
  type: "local" | "s3" | "aliyun_oss";
  enabled: boolean;
  config: Record<string, unknown>;
  has_secret: boolean;
  project_count?: number;
  blob_count?: number;
  last_test_status: string | null;
  last_test_message: string | null;
};

export type Share = {
  id: string;
  project_id: string;
  target_node_id: string;
  require_code: boolean;
  code_length?: 4 | 8;
  expires_at: string | null;
  disabled_at: string | null;
  created_at: string;
  url?: string;
  code?: string;
};

export type APIErrorBody = { error: { code: string; message: string; request_id: string } };
