import { useState, useEffect } from "react";
import { api } from "../lib/api";
import {
  Key,
  Eye,
  EyeOff,
  Save,
  CheckCircle2,
  Cpu,
  Globe,
  ExternalLink,
  Loader2,
  Plus,
  Search,
  Trash2,
  ChevronRight,
  X,
} from "lucide-react";

// --- models.dev API types ---

interface ModelsDevProvider {
  id: string;
  name: string;
  api: string;
  env: string[];
  npm: string;
  doc: string;
  models: Record<string, ModelsDevModel>;
}

interface ModelsDevModel {
  id: string;
  name: string;
  family?: string;
  reasoning?: boolean;
  tool_call?: boolean;
  cost?: {
    input: number;
    output: number;
    cache_read?: number;
  };
  limit?: {
    context: number;
    output: number;
  };
}

interface ProviderEntry {
  id: string;
  name: string;
  api: string;
  models: { id: string; name: string }[];
}

// Known good OpenAI-compatible providers to feature first
const FEATURED_IDS = new Set([
  "openai",
  "anthropic",
  "deepseek",
  "groq",
  "openrouter",
  "together",
  "fireworks",
  "xai",
  "google",
]);

export default function Settings() {
  const [apiKey, setApiKey] = useState("");
  const [apiBaseUrl, setApiBaseUrl] = useState("https://api.openai.com/v1");
  const [model, setModel] = useState("gpt-4o");
  const [showKey, setShowKey] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");

  // models.dev integration
  const [providers, setProviders] = useState<ProviderEntry[]>([]);
  const [loadingProviders, setLoadingProviders] = useState(true);
  const [selectedProvider, setSelectedProvider] = useState<string | null>(null);
  const [providerSearch, setProviderSearch] = useState("");

  // Environment variables
  const [envEntries, setEnvEntries] = useState<
    { key: string; value: string }[]
  >([]);
  const [newEnvKey, setNewEnvKey] = useState("");
  const [newEnvValue, setNewEnvValue] = useState("");
  const [showEnvValue, setShowEnvValue] = useState<Record<string, boolean>>({});
  const [envSaving, setEnvSaving] = useState(false);

  // Load saved settings
  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        setApiKey(s.apiKey || "");
        setApiBaseUrl(s.apiBaseUrl || "https://api.openai.com/v1");
        setModel(s.model || "gpt-4o");
      })
      .catch(() => {});
  }, []);

  // Fetch providers from models.dev
  useEffect(() => {
    setLoadingProviders(true);
    fetch("https://models.dev/api.json")
      .then((res) => res.json())
      .then((data: Record<string, ModelsDevProvider>) => {
        const entries: ProviderEntry[] = Object.values(data)
          .filter((p) => p.api) // only providers with an API endpoint
          .map((p) => ({
            id: p.id,
            name: p.name,
            api: p.api,
            models: Object.values(p.models || {}).map((m) => ({
              id: m.id,
              name: m.name || m.id,
            })),
          }))
          // Sort: featured first, then alphabetically
          .sort((a, b) => {
            const aFeat = FEATURED_IDS.has(a.id) ? 0 : 1;
            const bFeat = FEATURED_IDS.has(b.id) ? 0 : 1;
            if (aFeat !== bFeat) return aFeat - bFeat;
            return a.name.localeCompare(b.name);
          });
        setProviders(entries);
      })
      .catch((err) => {
        console.warn("Failed to load models.dev data:", err);
      })
      .finally(() => setLoadingProviders(false));
  }, []);

  // Load env vars
  useEffect(() => {
    api
      .listEnv()
      .then(setEnvEntries)
      .catch(() => {});
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setError("");
    try {
      await api.updateSettings({
        apiKey,
        apiBaseUrl,
        model,
      });
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch (e: any) {
      setError(e.message);
    }
    setSaving(false);
  };

  const selectProvider = (p: ProviderEntry) => {
    setApiBaseUrl(p.api);
    setSelectedProvider(p.id);
    // Auto-select first model from this provider if we don't already have one
    if (p.models.length > 0 && !p.models.find((m) => m.id === model)) {
      setModel(p.models[0].id);
    }
    setSaved(false);
  };

  const selectedProviderData = providers.find(
    (p) => p.id === selectedProvider
  );
  const currentModels = selectedProviderData?.models || [];

  // Filter providers by search
  const filteredProviders = providerSearch
    ? providers.filter(
        (p) =>
          p.name.toLowerCase().includes(providerSearch.toLowerCase()) ||
          p.id.toLowerCase().includes(providerSearch.toLowerCase())
      )
    : providers;

  return (
    <div className="h-full overflow-y-auto">
      <div className="max-w-2xl mx-auto p-8">
        <div className="flex items-center gap-3 mb-8">
          <Key className="w-5 h-5 text-primary" />
          <h1 className="text-xl font-bold">Settings</h1>
        </div>

        <div className="space-y-6">
          {/* API Key */}
          <div className="bg-card border border-border rounded-xl p-6">
            <label className="block text-sm font-semibold mb-2">
              API Key
            </label>
            <p className="text-xs text-muted-foreground mb-3">
              Your API key is encrypted at rest and never leaves your machine
              except when calling the configured API endpoint.
            </p>
            <div className="relative">
              <input
                type={showKey ? "text" : "password"}
                value={apiKey}
                onChange={(e) => {
                  setApiKey(e.target.value);
                  setSaved(false);
                }}
                placeholder="sk-..."
                className="w-full bg-input border border-border rounded-lg px-4 py-2.5 pr-10 text-sm focus:outline-none focus:ring-2 focus:ring-ring transition-colors"
              />
              <button
                type="button"
                onClick={() => setShowKey(!showKey)}
                className="absolute right-2 top-1/2 -translate-y-1/2 p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors"
              >
                {showKey ? (
                  <EyeOff className="w-4 h-4" />
                ) : (
                  <Eye className="w-4 h-4" />
                )}
              </button>
            </div>
          </div>

          {/* Provider selection — powered by models.dev */}
          <div className="bg-card border border-border rounded-xl p-6">
            <div className="flex items-center gap-2 mb-4">
              <Globe className="w-4 h-4 text-primary" />
              <label className="text-sm font-semibold">
                Provider
              </label>
              <span className="text-[10px] text-muted-foreground bg-secondary px-1.5 py-0.5 rounded ml-auto">
                data from models.dev
              </span>
            </div>
            <p className="text-xs text-muted-foreground mb-4">
              Select a provider to auto-fill the API base URL and browse their
              available models. Any OpenAI-compatible endpoint is supported.
            </p>

            {/* Search */}
            <div className="relative mb-3">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
              <input
                type="text"
                value={providerSearch}
                onChange={(e) => setProviderSearch(e.target.value)}
                placeholder="Search providers..."
                className="w-full bg-input border border-border rounded-lg pl-9 pr-4 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring transition-colors"
              />
              {providerSearch && (
                <button
                  onClick={() => setProviderSearch("")}
                  className="absolute right-2 top-1/2 -translate-y-1/2 p-1 text-muted-foreground hover:text-foreground"
                >
                  <X className="w-3.5 h-3.5" />
                </button>
              )}
            </div>

            {/* Provider list */}
            {loadingProviders ? (
              <div className="flex items-center gap-2 py-4 text-sm text-muted-foreground">
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
                Loading providers from models.dev...
              </div>
            ) : (
              <div className="max-h-48 overflow-y-auto border border-border rounded-lg divide-y divide-border">
                {filteredProviders.length === 0 ? (
                  <p className="text-sm text-muted-foreground p-4 text-center">
                    No providers match your search.
                  </p>
                ) : (
                  filteredProviders.map((p) => (
                    <button
                      key={p.id}
                      onClick={() => selectProvider(p)}
                      className={`w-full flex items-center gap-3 px-4 py-2.5 text-left hover:bg-secondary/50 transition-colors ${
                        selectedProvider === p.id
                          ? "bg-primary/10 border-l-2 border-l-primary"
                          : ""
                      }`}
                    >
                      <img
                        src={`https://models.dev/logos/${p.id}.svg`}
                        alt=""
                        className="w-5 h-5 rounded flex-shrink-0"
                        onError={(e) => {
                          (e.target as HTMLImageElement).style.display = "none";
                        }}
                      />
                      <div className="flex-1 min-w-0">
                        <p className="text-sm font-medium truncate">
                          {p.name}
                        </p>
                        <p className="text-[10px] text-muted-foreground truncate font-mono">
                          {p.api}
                        </p>
                      </div>
                      <span className="text-[10px] text-muted-foreground flex-shrink-0">
                        {p.models.length} models
                      </span>
                      <ChevronRight className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
                    </button>
                  ))
                )}
              </div>
            )}
          </div>

          {/* API Base URL */}
          <div className="bg-card border border-border rounded-xl p-6">
            <label className="block text-sm font-semibold mb-2">
              API Base URL
            </label>
            <p className="text-xs text-muted-foreground mb-3">
              The OpenAI-compatible endpoint for chat completions.{" "}
              <code className="text-[11px] bg-secondary px-1 rounded">
                /chat/completions
              </code>{" "}
              is appended automatically.
            </p>
            <input
              type="text"
              value={apiBaseUrl}
              onChange={(e) => {
                setApiBaseUrl(e.target.value);
                setSelectedProvider(null);
                setSaved(false);
              }}
              placeholder="https://api.openai.com/v1"
              className="w-full bg-input border border-border rounded-lg px-4 py-2.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring transition-colors font-mono"
            />
          </div>

          {/* Model */}
          <div className="bg-card border border-border rounded-xl p-6">
            <div className="flex items-center gap-2 mb-4">
              <Cpu className="w-4 h-4 text-primary" />
              <label className="text-sm font-semibold">Model</label>
            </div>
            <p className="text-xs text-muted-foreground mb-4">
              Enter a model name supported by your provider. If you've selected
              a provider above, their available models are shown below.{" "}
              <a
                href="https://models.dev"
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary hover:underline inline-flex items-center gap-0.5"
              >
                Browse all models on models.dev
                <ExternalLink className="w-3 h-3" />
              </a>
            </p>

            {/* Model text input */}
            <input
              type="text"
              value={model}
              onChange={(e) => {
                setModel(e.target.value);
                setSaved(false);
              }}
              placeholder="e.g. gpt-4o"
              list="model-suggestions"
              className="w-full bg-input border border-border rounded-lg px-4 py-2.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring transition-colors font-mono"
            />
            <datalist id="model-suggestions">
              {currentModels.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
            </datalist>

            {/* Provider model chips */}
            {currentModels.length > 0 && (
              <div className="mt-3">
                <p className="text-[10px] text-muted-foreground mb-2 uppercase tracking-wider">
                  {selectedProviderData?.name} models
                </p>
                <div className="flex flex-wrap gap-1.5 max-h-32 overflow-y-auto">
                  {currentModels.map((m) => (
                    <button
                      key={m.id}
                      onClick={() => {
                        setModel(m.id);
                        setSaved(false);
                      }}
                      title={m.name}
                      className={`px-2.5 py-1 rounded text-[11px] font-mono transition-colors ${
                        model === m.id
                          ? "bg-primary/20 text-primary border border-primary/30"
                          : "bg-secondary text-muted-foreground border border-transparent hover:border-primary/20"
                      }`}
                    >
                      {m.id}
                    </button>
                  ))}
                </div>
              </div>
            )}
          </div>

          {/* Environment Variables */}
          <div className="bg-card border border-border rounded-xl p-6">
            <div className="flex items-center gap-2 mb-4">
              <Cpu className="w-4 h-4 text-green-500" />
              <label className="text-sm font-semibold">
                Environment Variables
              </label>
              <span className="text-[10px] text-muted-foreground bg-secondary px-1.5 py-0.5 rounded ml-auto">
                encrypted at rest
              </span>
            </div>
            <p className="text-xs text-muted-foreground mb-4">
              Set production environment variables for your project. Values are
              encrypted before storage and never leave your machine except when
              deployed. The agent can read these at deploy time.
            </p>

            {/* Existing env vars */}
            {envEntries.length > 0 && (
              <div className="space-y-2 mb-4">
                {envEntries.map((e) => (
                  <div
                    key={e.key}
                    className="flex items-center gap-2 bg-secondary/50 rounded-lg px-3 py-2"
                  >
                    <code className="text-xs font-mono text-foreground flex-1 min-w-0 truncate">
                      {e.key}
                    </code>
                    <span className="text-xs text-muted-foreground flex-1 min-w-0 truncate font-mono">
                      {showEnvValue[e.key]
                        ? e.value
                        : "••••••••"}
                    </span>
                    <button
                      type="button"
                      onClick={() =>
                        setShowEnvValue((prev) => ({
                          ...prev,
                          [e.key]: !prev[e.key],
                        }))
                      }
                      className="p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors flex-shrink-0"
                    >
                      {showEnvValue[e.key] ? (
                        <EyeOff className="w-3.5 h-3.5" />
                      ) : (
                        <Eye className="w-3.5 h-3.5" />
                      )}
                    </button>
                    <button
                      type="button"
                      onClick={async () => {
                        try {
                          await api.deleteEnv(e.key);
                          setEnvEntries((prev) =>
                            prev.filter((x) => x.key !== e.key)
                          );
                        } catch {
                          /* ignore */
                        }
                      }}
                      className="p-1.5 rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors flex-shrink-0"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </div>
                ))}
              </div>
            )}

            {/* Add new env var */}
            <div className="flex gap-2">
              <input
                type="text"
                value={newEnvKey}
                onChange={(e) => setNewEnvKey(e.target.value)}
                placeholder="KEY"
                className="flex-1 bg-input border border-border rounded-lg px-3 py-2 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-ring transition-colors"
              />
              <input
                type="text"
                value={newEnvValue}
                onChange={(e) => setNewEnvValue(e.target.value)}
                placeholder="value"
                className="flex-[2] bg-input border border-border rounded-lg px-3 py-2 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-ring transition-colors"
              />
              <button
                type="button"
                disabled={
                  !newEnvKey.trim() || !newEnvValue.trim() || envSaving
                }
                onClick={async () => {
                  setEnvSaving(true);
                  try {
                    const updated = await api.updateEnv(
                      newEnvKey.trim(),
                      newEnvValue
                    );
                    setEnvEntries(updated);
                    setNewEnvKey("");
                    setNewEnvValue("");
                  } catch {
                    /* ignore */
                  }
                  setEnvSaving(false);
                }}
                className="px-3 py-2 bg-primary text-primary-foreground rounded-lg text-xs flex items-center gap-1.5 hover:bg-primary/90 disabled:opacity-50 transition-colors font-medium flex-shrink-0"
              >
                <Plus className="w-3.5 h-3.5" />
                {envSaving ? "Saving..." : "Add"}
              </button>
            </div>
          </div>

          {/* Save button */}
          <div className="flex items-center gap-3">
            <button
              onClick={handleSave}
              disabled={saving}
              className="px-4 py-2 bg-primary text-primary-foreground rounded-lg text-sm flex items-center gap-2 hover:bg-primary/90 disabled:opacity-50 transition-colors font-medium"
            >
              {saved ? (
                <CheckCircle2 className="w-4 h-4" />
              ) : (
                <Save className="w-4 h-4" />
              )}
              {saving ? "Saving..." : saved ? "Saved" : "Save Settings"}
            </button>
            {error && (
              <p className="text-xs text-destructive">{error}</p>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
