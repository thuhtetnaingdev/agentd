import { useState, useEffect } from "react";
import {
  Outlet,
  NavLink,
  useLocation,
} from "react-router-dom";
import {
  LayoutDashboard,
  Settings,
  Server,
  FolderOpen,
  Terminal,
} from "lucide-react";

const navItems = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/projects", icon: FolderOpen, label: "Project" },
  { to: "/servers", icon: Server, label: "Servers" },
  { to: "/settings", icon: Settings, label: "Settings" },
];

export default function Layout() {
  const location = useLocation();
  const [status, setStatus] = useState<string>("connecting");

  useEffect(() => {
    fetch("/api/health")
      .then((r) => r.json())
      .then(() => setStatus("connected"))
      .catch(() => setStatus("disconnected"));
  }, []);

  return (
    <div className="flex h-screen bg-background">
      {/* Sidebar */}
      <aside className="w-56 border-r border-border bg-card flex flex-col">
        <div className="p-4 border-b border-border">
          <div className="flex items-center gap-2">
            <Terminal className="w-5 h-5 text-primary" />
            <span className="font-bold text-sm tracking-tight">agentd</span>
          </div>
          <div className="flex items-center gap-1.5 mt-1">
            <span
              className={`w-2 h-2 rounded-full ${
                status === "connected" ? "bg-green-500" : "bg-red-500"
              }`}
            />
            <span className="text-[10px] text-muted-foreground uppercase tracking-wider">
              {status}
            </span>
          </div>
        </div>

        <nav className="flex-1 p-2 space-y-1">
          {navItems.map((item) => {
            const isActive =
              item.to === "/"
                ? location.pathname === "/"
                : location.pathname.startsWith(item.to);
            return (
              <NavLink
                key={item.to}
                to={item.to}
                className={`flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors ${
                  isActive
                    ? "bg-primary/10 text-primary"
                    : "text-muted-foreground hover:text-foreground hover:bg-secondary"
                }`}
              >
                <item.icon className="w-4 h-4" />
                {item.label}
              </NavLink>
            );
          })}
        </nav>

        <div className="p-3 border-t border-border">
          <p className="text-[10px] text-muted-foreground text-center">
            agentd
          </p>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-hidden">
        <Outlet />
      </main>
    </div>
  );
}
