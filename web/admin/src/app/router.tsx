import { createBrowserRouter } from "react-router-dom";
import { AppShell } from "./layout/AppShell";
import { Guard } from "@/features/auth/Guard";
import { Login } from "@/pages/login/Login";
import { Dashboard } from "@/pages/dashboard/Dashboard";
import { RoomsList } from "@/pages/rooms/RoomsList";
import { RoomDetail } from "@/pages/rooms/RoomDetail";
import { RoomCreate } from "@/pages/rooms/RoomCreate";
import { LayoutEditor } from "@/pages/layouts/LayoutEditor";
import { LayoutsList } from "@/pages/layouts/LayoutsList";
import { RoomControl } from "@/pages/rooms/RoomControl";
import { UsersPage } from "@/pages/users/Users";
import { Recordings } from "@/pages/recordings/Recordings";
import { Monitoring } from "@/pages/monitoring/Monitoring";
import { Logs } from "@/pages/logs/Logs";
import { SettingsPage } from "@/pages/settings/Settings";
import { ErrorPage } from "@/pages/error/ErrorPage";
import { Room } from "@/pages/room/Room";

export const router = createBrowserRouter([
  { path: "/login",  element: <Login />,                              errorElement: <ErrorPage /> },
  { path: "/r/:id",  element: <Guard><Room /></Guard>,                errorElement: <ErrorPage /> },
  {
    path: "/",
    element: <Guard><AppShell /></Guard>,
    errorElement: <ErrorPage />,
    children: [
      { index: true,              element: <Dashboard /> },
      { path: "rooms",            element: <RoomsList /> },
      { path: "rooms/new",        element: <RoomCreate /> },
      { path: "rooms/:id",        element: <RoomDetail /> },
      { path: "rooms/:id/control", element: <RoomControl /> },
      { path: "layouts/:id/edit",  element: <LayoutEditor /> },
      { path: "layouts",          element: <LayoutsList /> },
      { path: "recordings",       element: <Recordings /> },
      { path: "users",            element: <Guard roles={["admin"]}><UsersPage /></Guard> },
      { path: "monitoring",       element: <Monitoring /> },
      { path: "logs",             element: <Logs /> },
      { path: "settings",         element: <Guard roles={["admin"]}><SettingsPage /></Guard> },
      { path: "*",                element: <ErrorPage /> },
    ],
  },
]);
