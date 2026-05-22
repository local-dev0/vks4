import { Link, isRouteErrorResponse, useRouteError } from "react-router-dom";

export function ErrorPage() {
  const err = useRouteError();
  let status: number | string = "500";
  let title = "Что-то пошло не так";
  let detail: string | undefined;

  if (isRouteErrorResponse(err)) {
    status = err.status;
    title = err.statusText || title;
    detail = typeof err.data === "string" ? err.data : JSON.stringify(err.data);
  } else if (err instanceof Error) {
    detail = err.message;
  }

  return (
    <div className="h-full grid place-items-center p-8">
      <div className="card p-8 max-w-md text-center space-y-4">
        <div className="text-5xl font-bold text-brand-600">{status}</div>
        <div className="text-xl">{title}</div>
        {detail && (
          <pre className="text-xs text-left bg-slate-100 dark:bg-slate-800 p-3 rounded whitespace-pre-wrap break-words">
            {detail}
          </pre>
        )}
        <Link to="/" className="btn-primary inline-flex">На главную</Link>
      </div>
    </div>
  );
}
