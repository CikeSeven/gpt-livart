import { authHeaders } from './auth';

export const joinUrl = (baseUrl: string, path: string) => {
  if (!baseUrl) return '';
  return `${baseUrl.replace(/\/+$/, '')}/${path.replace(/^\/+/, '')}`;
};

const normalizeBaseUrl = (baseUrl: string | undefined) => (baseUrl || '').trim().replace(/\/+$/, '');

const TEXT_TO_IMAGE_PATH = 'images/generations';
const IMAGE_TO_IMAGE_PATH = 'images/edits';

export const AVAILABLE_MODELS = [
  'gpt-image-2'
];

export const AVAILABLE_CHAT_MODELS = [
  'gpt-5.5',
  'gpt-5.4'
];

export interface ApiConfig {
  baseUrl: string;
  textToImageUrl: string;
  imageToImageUrl: string;
  apiKey: string;
  model: string;
  chatModel: string;
  imageModels: string[];
  chatModels: string[];
  serverDefault?: boolean;
}

interface ApiResponse<T> {
  success: boolean;
  data?: T | null;
  error?: {
    message: string;
    code: string;
  };
}

export const buildImageApiUrls = (baseUrl: string) => {
  const normalizedBaseUrl = normalizeBaseUrl(baseUrl);
  return {
    textToImageUrl: joinUrl(normalizedBaseUrl, TEXT_TO_IMAGE_PATH),
    imageToImageUrl: joinUrl(normalizedBaseUrl, IMAGE_TO_IMAGE_PATH)
  };
};

const defaultBaseUrl = normalizeBaseUrl(process.env.IMAGE_API_BASE_URL || '');
const defaultImageModel = process.env.IMAGE_API_MODEL || 'gpt-image-2';
const defaultChatModel = process.env.PROMPT_OPTIMIZER_MODEL || process.env.CHAT_API_MODEL || 'gpt-5.5';

const normalizeModelList = (models: string[] | undefined, fallbackModels: string[]) => {
  const seen = new Set<string>();
  const normalized = (models || [])
    .map(model => model.trim())
    .filter(model => {
      if (!model || seen.has(model)) return false;
      seen.add(model);
      return true;
    });
  return normalized.length > 0 ? normalized : fallbackModels;
};

export const DEFAULT_API_CONFIG: ApiConfig = {
  baseUrl: defaultBaseUrl,
  ...buildImageApiUrls(defaultBaseUrl),
  apiKey: '',
  model: defaultImageModel,
  chatModel: defaultChatModel,
  imageModels: normalizeModelList([defaultImageModel, ...AVAILABLE_MODELS], AVAILABLE_MODELS),
  chatModels: normalizeModelList([defaultChatModel, ...AVAILABLE_CHAT_MODELS], AVAILABLE_CHAT_MODELS),
  serverDefault: false
};

let currentApiConfig = DEFAULT_API_CONFIG;
let hasLoadedUserApiConfig = false;

export const normalizeApiConfig = (config: Partial<ApiConfig>): ApiConfig => {
  const baseUrl = normalizeBaseUrl(config.baseUrl || DEFAULT_API_CONFIG.baseUrl);
  const imageApiUrls = buildImageApiUrls(baseUrl);
  const imageModels = normalizeModelList(config.imageModels, AVAILABLE_MODELS);
  const chatModels = normalizeModelList(config.chatModels, AVAILABLE_CHAT_MODELS);
  const requestedImageModel = (config.model || DEFAULT_API_CONFIG.model).trim();
  const requestedChatModel = (config.chatModel || DEFAULT_API_CONFIG.chatModel).trim();
  const imageModel = imageModels.includes(requestedImageModel) ? requestedImageModel : imageModels[0];
  const chatModel = chatModels.includes(requestedChatModel) ? requestedChatModel : chatModels[0];

  return {
    baseUrl,
    ...imageApiUrls,
    apiKey: (config.apiKey || '').trim(),
    model: imageModel,
    chatModel,
    imageModels,
    chatModels,
    serverDefault: !!config.serverDefault
  };
};

const unwrapApiResponse = async <T>(response: Response): Promise<T | null> => {
  const payload = await response.json().catch(() => null) as ApiResponse<T> | null;
  if (!response.ok || !payload?.success) {
    throw new Error(payload?.error?.message || `用户配置请求失败：${response.status}`);
  }
  return payload.data ?? null;
};

export const getApiConfig = (): ApiConfig => currentApiConfig;

export const loadApiConfig = async (): Promise<ApiConfig | null> => {
  const response = await fetch('/api/user/config', {
    headers: {
      Accept: 'application/json',
      ...authHeaders()
    }
  });
  const config = await unwrapApiResponse<ApiConfig>(response);
  if (!config) {
    currentApiConfig = normalizeApiConfig(DEFAULT_API_CONFIG);
    hasLoadedUserApiConfig = false;
    return null;
  }
  currentApiConfig = normalizeApiConfig(config);
  hasLoadedUserApiConfig = true;
  return currentApiConfig;
};

export const saveApiConfig = async (config: ApiConfig): Promise<ApiConfig> => {
  const normalizedConfig = normalizeApiConfig(config);
  const response = await fetch('/api/user/config', {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
      ...authHeaders()
    },
    body: JSON.stringify({
      baseUrl: normalizedConfig.baseUrl,
      apiKey: normalizedConfig.apiKey,
      model: normalizedConfig.model,
      chatModel: normalizedConfig.chatModel,
      imageModels: normalizedConfig.imageModels,
      chatModels: normalizedConfig.chatModels
    })
  });
  const savedConfig = await unwrapApiResponse<ApiConfig>(response);
  currentApiConfig = normalizeApiConfig(savedConfig || normalizedConfig);
  hasLoadedUserApiConfig = true;
  return currentApiConfig;
};

export const hasApiConfig = (): boolean => {
  return !!(
    hasLoadedUserApiConfig &&
    currentApiConfig.baseUrl &&
    (currentApiConfig.apiKey || currentApiConfig.serverDefault) &&
    currentApiConfig.model &&
    currentApiConfig.chatModel
  );
};

export const resetApiConfigSession = () => {
  currentApiConfig = normalizeApiConfig(DEFAULT_API_CONFIG);
  hasLoadedUserApiConfig = false;
};
