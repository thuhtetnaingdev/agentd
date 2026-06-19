import { useState, useEffect } from "react";
import { api } from "../lib/api";
import {
  Server,
  FolderOpen,
  Key,
  CheckCircle2,
  XCircle,
  Loader2,
  ArrowRight,
  Activity,
  Trash2,
  RefreshCw,
  Globe,
  Clock,
} from "lucide-react";
import { useNavigate } from "react-router-dom";

interface Stats {
  serverCount: number;
  projectCount: number;
  deploymentCount: number;
  hasAPIKey: boolean;
  workDir: string;
}

interface Deployment {
  id: string;
  projectName: string;
  serverName: string;
  host: string;
  port: number;
  domain: string;
  status: string;
  healthStatus: string;
  deployedAt: string;
  lastChecked: string;
  error: string;
}

export default function Dashboard() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [loading, setLoading] = useState(true);
  const [checkingId, setCheckingId] = useState<string | null>(null);
  const navigate = useNavigate();

  const loadData = () => {
    setLoading(true);
    Promise.all([
      api.getStats(),
      api.listDeployments(),
    ])
      .then(([s, d]) => {
        setStats(s as any);
        setDeployments(d as any);
      })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    loadData();
  }, []);

  const handleHealthCheck = async (id: string) => {
    setCheckingId(id);
    try {
      await api.checkDeploymentHealth(id);
      loadData();
    } catch (e) {
      console.error(e);
    } finally {
      setCheckingId(null);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm("Delete this deployment record?")) return;
    try {
      await api.deleteDeployment(id);
      setDeployments((prev) => prev.filter((d) => d.id !== id));
    } catch (e) {
      console.error(e);
    }
  };

  if (loading && !stats) {
    return (
      <div className="h-full flex items-center justify-center">
        <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto">
      <div className="max-w-4xl mx-auto p-8">
        <div className="mb-8">
          <h1 className="text-2xl font-bold tracking-tight mb-1">Dashboard</h1>
          <p className="text-sm text-muted-foreground">
            Working directory:{" "}
            <code className="text-xs bg-secondary px-1.5 py-0.5 rounded">
              {stats?.workDir || "—"}
            </code>
          </p>
        </div>

        {/* Status cards */}
        <div className="grid grid-cols-4 gap-4 mb-8">
          <StatusCard
            icon={Key}
            label="API Key"
            ok={stats?.hasAPIKey ?? false}
            okText="Configured"
            badText="Not set"
            onClick={() => navigate("/settings")}
          />
          <StatusCard
            icon={Server}
            label="Servers"
            ok={(stats?.serverCount ?? 0) > 0}
            okText={`${stats?.serverCount} server(s)`}
            badText="None added"
            onClick={() => navigate("/servers")}
          />
          <StatusCard
            icon={FolderOpen}
            label="Projects"
            ok={(stats?.projectCount ?? 0) > 0}
            okText={`${stats?.projectCount} project(s)`}
            badText="None found"
            onClick={() => navigate("/projects")}
          />
          <StatusCard
            icon={Activity}
            label="Deployments"
            ok={(stats?.deploymentCount ?? 0) > 0}
            okText={`${stats?.deploymentCount} deployment(s)`}
            badText="None yet"
            onClick={() => {}}
          />
        </div>

        {/* Quick start guide (only when no deployments) */}
        {deployments.length === 0 && (
          <div className="bg-card border border-border rounded-lg p-6 mb-8">
            <h2 className="text-sm font-semibold mb-4">Quick Start</h2>
            <div className="space-y-3">
              <Step
                num={1}
                done={stats?.hasAPIKey ?? false}
                text="Add your API key"
                link="/settings"
              />
              <Step
                num={2}
                done={(stats?.serverCount ?? 0) > 0}
                text="Add a VPS server (SSH credentials)"
                link="/servers"
              />
              <Step
                num={3}
                done={true}
                text="Go to Projects, select one, and type 'prepare to deploy'"
                link="/projects"
              />
            </div>
          </div>
        )}

        {/* Deployment History */}
        {deployments.length > 0 && (
          <div className="bg-card border border-border rounded-lg">
            <div className="px-6 py-4 border-b border-border flex items-center justify-between">
              <h2 className="text-sm font-semibold">Deployment History</h2>
              <button
                onClick={loadData}
                className="text-xs text-muted-foreground hover:text-foreground transition-colors flex items-center gap-1"
              >
                <RefreshCw className="w-3 h-3" />
                Refresh
              </button>
            </div>
            <div className="divide-y divide-border">
              {deployments.map((dep) => (
                <DeploymentRow
                  key={dep.id}
                  dep={dep}
                  checking={checkingId === dep.id}
                  onHealthCheck={() => handleHealthCheck(dep.id)}
                  onDelete={() => handleDelete(dep.id)}
                />
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function DeploymentRow({
  dep,
  checking,
  onHealthCheck,
  onDelete,
}: {
  dep: Deployment;
  checking: boolean;
  onHealthCheck: () => void;
  onDelete: () => void;
}) {
  const isHealthy = dep.healthStatus === "healthy";
  const isUnhealthy = dep.healthStatus === "unhealthy";
  const isUnknown = dep.healthStatus === "unknown";
  const isFailed = dep.status === "failed";

  const timeAgo = getTimeAgo(new Date(dep.deployedAt));
  const checkedAgo = dep.lastChecked
    ? getTimeAgo(new Date(dep.lastChecked))
    : "never";

  return (
    <div className="px-6 py-4 hover:bg-secondary/30 transition-colors">
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start gap-3 min-w-0">
          {/* Health indicator */}
          <div className="mt-0.5 flex-shrink-0">
            {checking ? (
              <Loader2 className="w-4 h-4 animate-spin text-muted-foreground" />
            ) : isHealthy ? (
              <CheckCircle2 className="w-4 h-4 text-green-500" />
            ) : isUnhealthy ? (
              <XCircle className="w-4 h-4 text-red-500" />
            ) : isFailed ? (
              <XCircle className="w-4 h-4 text-orange-500" />
            ) : (
              <div className="w-4 h-4 rounded-full border-2 border-muted-foreground/30" />
            )}
          </div>

          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="font-medium text-sm">{dep.projectName}</span>
              {dep.domain && (
                <span className="text-xs text-muted-foreground flex items-center gap-1">
                  <Globe className="w-3 h-3" />
                  {dep.domain}
                </span>
              )}
              <span className="text-xs text-muted-foreground">
                → {dep.host}:{dep.port}
              </span>
            </div>
            <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
              <span className="flex items-center gap-1">
                <Server className="w-3 h-3" />
                {dep.serverName}
              </span>
              <span className="flex items-center gap-1">
                <Clock className="w-3 h-3" />
                {timeAgo}
              </span>
              {dep.healthStatus !== "unknown" && (
                <span
                  className={
                    isHealthy ? "text-green-500" : "text-red-500"
                  }
                >
                  {isHealthy ? "Healthy" : isUnhealthy ? "Unhealthy" : dep.healthStatus}
                </span>
              )}
              {dep.status === "failed" && (
                <span className="text-orange-500" title={dep.error}>
                  Failed
                </span>
              )}
            </div>
            {dep.lastChecked && (
              <div className="text-xs text-muted-foreground mt-0.5">
                Last checked: {checkedAgo}
              </div>
            )}
          </div>
        </div>

        {/* Actions */}
        <div className="flex items-center gap-1 flex-shrink-0">
          <button
            onClick={onHealthCheck}
            disabled={checking}
            className="p-1.5 rounded-md hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
            title="Check health"
          >
            <RefreshCw
              className={`w-3.5 h-3.5 ${checking ? "animate-spin" : ""}`}
            />
          </button>
          <button
            onClick={onDelete}
            className="p-1.5 rounded-md hover:bg-secondary text-muted-foreground hover:text-red-500 transition-colors"
            title="Delete record"
          >
            <Trash2 className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </div>
  );
}

function StatusCard({
  icon: Icon,
  label,
  ok,
  okText,
  badText,
  onClick,
}: {
  icon: any;
  label: string;
  ok: boolean;
  okText: string;
  badText: string;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className="bg-card border border-border rounded-lg p-4 text-left hover:border-primary/30 transition-colors"
    >
      <div className="flex items-center gap-2 mb-2">
        <Icon className="w-4 h-4 text-primary" />
        <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
          {label}
        </span>
      </div>
      <div className="flex items-center gap-2">
        {ok ? (
          <CheckCircle2 className="w-4 h-4 text-green-500 flex-shrink-0" />
        ) : (
          <XCircle className="w-4 h-4 text-red-500 flex-shrink-0" />
        )}
        <span className={`text-sm ${ok ? "text-green-500" : "text-red-500"}`}>
          {ok ? okText : badText}
        </span>
      </div>
    </button>
  );
}

function Step({
  num,
  done,
  text,
  link,
}: {
  num: number;
  done: boolean;
  text: string;
  link: string;
}) {
  return (
    <a
      href={link}
      className="flex items-center gap-3 p-3 rounded-md hover:bg-secondary transition-colors group"
    >
      <div
        className={`w-6 h-6 rounded-full flex items-center justify-center text-xs font-bold flex-shrink-0 ${
          done
            ? "bg-green-500/20 text-green-500"
            : "bg-secondary text-muted-foreground"
        }`}
      >
        {done ? <CheckCircle2 className="w-3.5 h-3.5" /> : num}
      </div>
      <span className="text-sm flex-1">{text}</span>
      <ArrowRight className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
    </a>
  );
}

function getTimeAgo(date: Date): string {
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  if (diffSec < 60) return "just now";
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHour = Math.floor(diffMin / 60);
  if (diffHour < 24) return `${diffHour}h ago`;
  const diffDay = Math.floor(diffHour / 24);
  if (diffDay < 30) return `${diffDay}d ago`;
  return date.toLocaleDateString();
}
