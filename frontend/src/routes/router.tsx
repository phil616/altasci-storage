import { lazy, Suspense, type ReactNode } from "react";
import { createBrowserRouter, Navigate } from "react-router-dom";
import { AdminLayout, ProtectedLayout } from "../components/Layout";
import { RouteErrorPage, RouteLoading } from "../components/RouteStatus";

const LoginPage = lazy(async () => ({ default: (await import("../pages/LoginPage")).LoginPage }));
const APIKeysPage = lazy(async () => ({ default: (await import("../pages/APIKeysPage")).APIKeysPage }));
const ProfilePage = lazy(async () => ({ default: (await import("../pages/ProfilePage")).ProfilePage }));
const ProjectPage = lazy(async () => ({ default: (await import("../pages/ProjectPage")).ProjectPage }));
const ProjectsPage = lazy(async () => ({ default: (await import("../pages/ProjectsPage")).ProjectsPage }));
const PublicSharePage = lazy(async () => ({ default: (await import("../pages/PublicSharePage")).PublicSharePage }));
const SharesPage = lazy(async () => ({ default: (await import("../pages/SharesPage")).SharesPage }));
const OIDCPage = lazy(async () => ({ default: (await import("../pages/admin/OIDCPage")).OIDCPage }));
const SettingsPage = lazy(async () => ({ default: (await import("../pages/admin/SettingsPage")).SettingsPage }));
const StoragePage = lazy(async () => ({ default: (await import("../pages/admin/StoragePage")).StoragePage }));
const UsersPage = lazy(async () => ({ default: (await import("../pages/admin/UsersPage")).UsersPage }));

function page(element: ReactNode) {
  return <Suspense fallback={<RouteLoading />}>{element}</Suspense>;
}

export const router = createBrowserRouter([
  { path: "/login", element: page(<LoginPage />), errorElement: <RouteErrorPage /> },
  { path: "/s/:shareToken", element: page(<PublicSharePage />), errorElement: <RouteErrorPage /> },
  {
    element: <ProtectedLayout />,
    errorElement: <RouteErrorPage />,
    children: [
      { path: "/projects", element: page(<ProjectsPage />) },
      { path: "/projects/:projectId", element: page(<ProjectPage />) },
      { path: "/shares", element: page(<SharesPage />) },
      { path: "/settings/api-keys", element: page(<APIKeysPage />) },
      { path: "/settings/profile", element: page(<ProfilePage />) },
      {
        path: "/admin",
        element: <AdminLayout />,
        children: [
          { path: "users", element: page(<UsersPage />) },
          { path: "storage", element: page(<StoragePage />) },
          { path: "oidc", element: page(<OIDCPage />) },
          { path: "settings", element: page(<SettingsPage />) },
        ],
      },
    ],
  },
  { path: "*", element: <Navigate to="/projects" replace /> },
]);
