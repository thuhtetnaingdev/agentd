import { useState, useEffect } from "react";
import { api } from "../lib/api";
import {
  Server,
  Plus,
  Trash2,
  Eye,
  EyeOff,
  X,
  Check,
  Globe,
  User,
  Key,
  Hash,
  Pencil,
} from "lucide-react";

interface ServerData {
  id: string;
  name: string;
  host: string;
  port: number;
  username: string;
  password: string;
}

export default function Servers() {
  const [servers, setServers] = useState<ServerData[]>([]);
  const [showAdd, setShowAdd] = useState(false);
  const [editing, setEditing] = useState<ServerData | null>(null);
  const [showPassword, setShowPassword] = useState(false);
  const [form, setForm] = useState({
    name: "",
    host: "",
    port: 22,
    username: "root",
    password: "",
  });
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  const load = () => {
    api.listServers().then(setServers).catch(console.error);
  };

  useEffect(() => {
    load();
  }, []);

  const handleAdd = async () => {
    setError("");
    setSaving(true);
    try {
      await api.createServer(form);
      setForm({ name: "", host: "", port: 22, username: "root", password: "" });
      setShowAdd(false);
      load();
    } catch (e: any) {
      setError(e.message);
    }
    setSaving(false);
  };

  const handleDelete = async (id: string) => {
    await api.deleteServer(id);
    load();
  };

  const handleUpdate = async () => {
    if (!editing) return;
    setError("");
    setSaving(true);
    try {
      await api.updateServer(editing.id, {
        name: editing.name,
        host: editing.host,
        port: editing.port,
        username: editing.username,
        password: editing.password === "••••••••" ? "" : editing.password,
      });
      setEditing(null);
      load();
    } catch (e: any) {
      setError(e.message);
    }
    setSaving(false);
  };

  const startEdit = (srv: ServerData) => {
    setEditing({ ...srv });
    setShowPassword(false);
  };

  return (
    <div className="h-full overflow-y-auto">
      <div className="max-w-2xl mx-auto p-8">
        <div className="flex items-center justify-between mb-8">
          <div className="flex items-center gap-3">
            <Server className="w-5 h-5 text-primary" />
            <h1 className="text-xl font-bold">Servers</h1>
          </div>
          {!showAdd && (
            <button
              onClick={() => setShowAdd(true)}
              className="px-4 py-2 bg-primary text-primary-foreground rounded-lg text-sm flex items-center gap-2 hover:bg-primary/90 transition-colors font-medium"
            >
              <Plus className="w-4 h-4" /> Add Server
            </button>
          )}
        </div>

        {/* Add form card */}
        {showAdd && (
          <div className="bg-card border border-border rounded-xl p-6 mb-6">
            <h3 className="text-sm font-semibold mb-5">New Server</h3>
            <ServerFormFields
              form={form}
              setForm={setForm}
              showPassword={showPassword}
              setShowPassword={setShowPassword}
            />
            {error && (
              <p className="text-xs text-destructive mt-3 bg-destructive/5 px-3 py-2 rounded-md">
                {error}
              </p>
            )}
            <div className="flex gap-2 mt-5 pt-4 border-t border-border">
              <button
                onClick={handleAdd}
                disabled={saving}
                className="px-4 py-2 bg-primary text-primary-foreground rounded-lg text-sm flex items-center gap-2 hover:bg-primary/90 disabled:opacity-50 transition-colors font-medium"
              >
                <Check className="w-4 h-4" />
                {saving ? "Saving..." : "Save Server"}
              </button>
              <button
                onClick={() => setShowAdd(false)}
                className="px-4 py-2 bg-secondary text-secondary-foreground rounded-lg text-sm flex items-center gap-2 hover:bg-secondary/90 transition-colors"
              >
                <X className="w-4 h-4" /> Cancel
              </button>
            </div>
          </div>
        )}

        {/* Empty state */}
        {servers.length === 0 && !showAdd && (
          <div className="text-center py-20 text-muted-foreground">
            <div className="w-14 h-14 rounded-2xl bg-secondary flex items-center justify-center mx-auto mb-4">
              <Server className="w-7 h-7 opacity-30" />
            </div>
            <p className="text-sm font-medium">No servers yet</p>
            <p className="text-xs mt-1.5 max-w-xs mx-auto">
              Add a VPS to start deploying. You'll need the IP address, SSH
              port, username, and password.
            </p>
          </div>
        )}

        {/* Server list */}
        <div className="space-y-3">
          {servers.map((srv) =>
            editing?.id === srv.id ? (
              <div
                key={srv.id}
                className="bg-card border border-primary/20 rounded-xl p-6"
              >
                <h3 className="text-sm font-semibold mb-5">Edit Server</h3>
                <ServerFormFields
                  form={editing}
                  setForm={(f) => setEditing(f as ServerData)}
                  showPassword={showPassword}
                  setShowPassword={setShowPassword}
                />
                {error && (
                  <p className="text-xs text-destructive mt-3 bg-destructive/5 px-3 py-2 rounded-md">
                    {error}
                  </p>
                )}
                <div className="flex gap-2 mt-5 pt-4 border-t border-border">
                  <button
                    onClick={handleUpdate}
                    disabled={saving}
                    className="px-4 py-2 bg-primary text-primary-foreground rounded-lg text-sm flex items-center gap-2 hover:bg-primary/90 disabled:opacity-50 transition-colors font-medium"
                  >
                    <Check className="w-4 h-4" />
                    {saving ? "Saving..." : "Save Changes"}
                  </button>
                  <button
                    onClick={() => {
                      setEditing(null);
                      setError("");
                    }}
                    className="px-4 py-2 bg-secondary text-secondary-foreground rounded-lg text-sm flex items-center gap-2 hover:bg-secondary/90 transition-colors"
                  >
                    <X className="w-4 h-4" /> Cancel
                  </button>
                </div>
              </div>
            ) : (
              <div
                key={srv.id}
                className="bg-card border border-border rounded-xl p-5 flex items-center justify-between group hover:border-primary/20 transition-colors"
              >
                <div className="flex items-center gap-4 min-w-0">
                  <div className="w-10 h-10 rounded-xl bg-primary/10 flex items-center justify-center flex-shrink-0">
                    <Server className="w-5 h-5 text-primary" />
                  </div>
                  <div className="min-w-0">
                    <p className="text-sm font-semibold truncate">
                      {srv.name}
                    </p>
                    <div className="flex flex-wrap items-center gap-x-4 gap-y-0.5 mt-1">
                      <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <Globe className="w-3 h-3 flex-shrink-0" />
                        {srv.host}:{srv.port}
                      </span>
                      <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <User className="w-3 h-3 flex-shrink-0" />
                        {srv.username}
                      </span>
                      <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <Key className="w-3 h-3 flex-shrink-0" />
                        {srv.password ? "••••••••" : "no password"}
                      </span>
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-1 ml-4 flex-shrink-0">
                  <button
                    onClick={() => startEdit(srv)}
                    className="p-2 text-muted-foreground hover:text-foreground hover:bg-secondary rounded-lg transition-colors"
                    title="Edit server"
                  >
                    <Pencil className="w-4 h-4" />
                  </button>
                  <button
                    onClick={() => handleDelete(srv.id)}
                    className="p-2 text-muted-foreground hover:text-destructive hover:bg-destructive/10 rounded-lg transition-colors"
                    title="Delete server"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            )
          )}
        </div>
      </div>
    </div>
  );
}

