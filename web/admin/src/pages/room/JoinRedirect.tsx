import { Navigate, useParams } from "react-router-dom";

// /r/:id — публичный join URL, который admin отдаёт клиенту.
// В MVP реального WebRTC-клиента ещё нет (см. ROADMAP), поэтому
// перенаправляем в админ-вью комнаты. Когда будет client SPA — заменить.
export function JoinRedirect() {
  const { id } = useParams();
  if (!id) return <Navigate to="/rooms" replace />;
  return <Navigate to={`/rooms/${id}`} replace />;
}
