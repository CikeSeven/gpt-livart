import { authHeaders } from './auth';
import type { ApiConfig } from './config';

interface ApiResponse<T> {
  success: boolean;
  data?: T;
  error?: {
    message: string;
    code: string;
  };
}

export interface AdminSummary {
  userCount: number;
  adminCount: number;
  canvasCount: number;
  assetCount: number;
}

export interface AdminUser {
  id: string;
  username: string;
  displayName: string;
  isAdmin?: boolean;
  createdAt?: string;
}

export interface AdminCanvas {
  id: string;
  title: string;
  userId: string;
  username: string;
  createdAt?: string;
  updatedAt?: string;
  revision: number;
}

export interface AdminAsset {
  id: string;
  canvasId?: string;
  userId?: string;
  urlPath: string;
  originalFilename?: string;
  mimeType: string;
  sizeBytes: number;
  createdAt?: string;
}

export type AdminApiConfig = ApiConfig;

const unwrapApiResponse = async <T>(response: Response): Promise<T> => {
  const payload = await response.json().catch(() => null) as ApiResponse<T> | null;
  if (!response.ok || !payload?.success || payload.data === undefined) {
    throw new Error(payload?.error?.message || `后台管理请求失败：${response.status}`);
  }
  return payload.data;
};

const getAdminResource = async <T>(path: string) => {
  const response = await fetch(path, {
    headers: {
      Accept: 'application/json',
      ...authHeaders()
    }
  });
  return unwrapApiResponse<T>(response);
};

export const loadAdminApiConfig = async () => {
  return getAdminResource<AdminApiConfig | null>('/api/admin/config');
};

export const saveAdminApiConfig = async (config: Pick<ApiConfig, 'baseUrl' | 'apiKey' | 'model' | 'chatModel'>) => {
  const response = await fetch('/api/admin/config', {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
      ...authHeaders()
    },
    body: JSON.stringify(config)
  });
  return unwrapApiResponse<AdminApiConfig>(response);
};

export const loadAdminDashboard = async () => {
  const [summary, users, canvases, assets] = await Promise.all([
    getAdminResource<AdminSummary>('/api/admin/summary'),
    getAdminResource<AdminUser[]>('/api/admin/users'),
    getAdminResource<AdminCanvas[]>('/api/admin/canvases'),
    getAdminResource<AdminAsset[]>('/api/admin/assets')
  ]);
  return { summary, users, canvases, assets };
};