// --- Shared form fields ---

function ServerFormFields({
  form,
  setForm,
  showPassword,
  setShowPassword,
}: {
  form: { name: string; host: string; port: number; username: string; password: string };
  setForm: (f: any) => void;
  showPassword: boolean;
  setShowPassword: (v: boolean) => void;
}) {
  const inputClass =
    "w-full bg-input border border-border rounded-lg px-4 py-2.5 text-sm placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent transition-colors";

  const labelClass = "block text-xs font-medium text-muted-foreground mb-1.5";

  return (
    <div className="space-y-4">
      {/* Name — full width */}
      <div>
        <label className={labelClass}>Server Name</label>
        <input
          className={inputClass}
          placeholder="e.g. Production, Staging, Dev VPS"
          value={form.name}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
        />
      </div>

      {/* Host + Port — side by side */}
      <div className="grid grid-cols-[1fr_120px] gap-3">
        <div>
          <label className={labelClass}>Host</label>
          <input
            className={inputClass}
            placeholder="IP address or domain"
            value={form.host}
            onChange={(e) => setForm({ ...form, host: e.target.value })}
          />
        </div>
        <div>
          <label className={labelClass}>Port</label>
          <input
            className={inputClass}
            type="number"
            placeholder="22"
            value={form.port}
            onChange={(e) =>
              setForm({ ...form, port: parseInt(e.target.value) || 22 })
            }
          />
        </div>
      </div>

      {/* Username — full width */}
      <div>
        <label className={labelClass}>Username</label>
        <input
          className={inputClass}
          placeholder="e.g. root"
          value={form.username}
          onChange={(e) => setForm({ ...form, username: e.target.value })}
        />
      </div>

      {/* Password — full width with toggle */}
      <div>
        <label className={labelClass}>SSH Password</label>
        <div className="relative">
          <input
            className={`${inputClass} pr-12`}
            type={showPassword ? "text" : "password"}
            placeholder="Enter SSH password"
            value={form.password}
            onChange={(e) => setForm({ ...form, password: e.target.value })}
          />
          <button
            type="button"
            onClick={() => setShowPassword(!showPassword)}
            className="absolute right-2 top-1/2 -translate-y-1/2 p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors"
            title={showPassword ? "Hide password" : "Show password"}
          >
            {showPassword ? (
              <EyeOff className="w-4 h-4" />
            ) : (
              <Eye className="w-4 h-4" />
            )}
          </button>
        </div>
        <p className="text-[11px] text-muted-foreground mt-1.5">
          Password is encrypted at rest with AES-256-GCM.
        </p>
      </div>
    </div>
  );
}
