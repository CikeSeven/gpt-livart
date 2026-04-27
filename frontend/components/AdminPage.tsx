import React, { useEffect, useState } from 'react';
import { BarChart3, Database, Eye, EyeOff, FileImage, Home, Loader2, LogOut, Save, Shield, Users } from 'lucide-react';
import type { AuthSession } from '../services/auth';
import { AdminAsset, AdminCanvas, AdminSummary, AdminUser, loadAdminApiConfig, loadAdminDashboard, saveAdminApiConfig } from '../services/admin';
import { AVAILABLE_CHAT_MODELS, AVAILABLE_MODELS, buildImageApiUrls, joinUrl, normalizeApiConfig, type ApiConfig } from '../services/config';

interface AdminPageProps {
  session: AuthSession;
  onLogout: () => void;
}

const formatDate = (value?: string) => {
  if (!value) return '-';
  return new Date(value).toLocaleString();
};

const formatBytes = (value: number) => {
  if (!Number.isFinite(value) || value <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  let size = value;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  return `${size.toFixed(unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
};

const formatModelList = (models: string[]) => models.join('\n');

const parseModelList = (value: string) => {
  const seen = new Set<string>();
  return value
    .split(/[\n,]/)
    .map(model => model.trim())
    .filter(model => {
      if (!model || seen.has(model)) return false;
      seen.add(model);
      return true;
    });
};

const nextSelectedModel = (current: string, models: string[], fallback: string) => (
  models.includes(current) ? current : (models[0] || fallback)
);

const StatCard: React.FC<{ label: string; value: number; icon: React.ReactNode; tone: string }> = ({ label, value, icon, tone }) => (
  <div className="rounded-3xl border border-white/70 bg-white p-5 shadow-[0_24px_80px_-48px_rgba(15,23,42,0.55)]">
    <div className="flex items-center justify-between">
      <div>
        <p className="text-xs font-black uppercase tracking-[0.22em] text-gray-400">{label}</p>
        <p className="mt-3 text-4xl font-black tracking-tighter text-gray-950">{value}</p>
      </div>
      <div className={`flex h-12 w-12 items-center justify-center rounded-2xl ${tone}`}>
        {icon}
      </div>
    </div>
  </div>
);

const AdminPage: React.FC<AdminPageProps> = ({ session, onLogout }) => {
  const [summary, setSummary] = useState<AdminSummary | null>(null);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [canvases, setCanvases] = useState<AdminCanvas[]>([]);
  const [assets, setAssets] = useState<AdminAsset[]>([]);
  const [apiConfig, setApiConfig] = useState<ApiConfig>({
    baseUrl: '',
    textToImageUrl: '',
    imageToImageUrl: '',
    apiKey: '',
    model: 'gpt-image-2',
    chatModel: 'gpt-5.5',
    imageModels: AVAILABLE_MODELS,
    chatModels: AVAILABLE_CHAT_MODELS,
    serverDefault: true
  });
  const [showApiKey, setShowApiKey] = useState(false);
  const [isSavingConfig, setIsSavingConfig] = useState(false);
  const [configMessage, setConfigMessage] = useState('');
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let isMounted = true;
    setIsLoading(true);
    setError('');
    Promise.all([loadAdminDashboard(), loadAdminApiConfig()])
      .then(([dashboard, config]) => {
        if (!isMounted) return;
        setSummary(dashboard.summary);
        setUsers(dashboard.users);
        setCanvases(dashboard.canvases);
        setAssets(dashboard.assets);
        if (config) {
          setApiConfig(normalizeApiConfig(config));
        }
      })
      .catch((loadError) => {
        if (!isMounted) return;
        setError(loadError instanceof Error ? loadError.message : '后台数据加载失败');
      })
      .finally(() => {
        if (isMounted) setIsLoading(false);
      });
    return () => {
      isMounted = false;
    };
  }, []);

  const imageApiUrls = buildImageApiUrls(apiConfig.baseUrl);
  const chatResponsesUrl = joinUrl(apiConfig.baseUrl, 'responses');

  const handleSaveApiConfig = async () => {
    const normalizedConfig = normalizeApiConfig(apiConfig);
    if (!normalizedConfig.baseUrl || !normalizedConfig.apiKey || !normalizedConfig.model || !normalizedConfig.chatModel) {
      setConfigMessage('请填写 Base URL、API Key、生图模型和对话模型。');
      return;
    }

    setIsSavingConfig(true);
    setConfigMessage('');
    try {
      const saved = await saveAdminApiConfig({
        baseUrl: normalizedConfig.baseUrl,
        apiKey: normalizedConfig.apiKey,
        model: normalizedConfig.model,
        chatModel: normalizedConfig.chatModel,
        imageModels: normalizedConfig.imageModels,
        chatModels: normalizedConfig.chatModels
      });
      setApiConfig(normalizeApiConfig(saved));
      setConfigMessage('全站中转站配置已保存，普通用户会自动使用该配置。');
    } catch (saveError) {
      setConfigMessage(saveError instanceof Error ? saveError.message : '保存全站配置失败');
    } finally {
      setIsSavingConfig(false);
    }
  };

  if (!session.user.isAdmin) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-[#f5f5f0] px-6 text-gray-950">
        <div className="max-w-md rounded-[36px] border border-white bg-white p-8 text-center shadow-2xl">
          <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-red-50 text-red-500">
            <Shield size={24} />
          </div>
          <h1 className="mt-5 text-2xl font-black tracking-tighter">没有后台权限</h1>
          <p className="mt-3 text-sm font-bold leading-6 text-gray-500">当前账号没有访问管理后台的权限。请切换到管理员账号后重试。</p>
          <div className="mt-6 flex gap-3">
            <a href="/" className="flex-1 rounded-2xl bg-black px-4 py-3 text-sm font-black text-white">返回画布</a>
            <button onClick={onLogout} className="flex-1 rounded-2xl border border-gray-200 px-4 py-3 text-sm font-black text-gray-700">退出登录</button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-[#f5f5f0] px-6 py-6 text-gray-950">
      <div className="mx-auto max-w-7xl">
        <header className="flex flex-wrap items-center justify-between gap-4 rounded-[32px] border border-white bg-white/80 px-5 py-4 shadow-[0_24px_80px_-52px_rgba(15,23,42,0.55)] backdrop-blur-2xl">
          <div className="flex items-center gap-4">
            <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-black text-white">
              <Shield size={22} />
            </div>
            <div>
              <h1 className="text-2xl font-black tracking-tighter">livart 管理后台</h1>
              <p className="text-xs font-bold text-gray-400">当前管理员：{session.user.displayName || session.user.username}</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <a href="/" className="flex h-10 items-center gap-2 rounded-2xl bg-gray-100 px-4 text-xs font-black text-gray-700 transition hover:bg-gray-200">
              <Home size={15} /> 返回画布
            </a>
            <button onClick={onLogout} className="flex h-10 items-center gap-2 rounded-2xl bg-black px-4 text-xs font-black text-white transition hover:opacity-90">
              <LogOut size={15} /> 退出
            </button>
          </div>
        </header>

        {isLoading && (
          <div className="mt-8 flex items-center gap-3 rounded-3xl bg-white p-6 text-sm font-black text-gray-500">
            <Loader2 className="animate-spin" size={18} /> 正在加载后台数据...
          </div>
        )}

        {error && (
          <div className="mt-8 rounded-3xl border border-red-100 bg-red-50 p-5 text-sm font-black text-red-600">
            {error}
          </div>
        )}

        {summary && !isLoading && (
          <>
            <section className="mt-8 grid gap-4 md:grid-cols-4">
              <StatCard label="用户" value={summary.userCount} icon={<Users size={22} />} tone="bg-indigo-50 text-indigo-600" />
              <StatCard label="管理员" value={summary.adminCount} icon={<Shield size={22} />} tone="bg-emerald-50 text-emerald-600" />
              <StatCard label="画布" value={summary.canvasCount} icon={<BarChart3 size={22} />} tone="bg-amber-50 text-amber-600" />
              <StatCard label="资源" value={summary.assetCount} icon={<FileImage size={22} />} tone="bg-sky-50 text-sky-600" />
            </section>

            <section className="mt-8 rounded-[32px] bg-white p-5 shadow-[0_24px_80px_-52px_rgba(15,23,42,0.55)]">
              <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
                <div>
                  <h2 className="flex items-center gap-2 text-lg font-black tracking-tight"><Shield size={18} /> 全站中转站配置</h2>
                  <p className="mt-1 text-xs font-bold text-gray-400">普通用户不再填写 API Key，所有生图请求统一使用这里的公益站配置。</p>
                </div>
                <button
                  onClick={handleSaveApiConfig}
                  disabled={isSavingConfig}
                  className="flex h-10 items-center gap-2 rounded-2xl bg-black px-4 text-xs font-black text-white transition hover:opacity-90 disabled:opacity-40"
                >
                  {isSavingConfig ? <Loader2 size={15} className="animate-spin" /> : <Save size={15} />}
                  {isSavingConfig ? '保存中' : '保存配置'}
                </button>
              </div>

              <div className="grid gap-4 lg:grid-cols-2">
                <div>
                  <label className="mb-2 block text-sm font-black text-gray-700">中转站 Base URL</label>
                  <input
                    value={apiConfig.baseUrl}
                    onChange={(event) => setApiConfig(prev => ({ ...prev, baseUrl: event.target.value }))}
                    placeholder="https://your-openai-compatible-endpoint.example/v1/"
                    className="w-full rounded-2xl border border-gray-200 px-4 py-3 text-sm font-bold outline-none transition-all focus:border-gray-300 focus:ring-4 focus:ring-black/5"
                  />
                </div>
                <div>
                  <label className="mb-2 block text-sm font-black text-gray-700">API Key</label>
                  <div className="relative">
                    <input
                      type={showApiKey ? 'text' : 'password'}
                      value={apiConfig.apiKey}
                      onChange={(event) => setApiConfig(prev => ({ ...prev, apiKey: event.target.value }))}
                      placeholder="sk-..."
                      className="w-full rounded-2xl border border-gray-200 px-4 py-3 pr-12 text-sm font-bold outline-none transition-all focus:border-gray-300 focus:ring-4 focus:ring-black/5"
                    />
                    <button
                      type="button"
                      onClick={() => setShowApiKey(prev => !prev)}
                      className="absolute right-3 top-1/2 -translate-y-1/2 rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-100 hover:text-gray-700"
                    >
                      {showApiKey ? <EyeOff size={17} /> : <Eye size={17} />}
                    </button>
                  </div>
                </div>
                <div>
                  <label className="mb-2 block text-sm font-black text-gray-700">生图模型</label>
                  <select
                    value={apiConfig.model}
                    onChange={(event) => setApiConfig(prev => ({ ...prev, model: event.target.value }))}
                    className="w-full rounded-2xl border border-gray-200 bg-white px-4 py-3 text-sm font-bold outline-none transition-all focus:border-gray-300 focus:ring-4 focus:ring-black/5"
                  >
                    {apiConfig.imageModels.map(model => <option key={model} value={model}>{model}</option>)}
                  </select>
                </div>
                <div>
                  <label className="mb-2 block text-sm font-black text-gray-700">对话模型</label>
                  <select
                    value={apiConfig.chatModel}
                    onChange={(event) => setApiConfig(prev => ({ ...prev, chatModel: event.target.value }))}
                    className="w-full rounded-2xl border border-gray-200 bg-white px-4 py-3 text-sm font-bold outline-none transition-all focus:border-gray-300 focus:ring-4 focus:ring-black/5"
                  >
                    {apiConfig.chatModels.map(model => <option key={model} value={model}>{model}</option>)}
                  </select>
                </div>
                <div>
                  <label className="mb-2 block text-sm font-black text-gray-700">生图模型列表</label>
                  <textarea
                    value={formatModelList(apiConfig.imageModels)}
                    onChange={(event) => {
                      const imageModels = parseModelList(event.target.value);
                      setApiConfig(prev => ({
                        ...prev,
                        imageModels,
                        model: nextSelectedModel(prev.model, imageModels, AVAILABLE_MODELS[0])
                      }));
                    }}
                    rows={4}
                    placeholder="每行一个模型，例如：gpt-image-2"
                    className="w-full rounded-2xl border border-gray-200 px-4 py-3 text-sm font-bold outline-none transition-all focus:border-gray-300 focus:ring-4 focus:ring-black/5"
                  />
                </div>
                <div>
                  <label className="mb-2 block text-sm font-black text-gray-700">对话模型列表</label>
                  <textarea
                    value={formatModelList(apiConfig.chatModels)}
                    onChange={(event) => {
                      const chatModels = parseModelList(event.target.value);
                      setApiConfig(prev => ({
                        ...prev,
                        chatModels,
                        chatModel: nextSelectedModel(prev.chatModel, chatModels, AVAILABLE_CHAT_MODELS[0])
                      }));
                    }}
                    rows={4}
                    placeholder="每行一个模型，例如：gpt-5.5"
                    className="w-full rounded-2xl border border-gray-200 px-4 py-3 text-sm font-bold outline-none transition-all focus:border-gray-300 focus:ring-4 focus:ring-black/5"
                  />
                </div>
              </div>

              <div className="mt-4 rounded-2xl border border-gray-100 bg-gray-50 p-4 text-xs font-bold text-gray-500">
                <div className="font-black text-gray-700">自动拼接地址</div>
                <div className="mt-2 break-all">文生图：{imageApiUrls.textToImageUrl || '填写 Base URL 后自动生成'}</div>
                <div className="mt-1 break-all">图生图：{imageApiUrls.imageToImageUrl || '填写 Base URL 后自动生成'}</div>
                <div className="mt-1 break-all">对话：{chatResponsesUrl || '填写 Base URL 后自动生成'}</div>
              </div>

              {configMessage && (
                <div className={`mt-4 rounded-2xl px-4 py-3 text-sm font-black ${configMessage.includes('已保存') ? 'bg-emerald-50 text-emerald-600' : 'bg-red-50 text-red-600'}`}>
                  {configMessage}
                </div>
              )}
            </section>

            <section className="mt-8 grid gap-6 lg:grid-cols-2">
              <div className="rounded-[32px] bg-white p-5 shadow-[0_24px_80px_-52px_rgba(15,23,42,0.55)]">
                <h2 className="mb-4 flex items-center gap-2 text-lg font-black tracking-tight"><Users size={18} /> 用户</h2>
                <div className="space-y-2">
                  {users.map(user => (
                    <div key={user.id} className="rounded-2xl bg-gray-50 px-4 py-3">
                      <div className="flex items-center justify-between gap-3">
                        <div>
                          <p className="text-sm font-black text-gray-900">{user.displayName || user.username}</p>
                          <p className="text-xs font-bold text-gray-400">@{user.username} · {formatDate(user.createdAt)}</p>
                        </div>
                        {user.isAdmin && <span className="rounded-full bg-black px-3 py-1 text-[10px] font-black uppercase tracking-widest text-white">admin</span>}
                      </div>
                    </div>
                  ))}
                </div>
              </div>

              <div className="rounded-[32px] bg-white p-5 shadow-[0_24px_80px_-52px_rgba(15,23,42,0.55)]">
                <h2 className="mb-4 flex items-center gap-2 text-lg font-black tracking-tight"><Database size={18} /> 项目画布</h2>
                <div className="space-y-2">
                  {canvases.length === 0 && <p className="text-sm font-bold text-gray-400">暂无画布项目</p>}
                  {canvases.slice(0, 12).map(canvas => (
                    <div key={canvas.id} className="rounded-2xl bg-gray-50 px-4 py-3">
                      <p className="text-sm font-black text-gray-900">{canvas.title || '未命名项目'}</p>
                      <p className="text-xs font-bold text-gray-400">用户 @{canvas.username || 'unknown'} · revision {canvas.revision} · {formatDate(canvas.updatedAt)}</p>
                    </div>
                  ))}
                </div>
              </div>
            </section>

            <section className="mt-6 rounded-[32px] bg-white p-5 shadow-[0_24px_80px_-52px_rgba(15,23,42,0.55)]">
              <h2 className="mb-4 flex items-center gap-2 text-lg font-black tracking-tight"><FileImage size={18} /> 图片资源</h2>
              <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
                {assets.length === 0 && <p className="text-sm font-bold text-gray-400">暂无图片资源</p>}
                {assets.slice(0, 18).map(asset => (
                  <div key={asset.id} className="rounded-2xl bg-gray-50 px-4 py-3">
                    <p className="truncate text-sm font-black text-gray-900">{asset.originalFilename || asset.id}</p>
                    <p className="text-xs font-bold text-gray-400">{asset.mimeType} · {formatBytes(asset.sizeBytes)} · {formatDate(asset.createdAt)}</p>
                  </div>
                ))}
              </div>
            </section>
          </>
        )}
      </div>
    </div>
  );
};

export default AdminPage;
