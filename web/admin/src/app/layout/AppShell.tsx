import { NavLink, Outlet } from "react-router-dom";
import {
  LayoutDashboard, Users, Video, Layers, FileVideo, LineChart,
  ScrollText, Settings, Sun, Moon, LogOut,
} from "lucide-react";
import clsx from "clsx";
import { useTheme } from "@/features/theme/ThemeProvider";
import { useAuth } from "@/features/auth/AuthProvider";

type Role = "admin" | "operator" | "moderator" | "viewer";
interface NavLinkSpec {
  to: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  roles?: Role[];
}

const links: NavLinkSpec[] = [
  { to: "/",            label: "Dashboard",   icon: LayoutDashboard },
  { to: "/rooms",       label: "Rooms",       icon: Video },
  { to: "/layouts",     label: "Layouts",     icon: Layers },
  { to: "/recordings",  label: "Recordings",  icon: FileVideo },
  { to: "/users",       label: "Users",       icon: Users, roles: ["admin"] },
  { to: "/monitoring",  label: "Monitoring",  icon: LineChart },
  { to: "/logs",        label: "Logs",        icon: ScrollText },
  { to: "/settings",    label: "Settings",    icon: Settings, roles: ["admin"] },
];

export function AppShell() {
  const { theme, toggle } = useTheme();
  const { user, logout } = useAuth();
  return (
    <div className="grid grid-cols-[14rem_1fr] grid-rows-[3.5rem_1fr] h-full">
      <div className="row-span-2 border-r border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 flex flex-col">
        <div className="px-4 py-3 font-semibold text-brand-600 dark:text-brand-300 text-lg tracking-tight">vks4 admin</div>
        <nav className="flex-1 px-2 space-y-0.5">
          {links.map((l) => {
            if (l.roles && (!user || !l.roles.includes(user.role as Role))) return null;
            const Icon = l.icon;
            return (
              <NavLink
                key={l.to}
                to={l.to}
                end={l.to === "/"}
                className={({ isActive }) =>
                  clsx(
                    "flex items-center gap-2 px-3 py-2 rounded-md text-sm",
                    isActive
                      ? "bg-brand-50 dark:bg-brand-900/40 text-brand-700 dark:text-brand-200"
                      : "hover:bg-slate-100 dark:hover:bg-slate-800/60",
                  )
                }
              >
                <Icon className="size-4" />
                {l.label}
              </NavLink>
            );
          })}
        </nav>
        <div className="p-2 text-xs text-slate-500 border-t border-slate-200 dark:border-slate-800">
          v0.1.0
        </div>
      </div>
      <header className="col-start-2 flex items-center justify-end gap-2 px-4 border-b border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900">
        <div className="text-sm text-slate-500">{user?.email}</div>
        <button className="btn-ghost" title="Theme" onClick={toggle}>
          {theme === "dark" ? <Sun className="size-4" /> : <Moon className="size-4" />}
        </button>
        <button className="btn-ghost" title="Logout" onClick={logout}>
          <LogOut className="size-4" />
        </button>
      </header>
      <main className="col-start-2 overflow-auto p-6">
        <Outlet />
      </main>
    </div>
  );
}
