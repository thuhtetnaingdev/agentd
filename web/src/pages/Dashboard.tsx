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
} from "lucide-react";
import { useNavigate } from "react-router-dom";

interface Stats {
  serverCount: number;
  projectCount: number;
  hasAPIKey: boolean;
  workDir: string;
}

export default function Dashboard() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(true);
  const navigate = useNavigate();

  useEffect(() => {
    api
      .getStats()
      .then(setStats as any)
      .catch(console.error)
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="h-full flex items-center justify-center">
        <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto">
      <div className="max-w-3xl mx-auto p-8">
        <div className="mb-8">
          <h1 className="text-2xl font-bold tracking-tight mb-1">Dashboard</h1>
          <p className="text-sm text-muted-foreground">
            Working directory: <code className="text-xs bg-secondary px-1.5 py-0.5 rounded">{stats?.workDir || "—"}</code>
          </p>
        </div>

        {/* Status cards */}
        <div className="grid grid-cols-3 gap-4 mb-8">
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
            count={stats?.serverCount ?? 0}
            ok={(stats?.serverCount ?? 0) > 0}
            okText={`${stats?.serverCount} server(s)`}
            badText="None added"
            onClick={() => navigate("/servers")}
          />
          <StatusCard
            icon={FolderOpen}
            label="Projects"
            count={stats?.projectCount ?? 0}
            ok={(stats?.projectCount ?? 0) > 0}
            okText={`${stats?.projectCount} project(s)`}
            badText="None found"
            onClick={() => navigate("/projects")}
          />
        </div>

        {/* Quick start guide */}
        <div className="bg-card border border-border rounded-lg p-6">
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
      </div>
    </div>
  );
}

function StatusCard({
  icon: Icon,
  label,
  ok,
  count,
  okText,
  badText,
  onClick,
}: {
  icon: any;
  label: string;
  ok: boolean;
  count?: number;
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
